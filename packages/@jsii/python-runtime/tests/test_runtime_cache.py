import os
import shutil
import tempfile

import pytest


class TestRuntimeCache:
    """Tests for the persistent runtime extraction cache."""

    def test_cache_dir_computation(self):
        """Verify _get_cache_dir returns a path under ~/.cache/aws/jsii/."""
        from jsii._kernel.providers.process import _get_cache_dir

        cache_dir = _get_cache_dir()
        assert "aws" in cache_dir
        assert "jsii" in cache_dir
        assert "runtime-" in cache_dir

    def test_cache_dir_respects_override(self, tmp_path):
        """Verify JSII_RUNTIME_CACHE_DIR env var overrides the default."""
        from jsii._kernel.providers.process import _get_cache_dir

        override = str(tmp_path / "custom-cache")
        old = os.environ.get("JSII_RUNTIME_CACHE_DIR")
        try:
            os.environ["JSII_RUNTIME_CACHE_DIR"] = override
            assert _get_cache_dir() == override
        finally:
            if old is None:
                os.environ.pop("JSII_RUNTIME_CACHE_DIR", None)
            else:
                os.environ["JSII_RUNTIME_CACHE_DIR"] = old

    def test_cache_dir_respects_xdg(self, tmp_path):
        """Verify XDG_CACHE_HOME is respected."""
        from jsii._kernel.providers.process import _get_cache_dir

        xdg_dir = str(tmp_path / "xdg-cache")
        old_xdg = os.environ.get("XDG_CACHE_HOME")
        old_override = os.environ.get("JSII_RUNTIME_CACHE_DIR")
        try:
            os.environ.pop("JSII_RUNTIME_CACHE_DIR", None)
            os.environ["XDG_CACHE_HOME"] = xdg_dir
            cache_dir = _get_cache_dir()
            assert cache_dir.startswith(xdg_dir)
        finally:
            if old_xdg is None:
                os.environ.pop("XDG_CACHE_HOME", None)
            else:
                os.environ["XDG_CACHE_HOME"] = old_xdg
            if old_override is not None:
                os.environ["JSII_RUNTIME_CACHE_DIR"] = old_override

    def test_runtime_hash_is_stable(self):
        """Verify _compute_runtime_hash returns the same value on repeated calls."""
        from jsii._kernel.providers.process import _compute_runtime_hash

        h1 = _compute_runtime_hash()
        h2 = _compute_runtime_hash()
        assert h1 == h2
        assert len(h1) == 16

    def test_second_spawn_reuses_cache(self, tmp_path):
        """Verify that a second _jsii_runtime call with the same cache dir
        skips extraction (cache hit via marker file)."""
        from jsii._kernel.providers.process import _NodeProcess, _get_cache_dir

        cache_dir = str(tmp_path / "jsii-cache")
        old = os.environ.get("JSII_RUNTIME_CACHE_DIR")
        old_no_cache = os.environ.get("JSII_RUNTIME_NO_CACHE")
        try:
            os.environ["JSII_RUNTIME_CACHE_DIR"] = cache_dir
            os.environ.pop("JSII_RUNTIME_NO_CACHE", None)

            # First call: should extract and create marker
            proc1 = _NodeProcess()
            path1 = proc1._jsii_runtime()
            assert os.path.isfile(path1)
            marker = os.path.join(cache_dir, ".jsii_cache_complete")
            assert os.path.isfile(marker)

            # Second call: should hit cache (marker exists)
            proc2 = _NodeProcess()
            path2 = proc2._jsii_runtime()
            assert path2 == path1
            assert proc2._using_cache is True
        finally:
            if old is None:
                os.environ.pop("JSII_RUNTIME_CACHE_DIR", None)
            else:
                os.environ["JSII_RUNTIME_CACHE_DIR"] = old
            if old_no_cache is not None:
                os.environ["JSII_RUNTIME_NO_CACHE"] = old_no_cache

    def test_no_cache_env_var(self, tmp_path):
        """Verify JSII_RUNTIME_NO_CACHE=1 forces temp dir extraction."""
        from jsii._kernel.providers.process import _NodeProcess

        old_no_cache = os.environ.get("JSII_RUNTIME_NO_CACHE")
        old_cache_dir = os.environ.get("JSII_RUNTIME_CACHE_DIR")
        try:
            os.environ["JSII_RUNTIME_NO_CACHE"] = "1"
            os.environ.pop("JSII_RUNTIME_CACHE_DIR", None)

            proc = _NodeProcess()
            path = proc._jsii_runtime()
            assert os.path.isfile(path)
            # Should NOT be using cache
            assert proc._using_cache is False
        finally:
            if old_no_cache is None:
                os.environ.pop("JSII_RUNTIME_NO_CACHE", None)
            else:
                os.environ["JSII_RUNTIME_NO_CACHE"] = old_no_cache
            if old_cache_dir is not None:
                os.environ["JSII_RUNTIME_CACHE_DIR"] = old_cache_dir
