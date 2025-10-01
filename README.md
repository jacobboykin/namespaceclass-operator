# NamespaceClass Operator

A Kubernetes operator that automatically applies standardized resources to namespaces based on predefined templates called NamespaceClasses.

## Enhancements 

Things that could be added in the future to improve this project:

* More robust documentation and examples
* Enhanced RBAC support for finer-grained access control
* Proper Status.Conditions on NamespaceClassBinding resources to indicate sync status for each Namespace.
* Better CRD validation to ensure cluster-scoped resources are not included in NamespaceClass definitions
* Drift synchronization options to ensure resources remain in sync with NamespaceClass definitions
* Cleaner code organization and modularization for easier maintenance