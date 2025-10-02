package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	akuityv1alpha1 "github.com/jacobboykin/namespaceclass-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// deleteOldResources deletes resources tracked in the binding status but not in the current spec
func (r *NamespaceClassBindingReconciler) deleteOldResources(ctx context.Context,
	binding *akuityv1alpha1.NamespaceClassBinding) error {
	if len(binding.Status.AppliedResources) == 0 {
		// No resources to delete
		return nil
	}

	// Delete each resource tracked in the status
	for _, res := range binding.Status.AppliedResources {
		obj := &unstructured.Unstructured{}
		obj.SetAPIVersion(res.APIVersion)
		obj.SetKind(res.Kind)
		obj.SetName(res.Name)
		obj.SetNamespace(binding.Namespace)

		if err := r.Delete(ctx, obj); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete %s/%s: %w", res.Kind, res.Name, err)
		}
	}

	return nil
}

// handleNamespaceClassDeleted handles the case when the referenced NamespaceClass is deleted
func (r *NamespaceClassBindingReconciler) handleNamespaceClassDeleted(ctx context.Context,
	binding *akuityv1alpha1.NamespaceClassBinding) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("referenced NamespaceClass not found, cleaning up resources and deleting binding",
		"className", binding.Spec.ClassName)

	// Clean up all resources managed by this binding
	if err := r.deleteOldResources(ctx, binding); err != nil {
		logger.Error(err, "failed to delete resources for missing NamespaceClass")
		return ctrl.Result{}, err
	}

	// Delete the binding since the class no longer exists
	if err := r.Delete(ctx, binding); err != nil && !errors.IsNotFound(err) {
		logger.Error(err, "failed to delete binding for missing NamespaceClass",
			"NamespaceClass", binding.Spec.ClassName)
		return ctrl.Result{}, err
	}

	r.Recorder.Event(binding, corev1.EventTypeNormal, "CleanedUp",
		fmt.Sprintf("Cleaned up resources and deleted binding for missing NamespaceClass %s",
			binding.Spec.ClassName))

	return ctrl.Result{}, nil
}

// handleNamespaceClassUpdate handles applying updates from a NamespaceClass
func (r *NamespaceClassBindingReconciler) handleNamespaceClassUpdate(ctx context.Context, req ctrl.Request,
	binding *akuityv1alpha1.NamespaceClassBinding, class *akuityv1alpha1.NamespaceClass) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("applying resources", "generation", class.Generation)

	// Prune resources that are no longer in the desired state
	if err := r.pruneRemovedResources(ctx, binding, class); err != nil {
		return ctrl.Result{}, err
	}

	// Apply all resources from the NamespaceClass
	appliedResources, err := r.applyResources(ctx, binding, class.Spec.Resources)
	if err != nil {
		logger.Error(err, "failed to apply resources")
		return ctrl.Result{}, err
	}

	// Update the binding status
	if err := r.patchBindingStatus(ctx, req.NamespacedName, func(b *akuityv1alpha1.NamespaceClassBinding) {
		b.Status.ObservedClassName = class.Name
		b.Status.ObservedClassGeneration = class.Generation
		b.Status.AppliedResources = appliedResources
	}); err != nil {
		logger.Error(err, "failed to update binding status")
		return ctrl.Result{}, err
	}

	r.Recorder.Event(binding, corev1.EventTypeNormal, "ReconcileSucceeded",
		fmt.Sprintf("Successfully applied %d resources from class %s", len(appliedResources),
			binding.Spec.ClassName))

	return ctrl.Result{}, nil
}

// needsUpdate determines if the binding needs to be updated
func (r *NamespaceClassBindingReconciler) needsUpdate(binding *akuityv1alpha1.NamespaceClassBinding,
	class *akuityv1alpha1.NamespaceClass) bool {
	return binding.Status.ObservedClassGeneration != class.Generation ||
		binding.Status.ObservedClassName != binding.Spec.ClassName
}

