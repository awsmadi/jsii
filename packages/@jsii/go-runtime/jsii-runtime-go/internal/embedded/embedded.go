package embedded

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"hash"
	"os"
	"path"
	"path/filepath"
	"sort"
)

// embeddedRootDir is the name of the root directory for the embeddedFS variable.
const embeddedRootDir string = "resources"

//go:embed resources/*
var embeddedFS embed.FS

// entrypointName is the path to the entry point relative to the embeddedRootDir.
var entrypointName = path.Join("bin", "jsii-runtime.js")

// ExtractRuntime extracts a copy of the embedded runtime library into
// the designated directory, and returns the fully qualified path to the entry
// point to be used when starting the child process.
func ExtractRuntime(into string) (entrypoint string, err error) {
	err = extractRuntime(into, embeddedRootDir)
	if err == nil {
		entrypoint = filepath.Join(into, entrypointName)
	}
	return
}

// EntrypointPath returns the path to the entrypoint within a given root directory.
func EntrypointPath(root string) string {
	return filepath.Join(root, entrypointName)
}

// RuntimeHash returns a short hex hash of all embedded runtime files, used
// to construct version-specific cache directory names.
func RuntimeHash() string {
	h := sha256.New()
	hashEmbeddedDir(h, embeddedRootDir)
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

func hashEmbeddedDir(h hash.Hash, dir string) {
	files, err := embeddedFS.ReadDir(dir)
	if err != nil {
		return
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		src := path.Join(dir, name)
		h.Write([]byte(name))
		if data, err := embeddedFS.ReadFile(src); err == nil {
			h.Write(data)
		} else {
			// It's a directory; recurse
			hashEmbeddedDir(h, src)
		}
	}
}

// extractRuntime copies the contents of embeddedFS at "from" to the provided
// "into" directory, recursively.
func extractRuntime(into string, from string) error {
	files, err := embeddedFS.ReadDir(from)
	if err != nil {
		return err
	}
	for _, file := range files {
		src := path.Join(from, file.Name())
		dest := path.Join(into, file.Name())
		if file.IsDir() {
			if err = os.Mkdir(dest, 0o700); err != nil {
				return err
			}
			if err = extractRuntime(dest, src); err != nil {
				return err
			}
		} else {
			data, err := embeddedFS.ReadFile(src)
			if err != nil {
				return err
			}
			if err = os.WriteFile(dest, data, 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}
