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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	akuityv1alpha1 "github.com/jacobboykin/namespaceclass-operator/api/v1alpha1"
)

const (
	// namespaceControllerName is the name of this controller
	namespaceControllerName = "namespace-controller"
)

// NamespaceReconciler reconciles Namespace objects to manage NamespaceClassBindings
type NamespaceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// Reconcile manages NamespaceClassBindings based on namespace labels
func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("namespace", req.Name)

	// Fetch the namespace
	namespace := &corev1.Namespace{}
	if err := r.Get(ctx, req.NamespacedName, namespace); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		logger.Error(err, "failed to get Namespace", "Namespace", req.NamespacedName)
		return ctrl.Result{}, err
	}

	// Skip if namespace is being deleted
	if !namespace.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// Get the desired class from the label
	desiredClass := ""
	if namespace.Labels != nil {
		desiredClass = namespace.Labels[labelNamespaceClass]
	}

	// Get the existing binding if any
	binding := &akuityv1alpha1.NamespaceClassBinding{}
	bindingKey := types.NamespacedName{Name: namespace.Name, Namespace: namespace.Name}
	err := r.Get(ctx, bindingKey, binding)
	bindingExists := err == nil

	// If there's no class label and no binding, there's nothing to do
	if desiredClass == "" && !bindingExists {
		return ctrl.Result{}, nil
	}

	// If the label was removed, we should clean up the binding
	if desiredClass == "" && bindingExists {
		logger.Info("removing NamespaceClassBinding as label was removed")
		if err := r.Delete(ctx, binding); err != nil && !errors.IsNotFound(err) {
			logger.Error(err, "failed to delete NamespaceClassBinding",
				"NamespaceClassBinding", bindingKey)
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	// If desiredClass is set and binding does not exist, create it
	if desiredClass != "" && !bindingExists {
		logger.Info("creating NamespaceClassBinding", "class", desiredClass)
		binding = &akuityv1alpha1.NamespaceClassBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      namespace.Name,
				Namespace: namespace.Name,
			},
			Spec: akuityv1alpha1.NamespaceClassBindingSpec{
				ClassName: desiredClass,
			},
		}

		// Set owner reference to the namespace for garbage collection
		if err := controllerutil.SetControllerReference(namespace, binding, r.Scheme); err != nil {
			logger.Error(err, "failed to set owner reference")
			return ctrl.Result{}, err
		}

		if err := r.Create(ctx, binding); err != nil {
			logger.Error(err, "failed to create NamespaceClassBinding")
			return ctrl.Result{}, err
		}

		r.Recorder.Event(namespace, corev1.EventTypeNormal, "BindingCreated",
			fmt.Sprintf("Created NamespaceClassBinding for class %s", desiredClass))
		return ctrl.Result{}, nil
	}

	// If the class has changed, update the binding
	if desiredClass != "" && bindingExists && binding.Spec.ClassName != desiredClass {
		logger.Info("updating NamespaceClassBinding", "oldClass",
			binding.Spec.ClassName, "newClass", desiredClass)

		// Update the binding
		binding.Spec.ClassName = desiredClass
		if err := r.Update(ctx, binding); err != nil {
			logger.Error(err, "failed to update NamespaceClassBinding",
				"NamespaceClassBinding", bindingKey)
			return ctrl.Result{}, err
		}

		r.Recorder.Event(namespace, corev1.EventTypeNormal, "BindingUpdated",
			fmt.Sprintf("Updated NamespaceClassBinding to class %s", desiredClass))
		return ctrl.Result{}, nil
	}

	// Everything is in sync
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor(namespaceControllerName)

	// Only reconcile when our class label changes (or is present on create)
	nsPred := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			_, ok := e.Object.GetLabels()[labelNamespaceClass]
			return ok
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldLbl := e.ObjectOld.GetLabels()[labelNamespaceClass]
			newLbl := e.ObjectNew.GetLabels()[labelNamespaceClass]
			return oldLbl != newLbl
		},
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}

	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 2,
		}).
		For(&corev1.Namespace{}, builder.WithPredicates(nsPred)).
		Complete(r)
}
