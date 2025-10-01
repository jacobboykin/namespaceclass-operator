package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// kubectl executes a kubectl command and returns the output
func kubectl(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("kubectl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("kubectl %v failed: %v\nstdout: %s\nstderr: %s", args, err, stdout.String(), stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}

// kubectlApply applies a YAML string to the cluster
func kubectlApply(t *testing.T, yaml string) {
	t.Helper()
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(yaml)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("kubectl apply failed: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
}

// kubectlDelete deletes a resource
func kubectlDelete(t *testing.T, resourceType, name string) {
	t.Helper()
	cmd := exec.Command("kubectl", "delete", resourceType, name, "--ignore-not-found=true")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("kubectl delete %s %s failed: %v\nstdout: %s\nstderr: %s", resourceType, name, err, stdout.String(), stderr.String())
	}
}

// waitForResource waits for a resource to exist with a timeout
func waitForResource(t *testing.T, resourceType, name, namespace string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)

	args := []string{"get", resourceType, name}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	for time.Now().Before(deadline) {
		cmd := exec.Command("kubectl", args...)
		if err := cmd.Run(); err == nil {
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("timeout waiting for %s %s in namespace %s", resourceType, name, namespace)
}

// waitForResourceDeletion waits for a resource to be deleted with a timeout
func waitForResourceDeletion(t *testing.T, resourceType, name, namespace string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)

	args := []string{"get", resourceType, name}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	for time.Now().Before(deadline) {
		cmd := exec.Command("kubectl", args...)
		if err := cmd.Run(); err != nil {
			// Resource not found, deletion complete
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("timeout waiting for deletion of %s %s in namespace %s", resourceType, name, namespace)
}

// generateUniqueName generates a unique name for test resources
func generateUniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// getOwnerRefs retrieves the ownerReferences from a resource
func getOwnerRefs(t *testing.T, kind, name, namespace string) []map[string]interface{} {
	t.Helper()
	out := kubectl(t, "get", kind, name, "-n", namespace, "-o", "json")
	var obj struct {
		Metadata struct {
			OwnerReferences []map[string]interface{} `json:"ownerReferences"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("unmarshal %s ownerRefs: %v", kind, err)
	}
	return obj.Metadata.OwnerReferences
}

// getConfigMapData retrieves the data field from a ConfigMap
func getConfigMapData(t *testing.T, name, namespace string) map[string]string {
	t.Helper()
	out := kubectl(t, "get", "configmap", name, "-n", namespace, "-o", "json")
	var obj struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("unmarshal configmap json: %v", err)
	}
	return obj.Data
}

// getResourceUID retrieves the UID of a resource
func getResourceUID(t *testing.T, resourceType, name, namespace string) string {
	t.Helper()
	out := kubectl(t, "get", resourceType, name, "-n", namespace, "-o", "json")
	var obj struct {
		Metadata struct {
			UID string `json:"uid"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("unmarshal %s uid: %v", resourceType, err)
	}
	return obj.Metadata.UID
}

// waitForConfigMapData waits for a ConfigMap's data field to have a specific value
func waitForConfigMapData(t *testing.T, name, namespace, key, expectedValue string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		data := getConfigMapData(t, name, namespace)
		if data[key] == expectedValue {
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("timeout waiting for ConfigMap %s data[%s] to be %s", name, key, expectedValue)
}
