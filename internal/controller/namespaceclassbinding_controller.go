/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	akuityv1alpha1 "github.com/jacobboykin/namespaceclass-operator/api/v1alpha1"
)

const (
	// labelNamespaceClass is the label key used to specify the NamespaceClass for a namespace
	labelNamespaceClass = "namespaceclass.akuity.io/name"

	// bindingControllerName is the name of this controller
	bindingControllerName = "namespaceclassbinding-controller"

	// Condition types
	conditionTypeReady = "Ready"

	// Condition reasons
	reasonReconcileSuccess = "ReconcileSuccess"
	reasonClassNotFound    = "ClassNotFound"
	reasonPruneFailed      = "PruneFailed"
	reasonApplyFailed      = "ApplyFailed"
)

// NamespaceClassBindingReconciler reconciles a NamespaceClassBinding object
type NamespaceClassBindingReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=akuity.io,resources=namespaceclassbindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=akuity.io,resources=namespaceclassbindings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=akuity.io,resources=namespaceclassbindings/finalizers,verbs=update
// +kubebuilder:rbac:groups=akuity.io,resources=namespaceclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="*",resources="*",verbs=get;list;watch;create;update;patch;delete

// Reconcile handles the reconciliation of a NamespaceClassBinding
func (r *NamespaceClassBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues(
		"binding", req.Name,
		"namespace", req.Namespace,
	)

	// Fetch the NamespaceClassBinding
	binding := &akuityv1alpha1.NamespaceClassBinding{}
	if err := r.Get(ctx, req.NamespacedName, binding); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to fetch NamespaceClassBinding")
		return ctrl.Result{}, err
	}

	// Add className to logger context for all subsequent logs
	logger = logger.WithValues("className", binding.Spec.ClassName)

	// Fetch the referenced NamespaceClass
	class := &akuityv1alpha1.NamespaceClass{}
	if err := r.Get(ctx, types.NamespacedName{Name: binding.Spec.ClassName}, class); err != nil {
		if errors.IsNotFound(err) {
			// Class deleted - mark as not ready and delete binding
			logger.Info("NamespaceClass not found, deleting binding")
			r.setCondition(binding, conditionTypeReady, false, reasonClassNotFound,
				fmt.Sprintf("NamespaceClass %s not found", binding.Spec.ClassName))
			_ = r.updateStatus(ctx, req.NamespacedName, func(b *akuityv1alpha1.NamespaceClassBinding) {
				b.Status.Conditions = binding.Status.Conditions
			})
			if err := r.Delete(ctx, binding); err != nil && !errors.IsNotFound(err) {
				logger.Error(err, "failed to delete binding")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to fetch NamespaceClass")
		return ctrl.Result{}, err
	}

	// Add generation to logger context
	logger = logger.WithValues("generation", class.Generation)

	// Check if we need to update
	if binding.Status.ObservedClassGeneration == class.Generation &&
		binding.Status.ObservedClassName == class.Name {
		// Everything is up to date
		logger.V(1).Info("binding up to date, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	logger.Info("reconciling binding",
		"resourceCount", len(class.Spec.Resources),
		"previousGeneration", binding.Status.ObservedClassGeneration)

	// Prune resources no longer in the class
	if err := r.pruneResources(ctx, logger, binding, class); err != nil {
		logger.Error(err, "failed to prune resources")
		r.setCondition(binding, conditionTypeReady, false, reasonPruneFailed,
			fmt.Sprintf("Failed to prune resources: %v", err))
		_ = r.updateStatus(ctx, req.NamespacedName, func(b *akuityv1alpha1.NamespaceClassBinding) {
			b.Status.Conditions = binding.Status.Conditions
		})
		return ctrl.Result{}, err
	}

	// Apply all resources from the NamespaceClass
	appliedResources, err := r.applyResources(ctx, logger, binding, class.Spec.Resources)
	if err != nil {
		logger.Error(err, "failed to apply resources")
		r.setCondition(binding, conditionTypeReady, false, reasonApplyFailed,
			fmt.Sprintf("Failed to apply resources: %v", err))
		_ = r.updateStatus(ctx, req.NamespacedName, func(b *akuityv1alpha1.NamespaceClassBinding) {
			b.Status.Conditions = binding.Status.Conditions
		})
		return ctrl.Result{}, err
	}

	// Set ready condition on success
	r.setCondition(binding, conditionTypeReady, true, reasonReconcileSuccess,
		fmt.Sprintf("Successfully applied %d resources from class %s", len(appliedResources), class.Name))

	// Update the binding status
	if err := r.updateStatus(ctx, req.NamespacedName, func(b *akuityv1alpha1.NamespaceClassBinding) {
		b.Status.ObservedClassName = class.Name
		b.Status.ObservedClassGeneration = class.Generation
		b.Status.AppliedResources = appliedResources
		b.Status.Conditions = binding.Status.Conditions
	}); err != nil {
		logger.Error(err, "failed to update binding status")
		return ctrl.Result{}, err
	}

	logger.Info("successfully reconciled binding", "appliedResourceCount", len(appliedResources))

	r.Recorder.Event(binding, corev1.EventTypeNormal, "ReconcileSucceeded",
		fmt.Sprintf("Successfully applied %d resources from class %s", len(appliedResources), class.Name))

	return ctrl.Result{}, nil
}

// pruneResources removes resources that are no longer in the desired state
func (r *NamespaceClassBindingReconciler) pruneResources(ctx context.Context, logger logr.Logger,
	binding *akuityv1alpha1.NamespaceClassBinding, class *akuityv1alpha1.NamespaceClass) error {

	// Build set of desired resources
	desired := make(map[string]struct{})
	for _, raw := range class.Spec.Resources {
		apiVersion, kind, name, err := extractMetadata(raw)
		if err != nil || apiVersion == "" || kind == "" || name == "" {
			continue // Skip invalid entries
		}
		desired[resourceKey(apiVersion, kind, name)] = struct{}{}
	}

	// Delete resources that are no longer desired
	for _, res := range binding.Status.AppliedResources {
		if _, ok := desired[resourceKey(res.APIVersion, res.Kind, res.Name)]; !ok {
			obj := &unstructured.Unstructured{}
			obj.SetAPIVersion(res.APIVersion)
			obj.SetKind(res.Kind)
			obj.SetName(res.Name)
			obj.SetNamespace(binding.Namespace)

			logger.Info("pruning resource",
				"apiVersion", res.APIVersion,
				"kind", res.Kind,
				"name", res.Name)

			if err := r.Delete(ctx, obj); err != nil && !errors.IsNotFound(err) {
				return fmt.Errorf("failed to delete %s/%s: %w", res.Kind, res.Name, err)
			}
		}
	}

	return nil
}

// applyResources applies all resources from the NamespaceClass to the namespace
func (r *NamespaceClassBindingReconciler) applyResources(ctx context.Context, logger logr.Logger,
	binding *akuityv1alpha1.NamespaceClassBinding,
	resources []runtime.RawExtension) ([]akuityv1alpha1.AppliedResource, error) {

	applied := make([]akuityv1alpha1.AppliedResource, 0, len(resources))

	for _, raw := range resources {
		obj, err := parseResource(raw)
		if err != nil {
			return nil, err
		}
		if obj == nil {
			continue // Empty entry
		}

		// Set namespace and owner reference
		obj.SetNamespace(binding.Namespace)
		if err := controllerutil.SetControllerReference(binding, obj, r.Scheme); err != nil {
			return nil, fmt.Errorf("set owner reference for %s/%s: %w", obj.GetKind(), obj.GetName(), err)
		}

		// Apply using Server-Side Apply with force ownership
		if err := r.Patch(ctx, obj, client.Apply,
			client.FieldOwner(bindingControllerName),
			client.ForceOwnership); err != nil {
			return nil, fmt.Errorf("apply %s/%s: %w", obj.GetKind(), obj.GetName(), err)
		}

		applied = append(applied, akuityv1alpha1.AppliedResource{
			APIVersion: obj.GetAPIVersion(),
			Kind:       obj.GetKind(),
			Name:       obj.GetName(),
		})

		logger.Info("applied resource",
			"apiVersion", obj.GetAPIVersion(),
			"kind", obj.GetKind(),
			"name", obj.GetName())
	}

	return applied, nil
}

// parseResource converts a RawExtension into an Unstructured object
func parseResource(raw runtime.RawExtension) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}

	if len(raw.Raw) > 0 {
		if err := obj.UnmarshalJSON(raw.Raw); err != nil {
			return nil, fmt.Errorf("unmarshal: %w", err)
		}
	} else if raw.Object != nil {
		m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(raw.Object)
		if err != nil {
			return nil, fmt.Errorf("convert: %w", err)
		}
		obj.Object = m
	} else {
		return nil, nil // Empty entry
	}

	// Validate required fields
	if obj.GetAPIVersion() == "" || obj.GetKind() == "" || obj.GetName() == "" {
		return nil, nil // Skip invalid entries
	}

	return obj, nil
}

