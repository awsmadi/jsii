package process

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/Masterminds/semver/v3"
	"github.com/aws/jsii-runtime-go/internal/embedded"
)

const JSII_NODE string = "JSII_NODE"
const JSII_RUNTIME string = "JSII_RUNTIME"

type ErrorResponse struct {
	Error string  `json:"error"`
	Stack *string `json:"stack"`
	Name  *string `json:"name"`
}

// Process is a simple interface over the child process hosting the
// @jsii/kernel process. It only exposes a very straight-forward
// request/response interface.
type Process struct {
	compatibleVersions *semver.Constraints

	cmd        *exec.Cmd
	tmpdir     string
	usingCache bool

	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	requests   *json.Encoder
	responses  *json.Decoder
	stderrDone chan bool

	started bool
	closed  bool

	mutex sync.Mutex
}

// runtimeCacheDir returns the persistent cache directory for the embedded runtime.
// It respects JSII_RUNTIME_CACHE_DIR override and XDG_CACHE_HOME.
func runtimeCacheDir() string {
	if override := os.Getenv("JSII_RUNTIME_CACHE_DIR"); override != "" {
		return override
	}

	var base string
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		base = xdg
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".cache")
	}

	runtimeHash := embedded.RuntimeHash()
	return filepath.Join(base, "aws", "jsii", fmt.Sprintf("runtime-%s", runtimeHash))
}

// NewProcess prepares a new child process, but does not start it yet. It will
// be automatically started whenever the client attempts to send a request
// to it.
//
// If the JSII_RUNTIME environment variable is set, this command will be used
// to start the child process, in a sub-shell (using %COMSPEC% or cmd.exe on
// Windows; $SHELL or /bin/sh on other OS'es). Otherwise, the embedded runtime
// application will be extracted into a cache directory (or temporary directory
// if caching is disabled), and used.
//
// The current process' environment is inherited by the child process. Additional
// environment may be injected into the child process' environment - all of which
// with lower precedence than the launching process' environment, with the notable
// exception of JSII_AGENT, which is reserved.
func NewProcess(compatibleVersions string) (*Process, error) {
	p := Process{}

	if constraints, err := semver.NewConstraint(compatibleVersions); err != nil {
		return nil, err
	} else {
		p.compatibleVersions = constraints
	}

	if custom := os.Getenv(JSII_RUNTIME); custom != "" {
		var (
			command string
			args    []string
		)
		// Sub-shelling in order to avoid having to parse arguments
		if runtime.GOOS == "windows" {
			// On windows, we use %ComSpec% if set, or cmd.exe
			if cmd := os.Getenv("ComSpec"); cmd != "" {
				command = cmd
			} else {
				command = "cmd.exe"
			}
			// The /d option disables Registry-defined AutoRun, it's safer to enable
			// The /s option tells cmd.exe the command is quoted as if it were typed into a prompt
			// The /c option tells cmd.exe to run the specified command and exit immediately
			args = []string{"/d", "/s", "/c", custom}
		} else {
			// On other OS'es, we use $SHELL and fall back to "/bin/sh"
			if shell := os.Getenv("SHELL"); shell != "" {
				command = shell
			} else {
				command = "/bin/sh"
			}
			args = []string{"-c", custom}
		}
		p.cmd = exec.Command(command, args...)
	} else {
		entrypoint, err := p.extractOrCacheRuntime()
		if err != nil {
			p.Close()
			return nil, err
		}
		if node := os.Getenv(JSII_NODE); node != "" {
			p.cmd = exec.Command(node, entrypoint)
		} else {
			p.cmd = exec.Command("node", entrypoint)
		}
	}

	// Setting up environment - if duplicate keys are found, the last value is used, so we are careful with ordering. In
	// particular, we are setting NODE_OPTIONS only if `os.Environ()` does not have another value... So the user can
	// control the environment... However, JSII_AGENT must always be controlled by this process.
	p.cmd.Env = append([]string{"NODE_OPTIONS=--max-old-space-size=4069"}, os.Environ()...)
	p.cmd.Env = append(p.cmd.Env, fmt.Sprintf("JSII_AGENT=%v/%v/%v", runtime.Version(), runtime.GOOS, runtime.GOARCH))

	if stdin, err := p.cmd.StdinPipe(); err != nil {
		p.Close()
		return nil, err
	} else {
		p.stdin = stdin
		p.requests = json.NewEncoder(stdin)
	}
	if stdout, err := p.cmd.StdoutPipe(); err != nil {
		p.Close()
		return nil, err
	} else {
		p.stdout = stdout
		p.responses = json.NewDecoder(stdout)
	}
	if stderr, err := p.cmd.StderrPipe(); err != nil {
		p.Close()
		return nil, err
	} else {
		p.stderr = stderr
	}

	return &p, nil
}

