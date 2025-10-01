package e2e

import (
	"testing"
	"time"
)

// NOTE: Tests are run serially (no t.Parallel()) since they share a cluster.
// This prevents resource conflicts and ensures consistent test behavior.

// TestDefineNamespaceClass tests that a NamespaceClass can be defined and stored
func TestDefineNamespaceClass(t *testing.T) {
	namespaceclassName := generateUniqueName("test-nc")

	// Clean up after test
	defer kubectlDelete(t, "namespaceclass", namespaceclassName)

	// Define a NamespaceClass with some test resources
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
        key: value
    - apiVersion: v1
      kind: Secret
      metadata:
        name: test-secret
      type: Opaque
      stringData:
        password: secret123`

	// Apply the NamespaceClass
	kubectlApply(t, namespaceclassYAML)

	// Verify the NamespaceClass exists
	waitForResource(t, "namespaceclass", namespaceclassName, "", 30*time.Second)

	// Verify we can retrieve it
	output := kubectl(t, "get", "namespaceclass", namespaceclassName, "-o", "name")
	if output != "namespaceclass.akuity.io/"+namespaceclassName {
		t.Fatalf("expected NamespaceClass to be retrievable, got: %s", output)
	}
}

// TestApplyNamespaceClassToNamespace tests applying a NamespaceClass to a namespace
func TestApplyNamespaceClassToNamespace(t *testing.T) {
	namespaceclassName := generateUniqueName("test-nc")
	namespaceName := generateUniqueName("test-ns")

	// Clean up after test
	defer kubectlDelete(t, "namespaceclass", namespaceclassName)
	defer kubectlDelete(t, "namespace", namespaceName)

	// Create a NamespaceClass
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
        key: value
    - apiVersion: v1
      kind: Secret
      metadata:
        name: test-secret
      type: Opaque
      stringData:
        password: secret123`

	kubectlApply(t, namespaceclassYAML)
	waitForResource(t, "namespaceclass", namespaceclassName, "", 30*time.Second)

	// Create a namespace with the NamespaceClass label
	namespaceYAML := `apiVersion: v1
kind: Namespace
metadata:
  name: ` + namespaceName + `
  labels:
    namespaceclass.akuity.io/name: ` + namespaceclassName

	kubectlApply(t, namespaceYAML)
	waitForResource(t, "namespace", namespaceName, "", 30*time.Second)

	// Wait for resources to be created in the namespace
	waitForResource(t, "configmap", "test-configmap", namespaceName, 90*time.Second)
	waitForResource(t, "secret", "test-secret", namespaceName, 90*time.Second)

	// Verify the resources exist
	kubectl(t, "get", "configmap", "test-configmap", "-n", namespaceName)
	kubectl(t, "get", "secret", "test-secret", "-n", namespaceName)
}

// TestRemoveNamespaceClassLabel tests removing a NamespaceClass label from a namespace
func TestRemoveNamespaceClassLabel(t *testing.T) {
	namespaceclassName := generateUniqueName("test-nc")
	namespaceName := generateUniqueName("test-ns")

	// Clean up after test
	defer kubectlDelete(t, "namespaceclass", namespaceclassName)
	defer kubectlDelete(t, "namespace", namespaceName)

	// Create a NamespaceClass
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

	// Create a namespace with the NamespaceClass label
	namespaceYAML := `apiVersion: v1
kind: Namespace
metadata:
  name: ` + namespaceName + `
  labels:
    namespaceclass.akuity.io/name: ` + namespaceclassName

	kubectlApply(t, namespaceYAML)
	waitForResource(t, "namespace", namespaceName, "", 30*time.Second)

	// Wait for the ConfigMap to be created
	waitForResource(t, "configmap", "test-configmap", namespaceName, 90*time.Second)

	// Remove the label from the namespace
	kubectl(t, "label", "namespace", namespaceName, "namespaceclass.akuity.io/name-")

	// Allow time for event propagation and cache sync
	time.Sleep(2 * time.Second)

	// Wait for the ConfigMap to be deleted
	waitForResourceDeletion(t, "configmap", "test-configmap", namespaceName, 90*time.Second)
}