// updateStatus safely updates the binding status with conflict retry
func (r *NamespaceClassBindingReconciler) updateStatus(ctx context.Context,
	key types.NamespacedName, mutate func(*akuityv1alpha1.NamespaceClassBinding)) error {

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var binding akuityv1alpha1.NamespaceClassBinding
		if err := r.Get(ctx, key, &binding); err != nil {
			return err
		}
		base := binding.DeepCopy()
		mutate(&binding)
		return r.Status().Patch(ctx, &binding, client.MergeFrom(base))
	})
}

// extractMetadata extracts apiVersion, kind, and name from a raw resource
func extractMetadata(raw runtime.RawExtension) (string, string, string, error) {
	obj := &unstructured.Unstructured{}

	if len(raw.Raw) > 0 {
		if err := obj.UnmarshalJSON(raw.Raw); err != nil {
			return "", "", "", err
		}
	} else if raw.Object != nil {
		m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(raw.Object)
		if err != nil {
			return "", "", "", err
		}
		obj.Object = m
	} else {
		return "", "", "", nil
	}

	return obj.GetAPIVersion(), obj.GetKind(), obj.GetName(), nil
}

// resourceKey creates a unique key for a resource
func resourceKey(apiVersion, kind, name string) string {
	return apiVersion + "/" + kind + "/" + name
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceClassBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor(bindingControllerName)

	// Index bindings by class name for efficient lookups
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &akuityv1alpha1.NamespaceClassBinding{},
		"spec.className", func(rawObj client.Object) []string {
			binding := rawObj.(*akuityv1alpha1.NamespaceClassBinding)
			return []string{binding.Spec.ClassName}
		}); err != nil {
		return err
	}

	// Watch NamespaceClassBindings and NamespaceClasses
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 4,
		}).
		For(&akuityv1alpha1.NamespaceClassBinding{}).
		Watches(
			&akuityv1alpha1.NamespaceClass{},
			handler.EnqueueRequestsFromMapFunc(r.findBindingsForClass),
		).
		Complete(r)
}

