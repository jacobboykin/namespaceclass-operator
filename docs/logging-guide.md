# Logging Guide

The NamespaceClass operator uses structured logging with consistent key-value pairs for easy filtering, monitoring, and debugging.

## Log Structure

All logs include structured fields (JSON-formatted key-value pairs) that enable powerful filtering:

```json
{
  "level": "info",
  "msg": "reconciling binding",
  "binding": "my-app",
  "namespace": "my-app",
  "className": "production",
  "generation": 3,
  "resourceCount": 5,
  "previousGeneration": 2
}
```

## Standard Fields

### Always Present

| Field | Description | Example |
|-------|-------------|---------|
| `controller` | Which controller logged this | `namespaceclassbinding` |
| `binding` | Binding name | `my-app` |
| `namespace` | Namespace name | `my-app` |

### Context-Specific

| Field | Description | Example | When Present |
|-------|-------------|---------|--------------|
| `className` | NamespaceClass being applied | `production` | After class is loaded |
| `generation` | NamespaceClass version | `3` | During reconciliation |
| `previousGeneration` | Previous class version | `2` | When updating |
| `resourceCount` | Number of resources in class | `5` | During reconciliation |
| `appliedResourceCount` | Resources successfully applied | `5` | After apply completes |
| `apiVersion` | Resource API version | `v1` | Per-resource operations |
| `kind` | Resource kind | `ConfigMap` | Per-resource operations |
| `name` | Resource name | `app-config` | Per-resource operations |

## Filtering Logs

### By Namespace

```bash
kubectl logs -n namespaceclass-operator-system \
  deployment/namespaceclass-operator-controller-manager \
  | grep 'namespace.*my-namespace'
```

### By NamespaceClass

```bash
kubectl logs -n namespaceclass-operator-system \
  deployment/namespaceclass-operator-controller-manager \
  | grep 'className.*production'
```

### By Binding

```bash
kubectl logs -n namespaceclass-operator-system \
  deployment/namespaceclass-operator-controller-manager \
  | grep 'binding.*my-app'
```

### By Resource Type

```bash
# All ConfigMap operations
kubectl logs -n namespaceclass-operator-system \
  deployment/namespaceclass-operator-controller-manager \
  | grep 'kind.*ConfigMap'
```

### Errors Only

```bash
kubectl logs -n namespaceclass-operator-system \
  deployment/namespaceclass-operator-controller-manager \
  | grep ERROR
```

## Using jq for Advanced Filtering

If your logs are in JSON format, you can use `jq`:

```bash
# Get all logs for a specific namespace
kubectl logs -n namespaceclass-operator-system \
  deployment/namespaceclass-operator-controller-manager \
  --tail=1000 | jq 'select(.namespace=="my-app")'

# Get all resource applications for a class
kubectl logs -n namespaceclass-operator-system \
  deployment/namespaceclass-operator-controller-manager \
  --tail=1000 | jq 'select(.className=="production" and .msg=="applied resource")'

# Count reconciliations by class
kubectl logs -n namespaceclass-operator-system \
  deployment/namespaceclass-operator-controller-manager \
  --tail=1000 | jq -s 'group_by(.className) | map({class: .[0].className, count: length})'
```

## Common Log Patterns

### Successful Reconciliation

```
INFO  reconciling binding  binding=my-app namespace=my-app className=production generation=3 resourceCount=5
INFO  applied resource     binding=my-app namespace=my-app className=production kind=ConfigMap name=app-config
INFO  applied resource     binding=my-app namespace=my-app className=production kind=Secret name=app-secret
INFO  successfully reconciled binding  binding=my-app namespace=my-app appliedResourceCount=5
```

**What it means**: Binding successfully applied 5 resources from the NamespaceClass.

### Skipped Reconciliation

```
DEBUG binding up to date, skipping reconciliation  binding=my-app namespace=my-app className=production generation=3
```

**What it means**: NamespaceClass hasn't changed (generation matches), so reconciliation was skipped for efficiency.

### Resource Pruning

```
INFO  reconciling binding  binding=my-app namespace=my-app className=production generation=4 resourceCount=3 previousGeneration=3
INFO  pruning resource     binding=my-app namespace=my-app className=production kind=ConfigMap name=old-config
INFO  applied resource     binding=my-app namespace=my-app className=production kind=ConfigMap name=new-config
```

**What it means**: NamespaceClass was updated (generation 3→4), old resources removed, new resources added.

### Class Not Found

```
INFO  NamespaceClass not found, deleting binding  binding=my-app namespace=my-app className=missing-class
```

**What it means**: Referenced NamespaceClass doesn't exist, so the binding (and its resources) are being cleaned up.

### Apply Failed

```
ERROR failed to apply resources  binding=my-app namespace=my-app className=production error="apply ConfigMap/bad-config: Invalid..."
```

**What it means**: Controller couldn't create/update a resource. Check the error message for details.

## Log Levels

The operator uses standard log levels:

| Level | Usage | Example |
|-------|-------|---------|
| `ERROR` | Something failed | Failed to apply resource |
| `INFO` | Normal operations | Applied resource, reconciled binding |
| `DEBUG` | Verbose details | Skipping reconciliation (up to date) |

### Changing Log Verbosity

Increase verbosity to see `DEBUG` logs:

```bash
# Edit the operator deployment
kubectl edit deployment -n namespaceclass-operator-system \
  namespaceclass-operator-controller-manager

# Add to container args:
args:
  - --zap-log-level=debug
```

Or use log level flags (`V(1)` logs require verbosity >= 1):

```bash
args:
  - --v=1  # See V(1) debug logs
```

## Monitoring and Alerting

### Track Reconciliation Rate

```bash
# Count reconciliations in last 100 logs
kubectl logs -n namespaceclass-operator-system \
  deployment/namespaceclass-operator-controller-manager \
  --tail=100 | grep -c "reconciling binding"
```

### Track Error Rate

```bash
# Count errors in last 100 logs
kubectl logs -n namespaceclass-operator-system \
  deployment/namespaceclass-operator-controller-manager \
  --tail=100 | grep -c "ERROR"
```

### Alert on Specific Errors

```bash
# Watch for apply failures
kubectl logs -n namespaceclass-operator-system \
  deployment/namespaceclass-operator-controller-manager \
  --follow | grep "failed to apply resources"
```

## Integration with Log Aggregators

### Prometheus/Loki

The structured fields make it easy to create Prometheus/Loki queries:

```promql
# Count errors by namespace
count_over_time({namespace="namespaceclass-operator-system"} |= "ERROR" | json | namespace != "" [5m]) by (namespace)

# Rate of reconciliations by className
rate({namespace="namespaceclass-operator-system"} |= "reconciling binding" | json [5m]) by (className)

# Failed applies
{namespace="namespaceclass-operator-system"} |= "failed to apply resources" | json
```

### Elasticsearch/Kibana

Use field-based queries:

```
controller:"namespaceclassbinding" AND level:"error"
className:"production" AND msg:"applied resource"
namespace:"my-app" AND msg:"failed to apply resources"
```

### Datadog

Create log monitors using facets:

```
@controller:namespaceclassbinding @level:error
@className:production @msg:"applied resource"
@namespace:my-app @msg:"failed to apply resources"
```

## Debugging Workflow

When troubleshooting an issue:

1. **Get recent logs for the namespace**:
   ```bash
   kubectl logs -n namespaceclass-operator-system \
     deployment/namespaceclass-operator-controller-manager \
     --tail=200 | grep "namespace.*my-app"
   ```

2. **Look for the latest reconciliation**:
   ```bash
   kubectl logs -n namespaceclass-operator-system \
     deployment/namespaceclass-operator-controller-manager \
     --tail=200 | grep "namespace.*my-app" | grep "reconciling binding"
   ```

3. **Check for errors**:
   ```bash
   kubectl logs -n namespaceclass-operator-system \
     deployment/namespaceclass-operator-controller-manager \
     --tail=200 | grep "namespace.*my-app" | grep ERROR
   ```

4. **Verify resource operations**:
   ```bash
   kubectl logs -n namespaceclass-operator-system \
     deployment/namespaceclass-operator-controller-manager \
     --tail=200 | grep "namespace.*my-app" | grep "applied resource\|pruning resource"
   ```

5. **Cross-reference with conditions**:
   ```bash
   kubectl get namespaceclassbinding my-app -n my-app \
     -o jsonpath='{.status.conditions[?(@.type=="Ready")]}'
   ```

## Best Practices

1. **Use structured filtering**: Don't parse log messages as strings - use the structured fields
2. **Monitor error rates**: Set up alerts when error rates spike
3. **Track reconciliation patterns**: Frequent reconciliations may indicate thrashing
4. **Correlate with conditions**: Logs show why, conditions show what
5. **Keep logs short-term**: Use conditions for long-term state, logs for debugging

## Example Queries

### Find all bindings that reconciled in the last hour

```bash
kubectl logs -n namespaceclass-operator-system \
  deployment/namespaceclass-operator-controller-manager \
  --since=1h | grep "successfully reconciled binding" | \
  awk '{for(i=1;i<=NF;i++){if($i~/binding=/){print $i}}}' | \
  sort -u
```

### Count resources applied per namespace

```bash
kubectl logs -n namespaceclass-operator-system \
  deployment/namespaceclass-operator-controller-manager \
  --tail=500 | grep "applied resource" | \
  awk '{for(i=1;i<=NF;i++){if($i~/namespace=/){print $i}}}' | \
  sort | uniq -c
```

### See what changed in the last update

```bash
# Look for pruning and applying
kubectl logs -n namespaceclass-operator-system \
  deployment/namespaceclass-operator-controller-manager \
  --tail=100 | grep "namespace.*my-app" | \
  grep -E "pruning resource|applied resource"
```
