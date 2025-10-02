# Troubleshooting NamespaceClass Operator

This guide helps operators diagnose and fix issues with NamespaceClass bindings.

## Quick Health Check

### Check if a binding is healthy

```bash
# Check the Ready condition status
kubectl get namespaceclassbinding <binding-name> -n <namespace> \
  -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}'
```

**Expected output**: `True`

### Get detailed status

```bash
kubectl describe namespaceclassbinding <binding-name> -n <namespace>
```

Look for the `Conditions:` section which shows:
- **Status**: True/False (is the binding working?)
- **Reason**: Machine-readable error code
- **Message**: Human-readable explanation
- **Last Transition Time**: When this condition changed

## Status Conditions

NamespaceClassBinding uses Kubernetes standard conditions to report health.

### Ready Condition

The `Ready` condition indicates whether the binding successfully applied all resources from the NamespaceClass.

| Status | Reason | Meaning |
|--------|--------|---------|
| `True` | `ReconcileSuccess` | All resources applied successfully |
| `False` | `ClassNotFound` | Referenced NamespaceClass doesn't exist |
| `False` | `PruneFailed` | Failed to delete old resources |
| `False` | `ApplyFailed` | Failed to apply resources to namespace |

## Common Issues

### 1. NamespaceClass Not Found

**Symptom**:
```yaml
conditions:
- type: Ready
  status: "False"
  reason: ClassNotFound
  message: NamespaceClass foo not found
```

**Cause**: The namespace references a NamespaceClass that doesn't exist.

**Fix**:
```bash
# Check if the class exists
kubectl get namespaceclass <class-name>

# Either create the missing class or update the namespace label
kubectl label namespace <namespace> namespaceclass.akuity.io/name=<correct-class>
```

### 2. Resource Apply Failed

**Symptom**:
```yaml
conditions:
- type: Ready
  status: "False"
  reason: ApplyFailed
  message: "Failed to apply resources: ..."
```

**Cause**: The operator couldn't create/update one or more resources in the namespace.

**Common reasons**:
- Insufficient RBAC permissions
- Resource quota exceeded
- Invalid resource definition in NamespaceClass
- Namespace not ready/being deleted

**Fix**:
```bash
# Check operator logs for details
kubectl logs -n namespaceclass-operator-system \
  deployment/namespaceclass-operator-controller-manager

# Check resource quotas
kubectl get resourcequota -n <namespace>

# Verify the NamespaceClass definition
kubectl get namespaceclass <class-name> -o yaml
```

### 3. Prune Failed

**Symptom**:
```yaml
conditions:
- type: Ready
  status: "False"
  reason: PruneFailed
  message: "Failed to prune resources: ..."
```

**Cause**: The operator couldn't delete old resources that are no longer in the NamespaceClass.

**Common reasons**:
- Resource has finalizers preventing deletion
- Resource is protected by admission controller
- Insufficient RBAC permissions

**Fix**:
```bash
# Check which resources the binding thinks it created
kubectl get namespaceclassbinding <binding-name> -n <namespace> \
  -o jsonpath='{.status.appliedResources}'

# Check if those resources exist
kubectl get <resource-type> -n <namespace>

# Check for finalizers on stuck resources
kubectl get <resource-type> <resource-name> -n <namespace> -o yaml | grep -A 5 finalizers
```

## Monitoring Best Practices

### 1. Alert on Unhealthy Bindings

Set up alerts when bindings are not Ready:

```bash
# Get all bindings that aren't Ready
kubectl get namespaceclassbinding -A \
  -o json | jq -r '
    .items[] |
    select(.status.conditions[]? | select(.type=="Ready" and .status!="True")) |
    "\(.metadata.namespace)/\(.metadata.name): \(.status.conditions[] | select(.type=="Ready") | .message)"
  '
```

### 2. Check Binding Status for All Namespaces

```bash
# List all bindings with their Ready status
kubectl get namespaceclassbinding -A \
  -o custom-columns=\
NAMESPACE:.metadata.namespace,\
NAME:.metadata.name,\
CLASS:.spec.className,\
READY:.status.conditions[?(@.type==\"Ready\")].status,\
REASON:.status.conditions[?(@.type==\"Ready\")].reason
```

### 3. Watch for Changes

```bash
# Watch binding conditions in real-time
kubectl get namespaceclassbinding -A --watch \
  -o custom-columns=\
NAMESPACE:.metadata.namespace,\
NAME:.metadata.name,\
READY:.status.conditions[?(@.type==\"Ready\")].status,\
MESSAGE:.status.conditions[?(@.type==\"Ready\")].message
```

## Debugging Steps

### Step 1: Check the Binding

```bash
kubectl get namespaceclassbinding <binding-name> -n <namespace> -o yaml
```

Look for:
- `status.conditions`: Is Ready=True?
- `status.observedClassName`: Does it match the namespace label?
- `status.observedClassGeneration`: Is it current?
- `status.appliedResources`: What resources were created?

### Step 2: Check the NamespaceClass

```bash
kubectl get namespaceclass <class-name> -o yaml
```

Verify:
- The class exists
- Resources are valid Kubernetes manifests
- Resource names are unique

### Step 3: Check Controller Logs

```bash
kubectl logs -n namespaceclass-operator-system \
  deployment/namespaceclass-operator-controller-manager \
  --tail=100 --follow
```

Look for errors mentioning your namespace or binding.

### Step 4: Check Created Resources

```bash
# List resources that should have been created
kubectl get namespaceclassbinding <binding-name> -n <namespace> \
  -o jsonpath='{.status.appliedResources[*].kind}' | tr ' ' '\n' | sort -u

# Check if they actually exist
kubectl get all,configmap,secret,serviceaccount -n <namespace>

# Check owner references (should point to binding)
kubectl get <resource-type> <resource-name> -n <namespace> \
  -o jsonpath='{.metadata.ownerReferences}'
```

### Step 5: Force Reconciliation

Sometimes triggering a reconciliation helps:

```bash
# Update the binding to trigger reconciliation
kubectl annotate namespaceclassbinding <binding-name> -n <namespace> \
  reconcile.akuity.io/trigger="$(date +%s)"
```

## Events

In addition to conditions, the operator emits Kubernetes events:

```bash
# See recent events for the binding
kubectl describe namespaceclassbinding <binding-name> -n <namespace> | grep -A 20 Events:

# See all operator events in the namespace
kubectl get events -n <namespace> \
  --field-selector involvedObject.kind=NamespaceClassBinding
```

## Getting Help

If you're still stuck after following this guide:

1. Collect diagnostics:
   ```bash
   kubectl get namespaceclass <class-name> -o yaml > class.yaml
   kubectl get namespaceclassbinding <binding-name> -n <namespace> -o yaml > binding.yaml
   kubectl get events -n <namespace> > events.txt
   kubectl logs -n namespaceclass-operator-system \
     deployment/namespaceclass-operator-controller-manager \
     --tail=200 > operator.log
   ```

2. Check the [GitHub Issues](https://github.com/jacobboykin/namespaceclass-operator/issues)

3. Include the collected files when opening an issue
