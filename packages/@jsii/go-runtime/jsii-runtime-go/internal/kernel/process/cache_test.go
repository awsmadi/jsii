package process

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// restoreEnv saves and restores an environment variable around a test.
func restoreEnv(t *testing.T, key string) {
	t.Helper()
	val, ok := os.LookupEnv(key)
	t.Cleanup(func() {
		if ok {
			os.Setenv(key, val)
		} else {
			os.Unsetenv(key)
		}
	})
}

func TestRuntimeCacheDir(t *testing.T) {
	t.Run("respects JSII_RUNTIME_CACHE_DIR override", func(t *testing.T) {
		restoreEnv(t, "JSII_RUNTIME_CACHE_DIR")

		os.Setenv("JSII_RUNTIME_CACHE_DIR", "/tmp/custom-jsii-cache")
		dir := runtimeCacheDir()
		if dir != "/tmp/custom-jsii-cache" {
			t.Errorf("expected /tmp/custom-jsii-cache, got %v", dir)
		}
	})

	t.Run("respects XDG_CACHE_HOME", func(t *testing.T) {
		restoreEnv(t, "JSII_RUNTIME_CACHE_DIR")
		restoreEnv(t, "XDG_CACHE_HOME")

		os.Unsetenv("JSII_RUNTIME_CACHE_DIR")
		os.Setenv("XDG_CACHE_HOME", "/tmp/xdg-test")
		dir := runtimeCacheDir()
		if len(dir) == 0 {
			t.Fatal("expected non-empty cache dir")
		}
		expected := filepath.Join("/tmp/xdg-test", "aws", "jsii")
		if !strings.HasPrefix(dir, expected) {
			t.Errorf("expected dir to start with %v, got %v", expected, dir)
		}
	})

	t.Run("returns stable path", func(t *testing.T) {
		restoreEnv(t, "JSII_RUNTIME_CACHE_DIR")
		os.Unsetenv("JSII_RUNTIME_CACHE_DIR")

		dir1 := runtimeCacheDir()
		dir2 := runtimeCacheDir()
		if dir1 != dir2 {
			t.Errorf("expected stable cache dir, got %v and %v", dir1, dir2)
		}
	})
}

func TestExtractOrCacheRuntime(t *testing.T) {
	t.Run("cache reuse on second call", func(t *testing.T) {
		tmpdir := t.TempDir()
		cacheDir := filepath.Join(tmpdir, "jsii-cache")

		restoreEnv(t, "JSII_RUNTIME_CACHE_DIR")
		restoreEnv(t, "JSII_RUNTIME_NO_CACHE")

		os.Setenv("JSII_RUNTIME_CACHE_DIR", cacheDir)
		os.Unsetenv("JSII_RUNTIME_NO_CACHE")

		// First extraction
		p1 := &Process{}
		entry1, err := p1.extractOrCacheRuntime()
		if err != nil {
			t.Fatal(err)
		}
		if !p1.usingCache {
			t.Error("expected usingCache=true after first extraction")
		}

		// Verify marker was created
		marker := filepath.Join(cacheDir, ".jsii_cache_complete")
		if _, err := os.Stat(marker); os.IsNotExist(err) {
			t.Error("expected cache marker to exist")
		}

		// Second extraction should be a cache hit
		p2 := &Process{}
		entry2, err := p2.extractOrCacheRuntime()
		if err != nil {
			t.Fatal(err)
		}
		if entry1 != entry2 {
			t.Errorf("expected same entrypoint, got %v and %v", entry1, entry2)
		}
		if !p2.usingCache {
			t.Error("expected usingCache=true on cache hit")
		}
	})

	t.Run("corrupted cache triggers re-extraction", func(t *testing.T) {
		tmpdir := t.TempDir()
		cacheDir := filepath.Join(tmpdir, "jsii-cache")

		restoreEnv(t, "JSII_RUNTIME_CACHE_DIR")
		restoreEnv(t, "JSII_RUNTIME_NO_CACHE")

		os.Setenv("JSII_RUNTIME_CACHE_DIR", cacheDir)
		os.Unsetenv("JSII_RUNTIME_NO_CACHE")

		// First extraction to populate cache
		p1 := &Process{}
		entry1, err := p1.extractOrCacheRuntime()
		if err != nil {
			t.Fatal(err)
		}

		// Corrupt the cache: delete the entrypoint but leave the marker
		if err := os.Remove(entry1); err != nil {
			t.Fatal(err)
		}

		// Should detect corruption and re-extract
		p2 := &Process{}
		entry2, err := p2.extractOrCacheRuntime()
		if err != nil {
			t.Fatal(err)
		}
		if entry2 == "" {
			t.Error("expected non-empty entrypoint after re-extraction")
		}
	})

	t.Run("no cache falls back to tempdir", func(t *testing.T) {
		restoreEnv(t, "JSII_RUNTIME_NO_CACHE")

		os.Setenv("JSII_RUNTIME_NO_CACHE", "1")

		p := &Process{}
		entry, err := p.extractOrCacheRuntime()
		if err != nil {
			t.Fatal(err)
		}
		if entry == "" {
			t.Error("expected non-empty entrypoint")
		}
		if p.usingCache {
			t.Error("expected usingCache=false when JSII_RUNTIME_NO_CACHE=1")
		}
		if p.tmpdir == "" {
			t.Error("expected tmpdir to be set for temp extraction")
		}
		// Cleanup
		os.RemoveAll(p.tmpdir)
	})

	t.Run("no cache with true string", func(t *testing.T) {
		restoreEnv(t, "JSII_RUNTIME_NO_CACHE")

		os.Setenv("JSII_RUNTIME_NO_CACHE", "True")

		p := &Process{}
		entry, err := p.extractOrCacheRuntime()
		if err != nil {
			t.Fatal(err)
		}
		if entry == "" {
			t.Error("expected non-empty entrypoint")
		}
		if p.usingCache {
			t.Error("expected usingCache=false when JSII_RUNTIME_NO_CACHE=True")
		}
		if p.tmpdir == "" {
			t.Error("expected tmpdir to be set for temp extraction")
		}
		// Cleanup
		os.RemoveAll(p.tmpdir)
	})
}