// pruneRemovedResources removes resources that are no longer in the desired state
func (r *NamespaceClassBindingReconciler) pruneRemovedResources(ctx context.Context,
	binding *akuityv1alpha1.NamespaceClassBinding, class *akuityv1alpha1.NamespaceClass) error {
	// Build desired resource index
	desired := make(map[string]struct{})
	for _, raw := range class.Spec.Resources {
		apiVersion, kind, name, err := extractMetaOnly(raw)
		if err != nil || apiVersion == "" || kind == "" || name == "" {
			return fmt.Errorf("invalid resource in NamespaceClass %q: %v", class.Name, err)
		}
		key := getKey(apiVersion, kind, name)
		desired[key] = struct{}{}
	}

	// Remove resources that are no longer desired
	for _, prev := range binding.Status.AppliedResources {
		key := getKey(prev.APIVersion, prev.Kind, prev.Name)
		if _, ok := desired[key]; !ok {
			u := &unstructured.Unstructured{}
			u.SetAPIVersion(prev.APIVersion)
			u.SetKind(prev.Kind)
			u.SetName(prev.Name)
			u.SetNamespace(binding.Namespace)

			if err := r.Delete(ctx, u); err != nil && !errors.IsNotFound(err) {
				return fmt.Errorf("failed to delete old resource %s/%s: %w", prev.Kind, prev.Name, err)
			}
		}
	}

	return nil
}

// patchBindingStatus safely patches the binding status with conflict retry
func (r *NamespaceClassBindingReconciler) patchBindingStatus(ctx context.Context,
	key types.NamespacedName, mutate func(*akuityv1alpha1.NamespaceClassBinding)) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Get the latest version of the binding
		var cur akuityv1alpha1.NamespaceClassBinding
		if err := r.Get(ctx, key, &cur); err != nil {
			return err
		}

		// Mutate and patch
		base := cur.DeepCopy()
		mutate(&cur)
		return r.Status().Patch(ctx, &cur, client.MergeFrom(base))
	})
}

// applyResources applies all resources from the NamespaceClass (raw list) to the namespace
func (r *NamespaceClassBindingReconciler) applyResources(
	ctx context.Context,
	binding *akuityv1alpha1.NamespaceClassBinding,
	raws []runtime.RawExtension,
) ([]akuityv1alpha1.AppliedResource, error) {
	logger := log.FromContext(ctx)
	applied := make([]akuityv1alpha1.AppliedResource, 0, len(raws))

	for _, raw := range raws {
		apiVersion, kind, name, err := extractMetaOnly(raw)
		if err != nil {
			// malformed entry; surface the error
			return nil, fmt.Errorf("extract meta: %w", err)
		}

		// skip empty items quietly
		if apiVersion == "" || kind == "" || name == "" {
			continue
		}

		// Parse the full object into Unstructured to preserve arbitrary fields
		u := &unstructured.Unstructured{}
		if len(raw.Raw) > 0 {
			if err := u.UnmarshalJSON(raw.Raw); err != nil {
				return nil, fmt.Errorf("unmarshal raw object %s %s: %w", kind, name, err)
			}
		} else if raw.Object != nil {
			m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(raw.Object)
			if err != nil {
				return nil, fmt.Errorf("to-unstructured %s %s: %w", kind, name, err)
			}
			u.Object = m
		} else {
			// nothing to do because there's no data
			continue
		}

		// Ensure GVK & name/namespace are set correctly
		u.SetAPIVersion(apiVersion)
		u.SetKind(kind)
		u.SetName(name)
		u.SetNamespace(binding.Namespace)

		// Make the Binding the controller owner (anchor → children)
		if err := controllerutil.SetControllerReference(binding, u, r.Scheme); err != nil {
			return nil, fmt.Errorf("set ownerRef for %s/%s: %w", kind, name, err)
		}

		// Apply via Server-Side Apply (idempotent)
		if err := r.applyResourceSSA(ctx, u); err != nil {
			return nil, fmt.Errorf("apply %s/%s: %w", kind, name, err)
		}

		// Track applied resource (UID omitted unless you re-GET)
		applied = append(applied, akuityv1alpha1.AppliedResource{
			APIVersion: apiVersion,
			Kind:       kind,
			Name:       name,
		})

		logger.Info("applied resource", "apiVersion", apiVersion, "kind", kind, "name", name)
	}

	return applied, nil
}

// applyResourceSSA performs Server-Side Apply with graduated conflict resolution
func (r *NamespaceClassBindingReconciler) applyResourceSSA(ctx context.Context, u *unstructured.Unstructured) error {
	// First try: Apply without force ownership (most common case)
	err := r.Patch(ctx, u, client.Apply, client.FieldOwner(bindingControllerName))
	if err == nil {
		return nil
	}

	// Second try: Handle field manager conflicts with force ownership
	if r.isFieldManagerConflict(err) {
		return r.Patch(ctx, u, client.Apply,
			client.FieldOwner(bindingControllerName),
			client.ForceOwnership,
		)
	}

	// Third try: Handle immutable field errors by recreating
	if r.isImmutableFieldError(err) {
		return r.recreateResource(ctx, u)
	}

	// Return original error for all other cases
	return err
}

