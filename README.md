# NamespaceClass Operator

A Kubernetes operator that automatically applies standardized resources to namespaces based on predefined templates called NamespaceClasses.

## How It Works

The NamespaceClass Operator provides a declarative way to manage common namespace-scoped resources across your Kubernetes cluster. It consists of two main components:

### NamespaceClass
A `NamespaceClass` defines a template containing a collection of Kubernetes resources that should be applied to namespaces. These can include:
- NetworkPolicies for security isolation
- ResourceQuotas for resource management
- LimitRanges for container resource limits
- ConfigMaps, Secrets, and other namespace-scoped resources

## Enhancements 

Things that could be added in the future to improve this project:

* More robust documentation and examples
* Enhanced RBAC support for finer-grained access control
* Proper Status.Conditions on NamespaceClassBinding resources to indicate sync status for each Namespace.
* Better CRD validation to ensure cluster-scoped resources are not included in NamespaceClass definitions
* Drift synchronization options to ensure resources remain in sync with NamespaceClass definitions
* Cleaner code organization and modularization for easier maintenance
* E2E tests using KiND