// findBindingsForClass returns reconcile requests for all bindings that reference the given class
func (r *NamespaceClassBindingReconciler) findBindingsForClass(ctx context.Context,
	obj client.Object) []reconcile.Request {
	class := obj.(*akuityv1alpha1.NamespaceClass)

	var bindings akuityv1alpha1.NamespaceClassBindingList
	if err := r.List(ctx, &bindings, client.MatchingFields{"spec.className": class.Name}); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, len(bindings.Items))
	for i, binding := range bindings.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      binding.Name,
				Namespace: binding.Namespace,
			},
		}
	}

	return requests
}

// setCondition sets a condition on the binding using standard k8s condition helpers
func (r *NamespaceClassBindingReconciler) setCondition(binding *akuityv1alpha1.NamespaceClassBinding,
	conditionType string, status bool, reason, message string) {
	// Determine metav1 status
	var metaStatus metav1.ConditionStatus
	if status {
		metaStatus = metav1.ConditionTrue
	} else {
		metaStatus = metav1.ConditionFalse
	}

	condition := apimeta.FindStatusCondition(binding.Status.Conditions, conditionType)

	// Only update if condition changed to avoid unnecessary updates
	if condition != nil &&
		condition.Status == metaStatus &&
		condition.Reason == reason &&
		condition.Message == message {
		return
	}

	apimeta.SetStatusCondition(&binding.Status.Conditions, metav1.Condition{
		Type:    conditionType,
		Status:  metaStatus,
		Reason:  reason,
		Message: message,
	})
}