// extractOrCacheRuntime extracts the embedded runtime to a persistent cache
// directory, or falls back to a temporary directory if caching is disabled.
func (p *Process) extractOrCacheRuntime() (string, error) {
	noCache := strings.TrimSpace(os.Getenv("JSII_RUNTIME_NO_CACHE"))
	if noCache == "1" || strings.EqualFold(noCache, "true") {
		return p.extractToTempDir()
	}

	cacheDir := runtimeCacheDir()
	if cacheDir == "" {
		return p.extractToTempDir()
	}

	marker := filepath.Join(cacheDir, ".jsii_cache_complete")
	if _, err := os.Stat(marker); err == nil {
		// Cache hit
		p.usingCache = true
		return embedded.EntrypointPath(cacheDir), nil
	}

	// Cache miss - extract
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return p.extractToTempDir()
	}

	entrypoint, err := embedded.ExtractRuntime(cacheDir)
	if err != nil {
		// Fall back to temp dir on extraction failure
		os.RemoveAll(cacheDir)
		return p.extractToTempDir()
	}

	// Write marker after successful extraction
	if err := os.WriteFile(marker, []byte("ok"), 0o644); err != nil {
		// Non-fatal: cache works but won't be detected next time
		fmt.Fprintf(os.Stderr, "warning: could not write cache marker: %v\n", err)
	}

	p.usingCache = true
	return entrypoint, nil
}

// extractToTempDir is the original extraction logic using a temporary directory.
func (p *Process) extractToTempDir() (string, error) {
	tmpdir, err := ioutil.TempDir("", "jsii-runtime.*")
	if err != nil {
		return "", err
	}
	p.tmpdir = tmpdir

	entrypoint, err := embedded.ExtractRuntime(tmpdir)
	if err != nil {
		return "", err
	}
	return entrypoint, nil
}

func (p *Process) ensureStarted() error {
	if p.closed {
		return fmt.Errorf("this process has been closed")
	}
	if p.started {
		return nil
	}
	if err := p.cmd.Start(); err != nil {
		p.Close()
		return err
	}
	p.started = true

	done := make(chan bool, 1)
	go p.consumeStderr(done)
	p.stderrDone = done

	var handshake handshakeResponse
	if err := p.readResponse(&handshake); err != nil {
		p.Close()
		return err
	}

	if runtimeVersion, err := handshake.runtimeVersion(); err != nil {
		p.Close()
		return err
	} else if ok, errs := p.compatibleVersions.Validate(runtimeVersion); !ok {
		causes := make([]string, len(errs))
		for i, err := range errs {
			causes[i] = fmt.Sprintf("- %v", err)
		}
		p.Close()
		return fmt.Errorf("incompatible runtime version:\n%v", strings.Join(causes, "\n"))
	}

	go func() {
		err := p.cmd.Wait()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Runtime process exited abnormally: %v", err.Error())
		}
		p.Close()
	}()

	return nil
}

// Request starts the child process if that has not happened yet, then
// encodes the supplied request and sends it to the child process
// via the requests channel, then decodes the response into the provided
// response pointer. If the process is not in a usable state, or if the
// encoding fails, an error is returned.
func (p *Process) Request(request interface{}, response interface{}) error {
	if err := p.ensureStarted(); err != nil {
		return err
	}
	if err := p.requests.Encode(request); err != nil {
		p.Close()
		return err
	}
	return p.readResponse(response)
}

func (p *Process) readResponse(into interface{}) error {
	if !p.responses.More() {
		return fmt.Errorf("no response received from child process")
	}

	var raw json.RawMessage
	var respmap map[string]interface{}
	err := p.responses.Decode(&raw)
	if err != nil {
		return err
	}

	err = json.Unmarshal(raw, &respmap)
	if err != nil {
		return err
	}

	var errResp ErrorResponse
	if _, ok := respmap["error"]; ok {
		json.Unmarshal(raw, &errResp)

		if errResp.Name != nil && *errResp.Name == "@jsii/kernel.Fault" {
			return fmt.Errorf("JsiiError: %s %s", *errResp.Name, errResp.Error)
		}

		return errors.New(errResp.Error)
	}

	return json.Unmarshal(raw, &into)
}

func (p *Process) Close() {
	if p.closed {
		return
	}

	// Acquire the lock, so we don't try to concurrently close multiple times
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Check again now that we own the lock, it may be a fast exit!
	if p.closed {
		return
	}

	if p.stdin != nil {
		// Try to send the exit message, this might fail, but we can ignore that.
		p.stdin.Write([]byte("{\"exit\":0}\n"))

		// Close STDIN for the child process now. Ignoring errors, as it may
		// have been closed already (e.g: if the process exited).
		p.stdin.Close()
		p.stdin = nil
	}

	if p.stdout != nil {
		// Close STDOUT for the child process now, as we don't expect to receive
		// responses anymore. Ignoring errors, as it may have been closed
		// already (e.g: if the process exited).
		p.stdout.Close()
		p.stdout = nil
	}

	if p.stderrDone != nil {
		// Wait for the stderr sink goroutine to have finished
		<-p.stderrDone
		p.stderrDone = nil
	}

	if p.stderr != nil {
		// Close STDERR for the child process now, as we're no longer consuming
		// it anyway. Ignoring errors, as it may havebeen closed already (e.g:
		// if the process exited).
		p.stderr.Close()
		p.stderr = nil
	}

	if p.cmd != nil {
		// Wait for the child process to be dead and gone (should already be)
		p.cmd.Wait()
		p.cmd = nil
	}

	if p.tmpdir != "" {
		// Clean up any temporary directory we provisioned (not cached dirs).
		if err := os.RemoveAll(p.tmpdir); err != nil {
			fmt.Fprintf(os.Stderr, "could not clean up temporary directory: %v\n", err)
		}
		p.tmpdir = ""
	}

	p.closed = true
}
