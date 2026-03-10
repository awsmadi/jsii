package process

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeCacheDir(t *testing.T) {
	t.Run("respects JSII_RUNTIME_CACHE_DIR override", func(t *testing.T) {
		old := os.Getenv("JSII_RUNTIME_CACHE_DIR")
		defer os.Setenv("JSII_RUNTIME_CACHE_DIR", old)

		os.Setenv("JSII_RUNTIME_CACHE_DIR", "/tmp/custom-jsii-cache")
		dir := runtimeCacheDir()
		if dir != "/tmp/custom-jsii-cache" {
			t.Errorf("expected /tmp/custom-jsii-cache, got %v", dir)
		}
	})

	t.Run("respects XDG_CACHE_HOME", func(t *testing.T) {
		oldCache := os.Getenv("JSII_RUNTIME_CACHE_DIR")
		oldXDG := os.Getenv("XDG_CACHE_HOME")
		defer func() {
			os.Setenv("JSII_RUNTIME_CACHE_DIR", oldCache)
			os.Setenv("XDG_CACHE_HOME", oldXDG)
		}()

		os.Unsetenv("JSII_RUNTIME_CACHE_DIR")
		os.Setenv("XDG_CACHE_HOME", "/tmp/xdg-test")
		dir := runtimeCacheDir()
		if len(dir) == 0 {
			t.Fatal("expected non-empty cache dir")
		}
		expected := filepath.Join("/tmp/xdg-test", "aws", "jsii")
		if !filepath.HasPrefix(dir, expected) {
			t.Errorf("expected dir to start with %v, got %v", expected, dir)
		}
	})

	t.Run("returns stable path", func(t *testing.T) {
		oldCache := os.Getenv("JSII_RUNTIME_CACHE_DIR")
		defer os.Setenv("JSII_RUNTIME_CACHE_DIR", oldCache)
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

		oldCache := os.Getenv("JSII_RUNTIME_CACHE_DIR")
		oldNoCache := os.Getenv("JSII_RUNTIME_NO_CACHE")
		defer func() {
			os.Setenv("JSII_RUNTIME_CACHE_DIR", oldCache)
			if oldNoCache == "" {
				os.Unsetenv("JSII_RUNTIME_NO_CACHE")
			} else {
				os.Setenv("JSII_RUNTIME_NO_CACHE", oldNoCache)
			}
		}()

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

	t.Run("no cache falls back to tempdir", func(t *testing.T) {
		oldNoCache := os.Getenv("JSII_RUNTIME_NO_CACHE")
		defer func() {
			if oldNoCache == "" {
				os.Unsetenv("JSII_RUNTIME_NO_CACHE")
			} else {
				os.Setenv("JSII_RUNTIME_NO_CACHE", oldNoCache)
			}
		}()

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
}
