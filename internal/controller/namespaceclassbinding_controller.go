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

	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	"k8s.io/apimachinery/pkg/runtime"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	logger := log.FromContext(ctx).WithValues("binding", req.NamespacedName)

	// Fetch the NamespaceClassBinding
	binding := &akuityv1alpha1.NamespaceClassBinding{}
	if err := r.Get(ctx, req.NamespacedName, binding); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		logger.Error(err, "unable to fetch NamespaceClassBinding",
			"NamespaceClassBinding", req.NamespacedName)
		return ctrl.Result{}, err
	}

	// Fetch the referenced NamespaceClass
	class := &akuityv1alpha1.NamespaceClass{}
	if err := r.Get(ctx, types.NamespacedName{Name: binding.Spec.ClassName}, class); err != nil {
		if errors.IsNotFound(err) {
			return r.handleNamespaceClassDeleted(ctx, binding)
		}

		logger.Error(err, "unable to fetch NamespaceClass", "className", binding.Spec.ClassName)
		return ctrl.Result{}, err
	}

	// Handle class switching if detected
	if r.isClassSwitch(binding, class) {
		if err := r.handleClassSwitch(ctx, binding, class); err != nil {
			return ctrl.Result{}, err
		}
		// After deleting old resources, always apply new resources
		return r.handleNamespaceClassUpdate(ctx, req, binding, class)
	}

	// Check if we need to update based on generation
	if r.needsUpdate(binding, class) {
		return r.handleNamespaceClassUpdate(ctx, req, binding, class)
	}

	// Everything is up to date
	return ctrl.Result{}, nil
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

	// Trigger reconciliation of bindings when their referenced class changes
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
