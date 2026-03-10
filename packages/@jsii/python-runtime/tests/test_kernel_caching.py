"""Tests for kernel property caching functionality."""
import sys
import os
# Add src to path for direct import
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..', 'src'))

import unittest
from unittest.mock import Mock

# Direct imports from source
from jsii._kernel import Kernel
from jsii._utils import Singleton
from jsii._kernel.types import (
    ObjRef,
    GetResponse,
    SetResponse,
)


class TestKernelPropertyCaching(unittest.TestCase):
    """Test property caching in the Kernel class."""

    def setUp(self):
        """Set up test fixtures."""
        # Reset the Singleton so each test gets a fresh Kernel instance
        Singleton._instances.pop(Kernel, None)

        # Create a mock provider
        self.mock_provider_class = Mock()
        self.mock_provider = Mock()
        self.mock_provider_class.return_value = self.mock_provider

        # Create kernel with mock provider
        self.kernel = Kernel(provider_class=self.mock_provider_class)

    def tearDown(self):
        """Clean up singleton after each test."""
        Singleton._instances.pop(Kernel, None)

    def test_instance_property_get_without_cache(self):
        """Test that get() without cache always calls provider."""
        obj = Mock()
        obj.__jsii_ref__ = ObjRef(ref="test-ref-123")

        # Mock provider response
        response = GetResponse(value="test-value")
        self.mock_provider.get.return_value = response

        # Call get twice without caching
        result1 = self.kernel.get(obj, "testProp", cache=False)
        result2 = self.kernel.get(obj, "testProp", cache=False)

        # Should call provider both times
        self.assertEqual(self.mock_provider.get.call_count, 2)
        self.assertEqual(result1, "test-value")
        self.assertEqual(result2, "test-value")

    def test_instance_property_get_with_cache(self):
        """Test that get() with cache only calls provider once."""
        obj = Mock()
        obj.__jsii_ref__ = ObjRef(ref="test-ref-123")

        # Mock provider response
        response = GetResponse(value="test-value")
        self.mock_provider.get.return_value = response

        # Call get twice with caching
        result1 = self.kernel.get(obj, "testProp", cache=True)
        result2 = self.kernel.get(obj, "testProp", cache=True)

        # Should only call provider once (second call uses cache)
        self.assertEqual(self.mock_provider.get.call_count, 1)
        self.assertEqual(result1, "test-value")
        self.assertEqual(result2, "test-value")

    def test_instance_property_cache_invalidation_on_set(self):
        """Test that set() invalidates the property cache."""
        obj = Mock()
        obj.__jsii_ref__ = ObjRef(ref="test-ref-123")

        # Mock provider responses
        get_response = GetResponse(value="old-value")
        set_response = SetResponse()
        self.mock_provider.get.return_value = get_response
        self.mock_provider.set.return_value = set_response

        # Get with cache
        result1 = self.kernel.get(obj, "testProp", cache=True)
        self.assertEqual(result1, "old-value")

        # Set new value (should invalidate cache)
        self.kernel.set(obj, "testProp", "new-value")

        # Mock new response
        get_response2 = GetResponse(value="new-value")
        self.mock_provider.get.return_value = get_response2

        # Get again with cache - should call provider because cache was invalidated
        result2 = self.kernel.get(obj, "testProp", cache=True)

        # Should have called get twice (once before set, once after)
        self.assertEqual(self.mock_provider.get.call_count, 2)
        self.assertEqual(result2, "new-value")

    def test_static_property_get_always_cached(self):
        """Test that sget() always caches static properties."""
        klass = Mock()
        klass.__jsii_type__ = "test.Type"

        # Mock provider response
        response = Mock()
        response.value = "static-value"
        self.mock_provider.sget.return_value = response

        # Call sget twice
        result1 = self.kernel.sget(klass, "STATIC_PROP")
        result2 = self.kernel.sget(klass, "STATIC_PROP")

        # Should only call provider once (always cached)
        self.assertEqual(self.mock_provider.sget.call_count, 1)
        self.assertEqual(result1, "static-value")
        self.assertEqual(result2, "static-value")

    def test_static_property_cache_invalidation_on_sset(self):
        """Test that sset() invalidates the static property cache."""
        klass = Mock()
        klass.__jsii_type__ = "test.Type"

        # Mock provider responses
        response1 = Mock()
        response1.value = "old-static-value"
        self.mock_provider.sget.return_value = response1

        # Get static property (gets cached)
        result1 = self.kernel.sget(klass, "STATIC_PROP")
        self.assertEqual(result1, "old-static-value")

        # Set new value (should invalidate cache)
        self.kernel.sset(klass, "STATIC_PROP", "new-static-value")

        # Mock new response
        response2 = Mock()
        response2.value = "new-static-value"
        self.mock_provider.sget.return_value = response2

        # Get again - should call provider because cache was invalidated
        result2 = self.kernel.sget(klass, "STATIC_PROP")

        # Should have called sget twice
        self.assertEqual(self.mock_provider.sget.call_count, 2)
        self.assertEqual(result2, "new-static-value")

    def test_cache_isolation_between_objects(self):
        """Test that cache entries are isolated between different objects."""
        obj1 = Mock()
        obj1.__jsii_ref__ = ObjRef(ref="ref-1")
        obj2 = Mock()
        obj2.__jsii_ref__ = ObjRef(ref="ref-2")

        # Mock responses
        response1 = GetResponse(value="value-1")
        response2 = GetResponse(value="value-2")
        self.mock_provider.get.side_effect = [response1, response2]

        # Get same property from different objects
        result1 = self.kernel.get(obj1, "prop", cache=True)
        result2 = self.kernel.get(obj2, "prop", cache=True)

        # Should call provider twice (different objects)
        self.assertEqual(self.mock_provider.get.call_count, 2)
        self.assertEqual(result1, "value-1")
        self.assertEqual(result2, "value-2")

    def test_cache_isolation_between_properties(self):
        """Test that cache entries are isolated between different properties."""
        obj = Mock()
        obj.__jsii_ref__ = ObjRef(ref="test-ref")

        # Mock responses
        response1 = GetResponse(value="value-prop1")
        response2 = GetResponse(value="value-prop2")
        self.mock_provider.get.side_effect = [response1, response2]

        # Get different properties from same object
        result1 = self.kernel.get(obj, "prop1", cache=True)
        result2 = self.kernel.get(obj, "prop2", cache=True)

        # Should call provider twice (different properties)
        self.assertEqual(self.mock_provider.get.call_count, 2)
        self.assertEqual(result1, "value-prop1")
        self.assertEqual(result2, "value-prop2")

    def test_set_only_invalidates_target_property(self):
        """Test that set() only invalidates the specific property, not others."""
        obj = Mock()
        obj.__jsii_ref__ = ObjRef(ref="test-ref-123")

        # Mock provider responses
        response_a = GetResponse(value="value-a")
        response_b = GetResponse(value="value-b")
        self.mock_provider.get.side_effect = [response_a, response_b]
        self.mock_provider.set.return_value = SetResponse()

        # Cache two properties
        self.kernel.get(obj, "propA", cache=True)
        self.kernel.get(obj, "propB", cache=True)
        self.assertEqual(self.mock_provider.get.call_count, 2)

        # Set propA (should only invalidate propA's cache)
        self.kernel.set(obj, "propA", "new-value-a")

        # propB should still be cached (no provider call)
        result_b = self.kernel.get(obj, "propB", cache=True)
        self.assertEqual(self.mock_provider.get.call_count, 2)  # unchanged
        self.assertEqual(result_b, "value-b")

    def test_cache_invalidation_on_delete(self):
        """Test that delete() invalidates all cached properties for the object."""
        obj = Mock()
        obj.__jsii_ref__ = ObjRef(ref="test-ref-123")

        # Mock provider response
        response = GetResponse(value="cached-value")
        self.mock_provider.get.return_value = response

        # Cache a property
        self.kernel.get(obj, "prop1", cache=True)
        self.kernel.get(obj, "prop2", cache=True)
        self.assertEqual(self.mock_provider.get.call_count, 2)

        # Delete the object
        self.kernel.delete(obj.__jsii_ref__)

        # Mock new response
        response2 = GetResponse(value="new-value")
        self.mock_provider.get.return_value = response2

        # Get again - should call provider because cache was invalidated by delete
        result = self.kernel.get(obj, "prop1", cache=True)
        self.assertEqual(self.mock_provider.get.call_count, 3)
        self.assertEqual(result, "new-value")


if __name__ == "__main__":
    unittest.main()