// isFieldManagerConflict checks if the error is due to field manager conflicts
func (r *NamespaceClassBindingReconciler) isFieldManagerConflict(err error) bool {
	// Field manager conflicts typically contain "conflict" in the message
	return errors.IsConflict(err) ||
		(err != nil && (contains(err.Error(), "conflict") ||
			contains(err.Error(), "field manager")))
}

// isImmutableFieldError checks if the error is due to immutable field changes
func (r *NamespaceClassBindingReconciler) isImmutableFieldError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "immutable") ||
		contains(msg, "cannot be modified") ||
		contains(msg, "field is immutable")
}

// recreateResource safely deletes and recreates a resource for immutable field changes
func (r *NamespaceClassBindingReconciler) recreateResource(ctx context.Context, u *unstructured.Unstructured) error {
	logger := log.FromContext(ctx)

	// Check if we're the controller owner before deleting
	if !r.isControllerOwned(ctx, u) {
		return fmt.Errorf("cannot recreate resource %s/%s: not owned by this controller",
			u.GetKind(), u.GetName())
	}

	logger.Info("recreating resource due to immutable field changes",
		"kind", u.GetKind(), "name", u.GetName())

	// Delete the existing resource
	if err := r.Delete(ctx, u); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete resource for recreation: %w", err)
	}

	// Wait for deletion to complete (with timeout)
	if err := r.waitForDeletion(ctx, u); err != nil {
		return fmt.Errorf("failed waiting for resource deletion: %w", err)
	}

	// Recreate the resource
	return r.Patch(ctx, u, client.Apply, client.FieldOwner(bindingControllerName))
}

// isControllerOwned checks if the resource is owned by this controller
func (r *NamespaceClassBindingReconciler) isControllerOwned(ctx context.Context, u *unstructured.Unstructured) bool {
	// Get the current resource to check ownership
	current := &unstructured.Unstructured{}
	current.SetAPIVersion(u.GetAPIVersion())
	current.SetKind(u.GetKind())

	key := client.ObjectKeyFromObject(u)
	if err := r.Get(ctx, key, current); err != nil {
		return false // If we can't get it, assume we don't own it
	}

	// Check if we're in the owner references
	for _, owner := range current.GetOwnerReferences() {
		if owner.APIVersion == "akuity.io/v1alpha1" &&
			owner.Kind == "NamespaceClassBinding" &&
			owner.Controller != nil && *owner.Controller {
			return true
		}
	}
	return false
}

// waitForDeletion waits for a resource to be fully deleted
func (r *NamespaceClassBindingReconciler) waitForDeletion(ctx context.Context, u *unstructured.Unstructured) error {
	key := client.ObjectKeyFromObject(u)
	check := &unstructured.Unstructured{}
	check.SetAPIVersion(u.GetAPIVersion())
	check.SetKind(u.GetKind())

	// Wait up to 30 seconds for deletion
	for i := 0; i < 30; i++ {
		if err := r.Get(ctx, key, check); errors.IsNotFound(err) {
			return nil // Resource is gone
		}

		// Wait 1 second before checking again
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
			continue
		}
	}

	return fmt.Errorf("timed out waiting for resource deletion")
}

// metaOnly is used for extracting basic metadata from raw resources
type metaOnly struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name string `json:"name"`
	} `json:"metadata"`
}

// extractMetaOnly extracts apiVersion, kind, and name from a raw resource
func extractMetaOnly(raw runtime.RawExtension) (apiVersion, kind, name string, err error) {
	if len(raw.Raw) == 0 && raw.Object == nil {
		return "", "", "", nil
	}

	var m metaOnly
	switch {
	case len(raw.Raw) > 0:
		err = json.Unmarshal(raw.Raw, &m)
	default:
		// Convert the embedded object to JSON, then unmarshal
		b, e := json.Marshal(raw.Object)
		if e != nil {
			return "", "", "", e
		}
		err = json.Unmarshal(b, &m)
	}

	if err != nil {
		return "", "", "", err
	}
	return m.APIVersion, m.Kind, m.Metadata.Name, nil
}

// getKey creates a unique key for a resource
func getKey(apiVersion, kind, name string) string {
	return apiVersion + "|" + kind + "|" + name
}

// contains checks if substr is in s, ignoring case
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || len(substr) == 0 ||
			(len(s) > len(substr) && indexIgnoreCase(s, substr) >= 0))
}

// indexIgnoreCase performs case-insensitive substring search
func indexIgnoreCase(s, substr string) int {
	s, substr = strings.ToLower(s), strings.ToLower(substr)
	return strings.Index(s, substr)
}
