package e2e

import (
	"strings"
	"testing"
	"time"
)

// NOTE: Tests are run serially (no t.Parallel()) since they share a cluster.
// This prevents resource conflicts and ensures consistent test behavior.

// TestPruningResourceRemoval tests that removing a resource from a NamespaceClass
// deletes it from namespaces (not just additive updates)
func TestPruningResourceRemoval(t *testing.T) {
	namespaceclassName := generateUniqueName("test-nc")
	namespaceName := generateUniqueName("test-ns")

	// Clean up after test
	defer kubectlDelete(t, "namespaceclass", namespaceclassName)
	defer kubectlDelete(t, "namespace", namespaceName)

	// Create NamespaceClass with TWO resources
	namespaceclassYAML := `apiVersion: akuity.io/v1alpha1
kind: NamespaceClass
metadata:
  name: ` + namespaceclassName + `
spec:
  resources:
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: keep-configmap
      data:
        key: keep
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: remove-configmap
      data:
        key: remove`

	kubectlApply(t, namespaceclassYAML)
	waitForResource(t, "namespaceclass", namespaceclassName, "", 30*time.Second)

	// Create namespace with NamespaceClass
	namespaceYAML := `apiVersion: v1
kind: Namespace
metadata:
  name: ` + namespaceName + `
  labels:
    namespaceclass.akuity.io/name: ` + namespaceclassName

	kubectlApply(t, namespaceYAML)
	waitForResource(t, "namespace", namespaceName, "", 30*time.Second)

	// Wait for both ConfigMaps to be created
	waitForResource(t, "configmap", "keep-configmap", namespaceName, 90*time.Second)
	waitForResource(t, "configmap", "remove-configmap", namespaceName, 90*time.Second)

	// Update NamespaceClass to REMOVE one resource
	updatedNamespaceclassYAML := `apiVersion: akuity.io/v1alpha1
kind: NamespaceClass
metadata:
  name: ` + namespaceclassName + `
spec:
  resources:
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: keep-configmap
      data:
        key: keep`

	kubectlApply(t, updatedNamespaceclassYAML)

	// Wait for the removed ConfigMap to be deleted
	waitForResourceDeletion(t, "configmap", "remove-configmap", namespaceName, 90*time.Second)

	// Verify the kept ConfigMap still exists
	kubectl(t, "get", "configmap", "keep-configmap", "-n", namespaceName)
}

// TestFieldUpdateReconciliation tests that changes to resource fields
// are reconciled (drift repair)
func TestFieldUpdateReconciliation(t *testing.T) {
	namespaceclassName := generateUniqueName("test-nc")
	namespaceName := generateUniqueName("test-ns")

	// Clean up after test
	defer kubectlDelete(t, "namespaceclass", namespaceclassName)
	defer kubectlDelete(t, "namespace", namespaceName)

	// Create NamespaceClass with a ConfigMap
	namespaceclassYAML := `apiVersion: akuity.io/v1alpha1
kind: NamespaceClass
metadata:
  name: ` + namespaceclassName + `
spec:
  resources:
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: test-configmap
      data:
        key: original-value`

	kubectlApply(t, namespaceclassYAML)
	waitForResource(t, "namespaceclass", namespaceclassName, "", 30*time.Second)

	// Create namespace with NamespaceClass
	namespaceYAML := `apiVersion: v1
kind: Namespace
metadata:
  name: ` + namespaceName + `
  labels:
    namespaceclass.akuity.io/name: ` + namespaceclassName

	kubectlApply(t, namespaceYAML)
	waitForResource(t, "namespace", namespaceName, "", 30*time.Second)

	// Wait for ConfigMap to be created with original value
	waitForResource(t, "configmap", "test-configmap", namespaceName, 90*time.Second)

	// Verify original value
	originalData := getConfigMapData(t, "test-configmap", namespaceName)
	if originalData["key"] != "original-value" {
		t.Fatalf("expected original-value, got %s", originalData["key"])
	}

	// Update NamespaceClass to change the ConfigMap data
	updatedNamespaceclassYAML := `apiVersion: akuity.io/v1alpha1
kind: NamespaceClass
metadata:
  name: ` + namespaceclassName + `
spec:
  resources:
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: test-configmap
      data:
        key: updated-value`

	kubectlApply(t, updatedNamespaceclassYAML)

	// Wait for the ConfigMap data to be updated
	waitForConfigMapData(t, "test-configmap", namespaceName, "key", "updated-value", 90*time.Second)
}