// TestSwitchNamespaceClass tests switching a namespace from one NamespaceClass to another
func TestSwitchNamespaceClass(t *testing.T) {
	namespaceclassName1 := generateUniqueName("test-nc-1")
	namespaceclassName2 := generateUniqueName("test-nc-2")
	namespaceName := generateUniqueName("test-ns")

	// Clean up after test
	defer kubectlDelete(t, "namespaceclass", namespaceclassName1)
	defer kubectlDelete(t, "namespaceclass", namespaceclassName2)
	defer kubectlDelete(t, "namespace", namespaceName)

	// Create first NamespaceClass
	namespaceclassYAML1 := `apiVersion: akuity.io/v1alpha1
kind: NamespaceClass
metadata:
  name: ` + namespaceclassName1 + `
spec:
  resources:
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: configmap-class1
      data:
        class: "1"`

	kubectlApply(t, namespaceclassYAML1)
	waitForResource(t, "namespaceclass", namespaceclassName1, "", 30*time.Second)

	// Create second NamespaceClass
	namespaceclassYAML2 := `apiVersion: akuity.io/v1alpha1
kind: NamespaceClass
metadata:
  name: ` + namespaceclassName2 + `
spec:
  resources:
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: configmap-class2
      data:
        class: "2"`

	kubectlApply(t, namespaceclassYAML2)
	waitForResource(t, "namespaceclass", namespaceclassName2, "", 30*time.Second)

	// Create namespace with first NamespaceClass
	namespaceYAML := `apiVersion: v1
kind: Namespace
metadata:
  name: ` + namespaceName + `
  labels:
    namespaceclass.akuity.io/name: ` + namespaceclassName1

	kubectlApply(t, namespaceYAML)
	waitForResource(t, "namespace", namespaceName, "", 30*time.Second)

	// Wait for first ConfigMap to be created
	waitForResource(t, "configmap", "configmap-class1", namespaceName, 90*time.Second)

	// Switch to second NamespaceClass
	kubectl(t, "label", "namespace", namespaceName, "namespaceclass.akuity.io/name="+namespaceclassName2, "--overwrite")

	// Allow time for event propagation and cache sync
	time.Sleep(2 * time.Second)

	// Wait for first ConfigMap to be deleted
	waitForResourceDeletion(t, "configmap", "configmap-class1", namespaceName, 90*time.Second)

	// Wait for second ConfigMap to be created
	waitForResource(t, "configmap", "configmap-class2", namespaceName, 90*time.Second)

	// Verify only the second ConfigMap exists
	kubectl(t, "get", "configmap", "configmap-class2", "-n", namespaceName)
}

// TestUpdateNamespaceClass tests updating an existing NamespaceClass
func TestUpdateNamespaceClass(t *testing.T) {
	namespaceclassName := generateUniqueName("test-nc")
	namespaceName := generateUniqueName("test-ns")

	// Clean up after test
	defer kubectlDelete(t, "namespaceclass", namespaceclassName)
	defer kubectlDelete(t, "namespace", namespaceName)

	// Create initial NamespaceClass with one resource
	namespaceclassYAML := `apiVersion: akuity.io/v1alpha1
kind: NamespaceClass
metadata:
  name: ` + namespaceclassName + `
spec:
  resources:
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: configmap-1
      data:
        key: value1`

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

	// Wait for first ConfigMap to be created
	waitForResource(t, "configmap", "configmap-1", namespaceName, 90*time.Second)

	// Update NamespaceClass to add another resource
	updatedNamespaceclassYAML := `apiVersion: akuity.io/v1alpha1
kind: NamespaceClass
metadata:
  name: ` + namespaceclassName + `
spec:
  resources:
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: configmap-1
      data:
        key: value1
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: configmap-2
      data:
        key: value2`

	kubectlApply(t, updatedNamespaceclassYAML)

	// Wait for second ConfigMap to be created
	waitForResource(t, "configmap", "configmap-2", namespaceName, 90*time.Second)

	// Verify both ConfigMaps exist
	kubectl(t, "get", "configmap", "configmap-1", "-n", namespaceName)
	kubectl(t, "get", "configmap", "configmap-2", "-n", namespaceName)
}

// TestRemoveNamespaceClass tests removing a NamespaceClass that is in use
func TestRemoveNamespaceClass(t *testing.T) {
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

	// Delete the NamespaceClass
	kubectlDelete(t, "namespaceclass", namespaceclassName)

	// Wait for ConfigMap to be deleted
	waitForResourceDeletion(t, "configmap", "test-configmap", namespaceName, 90*time.Second)
}