// TestOverDeletionGuard tests that non-managed resources survive when
// the label is removed or class is deleted
func TestOverDeletionGuard(t *testing.T) {
	namespaceclassName := generateUniqueName("test-nc")
	namespaceName := generateUniqueName("test-ns")

	// Clean up after test
	defer kubectlDelete(t, "namespaceclass", namespaceclassName)
	defer kubectlDelete(t, "namespace", namespaceName)

	// Create namespace first without any class
	namespaceYAML := `apiVersion: v1
kind: Namespace
metadata:
  name: ` + namespaceName

	kubectlApply(t, namespaceYAML)
	waitForResource(t, "namespace", namespaceName, "", 30*time.Second)

	// Create an unmanaged ConfigMap directly in the namespace
	unmanagedConfigMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: unmanaged-configmap
  namespace: ` + namespaceName + `
data:
  key: unmanaged`

	kubectlApply(t, unmanagedConfigMapYAML)
	waitForResource(t, "configmap", "unmanaged-configmap", namespaceName, 30*time.Second)

	// Create NamespaceClass
	namespaceclassYAML := `apiVersion: akuity.io/v1alpha1
kind: NamespaceClass
metadata:
  name: ` + namespaceclassName + `
spec:
  resources:
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: managed-configmap
      data:
        key: managed`

	kubectlApply(t, namespaceclassYAML)
	waitForResource(t, "namespaceclass", namespaceclassName, "", 30*time.Second)

	// Add the NamespaceClass label to the namespace
	kubectl(t, "label", "namespace", namespaceName, "namespaceclass.akuity.io/name="+namespaceclassName)

	// Wait for managed ConfigMap to be created
	waitForResource(t, "configmap", "managed-configmap", namespaceName, 90*time.Second)

	// Verify both ConfigMaps exist
	kubectl(t, "get", "configmap", "unmanaged-configmap", "-n", namespaceName)
	kubectl(t, "get", "configmap", "managed-configmap", "-n", namespaceName)

	// Test 1: Remove the label and verify only managed resources are deleted
	kubectl(t, "label", "namespace", namespaceName, "namespaceclass.akuity.io/name-")

	// Allow time for event propagation and cache sync
	time.Sleep(2 * time.Second)

	// Wait for managed ConfigMap to be deleted
	waitForResourceDeletion(t, "configmap", "managed-configmap", namespaceName, 90*time.Second)

	// Verify unmanaged ConfigMap still exists after label removal
	kubectl(t, "get", "configmap", "unmanaged-configmap", "-n", namespaceName)

	// Test 2: Re-apply the class and then delete the NamespaceClass itself
	kubectl(t, "label", "namespace", namespaceName, "namespaceclass.akuity.io/name="+namespaceclassName)
	waitForResource(t, "configmap", "managed-configmap", namespaceName, 90*time.Second)

	// Delete the NamespaceClass
	kubectlDelete(t, "namespaceclass", namespaceclassName)

	// Wait for managed ConfigMap to be deleted again
	waitForResourceDeletion(t, "configmap", "managed-configmap", namespaceName, 90*time.Second)

	// Verify unmanaged ConfigMap still exists after class deletion
	kubectl(t, "get", "configmap", "unmanaged-configmap", "-n", namespaceName)
}

// TestProvenanceMarkers tests that created resources have proper ownership markers
func TestProvenanceMarkers(t *testing.T) {
	namespaceclassName := generateUniqueName("test-nc")
	namespaceName := generateUniqueName("test-ns")

	// Clean up after test
	defer kubectlDelete(t, "namespaceclass", namespaceclassName)
	defer kubectlDelete(t, "namespace", namespaceName)

	// Create NamespaceClass
	namespaceclassYAML := `apiVersion: akuity.io/v1alpha1
kind: NamespaceClass
metadata:
  name: ` + namespaceclassName + `
spec:
  resources:
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: test-configmap
      data:
        key: value`

	kubectlApply(t, namespaceclassYAML)
	waitForResource(t, "namespaceclass", namespaceclassName, "", 30*time.Second)

	// Create namespace with NamespaceClass
	namespaceYAML := `apiVersion: v1
kind: Namespace
metadata:
  name: ` + namespaceName + `
  labels:
    namespaceclass.akuity.io/name: ` + namespaceclassName

	kubectlApply(t, namespaceYAML)
	waitForResource(t, "namespace", namespaceName, "", 30*time.Second)

	// Wait for ConfigMap to be created
	waitForResource(t, "configmap", "test-configmap", namespaceName, 90*time.Second)

	// Check that the ConfigMap has an ownerReference with controller=true
	// Accept either NamespaceClassBinding or Namespace as the controller owner
	ownerRefs := getOwnerRefs(t, "configmap", "test-configmap", namespaceName)
	if len(ownerRefs) == 0 {
		t.Fatal("expected ConfigMap to have ownerReferences")
	}

	foundControllerOwner := false
	var controllerKind string
	for _, ref := range ownerRefs {
		if controller, ok := ref["controller"].(bool); ok && controller {
			foundControllerOwner = true
			if kind, ok := ref["kind"].(string); ok {
				controllerKind = kind
			}
			break
		}
	}

	if !foundControllerOwner {
		t.Fatalf("expected ConfigMap to have a controller ownerReference, got: %v", ownerRefs)
	}

	// Verify the controller owner kind is either NamespaceClassBinding or Namespace
	if controllerKind != "NamespaceClassBinding" && controllerKind != "Namespace" {
		t.Logf("Warning: controller owner kind is %q, expected NamespaceClassBinding or Namespace", controllerKind)
	}

	// If it's a NamespaceClassBinding, verify it exists
	if controllerKind == "NamespaceClassBinding" {
		kubectl(t, "get", "namespaceclassbinding", namespaceName, "-n", namespaceName)
	}
}

// TestIdempotency tests that re-applying the same class doesn't create duplicates
func TestIdempotency(t *testing.T) {
	namespaceclassName := generateUniqueName("test-nc")
	namespaceName := generateUniqueName("test-ns")

	// Clean up after test
	defer kubectlDelete(t, "namespaceclass", namespaceclassName)
	defer kubectlDelete(t, "namespace", namespaceName)

	// Create NamespaceClass
	namespaceclassYAML := `apiVersion: akuity.io/v1alpha1
kind: NamespaceClass
metadata:
  name: ` + namespaceclassName + `
spec:
  resources:
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: test-configmap
      data:
        key: value`

	kubectlApply(t, namespaceclassYAML)
	waitForResource(t, "namespaceclass", namespaceclassName, "", 30*time.Second)

	// Create namespace with NamespaceClass
	namespaceYAML := `apiVersion: v1
kind: Namespace
metadata:
  name: ` + namespaceName + `
  labels:
    namespaceclass.akuity.io/name: ` + namespaceclassName

	kubectlApply(t, namespaceYAML)
	waitForResource(t, "namespace", namespaceName, "", 30*time.Second)

	// Wait for ConfigMap to be created
	waitForResource(t, "configmap", "test-configmap", namespaceName, 90*time.Second)

	// Get the initial UID
	initialUID := getResourceUID(t, "configmap", "test-configmap", namespaceName)

	// Re-apply the same NamespaceClass (should be a no-op)
	kubectlApply(t, namespaceclassYAML)

	// Give the controller time to reconcile
	time.Sleep(5 * time.Second)

	// Verify the ConfigMap still exists with the same UID (not recreated)
	currentUID := getResourceUID(t, "configmap", "test-configmap", namespaceName)
	if currentUID != initialUID {
		t.Fatalf("ConfigMap was recreated (UID changed from %s to %s), expected idempotent behavior", initialUID, currentUID)
	}

	// Verify there's exactly one ConfigMap with this name
	out := kubectl(t, "get", "configmap", "test-configmap", "-n", namespaceName, "-o", "name")
	if strings.TrimSpace(out) != "configmap/test-configmap" {
		t.Fatalf("expected exactly one configmap/test-configmap, got %q", out)
	}
}
