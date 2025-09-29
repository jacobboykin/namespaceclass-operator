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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	akuityv1alpha1 "github.com/jacobboykin/namespaceclass-operator/api/v1alpha1"
)

func TestNamespaceReconciler_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, akuityv1alpha1.AddToScheme(scheme))

	tests := []struct {
		name            string
		namespace       *corev1.Namespace
		existingBinding *akuityv1alpha1.NamespaceClassBinding
		expectBinding   *akuityv1alpha1.NamespaceClassBinding
		expectNoBinding bool
		expectError     bool
		expectEvent     string
		expectNoEvent   bool
	}{
		{
			name: "create binding when namespace has class label",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
					Labels: map[string]string{
						labelNamespaceClass: "test-class",
					},
				},
			},
			expectBinding: &akuityv1alpha1.NamespaceClassBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ns",
					Namespace: "test-ns",
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion:         "v1",
						Kind:               "Namespace",
						Name:               "test-ns",
						Controller:         &[]bool{true}[0],
						BlockOwnerDeletion: &[]bool{true}[0],
					}},
				},
				Spec: akuityv1alpha1.NamespaceClassBindingSpec{
					ClassName: "test-class",
				},
			},
			expectEvent: "BindingCreated",
		},
		{
			name: "update binding when class label changes",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
					Labels: map[string]string{
						labelNamespaceClass: "new-class",
					},
				},
			},
			existingBinding: &akuityv1alpha1.NamespaceClassBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ns",
					Namespace: "test-ns",
				},
				Spec: akuityv1alpha1.NamespaceClassBindingSpec{
					ClassName: "old-class",
				},
			},
			expectBinding: &akuityv1alpha1.NamespaceClassBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ns",
					Namespace: "test-ns",
				},
				Spec: akuityv1alpha1.NamespaceClassBindingSpec{
					ClassName: "new-class",
				},
			},
			expectEvent: "BindingUpdated",
		},
		{
			name: "delete binding when class label is removed",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			},
			existingBinding: &akuityv1alpha1.NamespaceClassBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ns",
					Namespace: "test-ns",
				},
				Spec: akuityv1alpha1.NamespaceClassBindingSpec{
					ClassName: "test-class",
				},
			},
			expectNoBinding: true,
		},
		{
			name: "do nothing when no label and no binding",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			},
			expectNoBinding: true,
			expectNoEvent:   true,
		},
		{
			name: "do nothing when label matches existing binding",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
					Labels: map[string]string{
						labelNamespaceClass: "test-class",
					},
				},
			},
			existingBinding: &akuityv1alpha1.NamespaceClassBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ns",
					Namespace: "test-ns",
				},
				Spec: akuityv1alpha1.NamespaceClassBindingSpec{
					ClassName: "test-class",
				},
			},
			expectBinding: &akuityv1alpha1.NamespaceClassBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ns",
					Namespace: "test-ns",
				},
				Spec: akuityv1alpha1.NamespaceClassBindingSpec{
					ClassName: "test-class",
				},
			},
			expectNoEvent: true,
		},
		{
			name:            "handle namespace not found",
			expectNoBinding: true,
			expectNoEvent:   true,
		},
		{
			name: "skip reconciliation for namespace being deleted",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-ns",
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					Finalizers:        []string{"kubernetes"},
					Labels: map[string]string{
						labelNamespaceClass: "test-class",
					},
				},
			},
			expectNoBinding: true,
			expectNoEvent:   true,
		},
		{
			name: "handle namespace with nil labels",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-ns",
					Labels: nil,
				},
			},
			expectNoBinding: true,
			expectNoEvent:   true,
		},
		{
			name: "handle empty class label value",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
					Labels: map[string]string{
						labelNamespaceClass: "",
					},
				},
			},
			expectNoBinding: true,
			expectNoEvent:   true,
		},
		{
			name: "handle namespace with other labels but no class label",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
					Labels: map[string]string{
						"some-other-label": "some-value",
						"another-label":    "another-value",
					},
				},
			},
			expectNoBinding: true,
			expectNoEvent:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			recorder := record.NewFakeRecorder(10)

			var objects []client.Object
			if tt.namespace != nil {
				objects = append(objects, tt.namespace)
			}
			if tt.existingBinding != nil {
				objects = append(objects, tt.existingBinding)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			reconciler := &NamespaceReconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				Recorder: recorder,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "test-ns"},
			}

			result, err := reconciler.Reconcile(ctx, req)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, ctrl.Result{}, result)

			bindingKey := types.NamespacedName{Name: "test-ns", Namespace: "test-ns"}
			binding := &akuityv1alpha1.NamespaceClassBinding{}
			err = fakeClient.Get(ctx, bindingKey, binding)

			if tt.expectNoBinding {
				assert.True(t, errors.IsNotFound(err), "expected binding to not exist")
			} else if tt.expectBinding != nil {
				assert.NoError(t, err, "expected binding to exist")
				assert.Equal(t, tt.expectBinding.Spec.ClassName, binding.Spec.ClassName)

				if len(tt.expectBinding.OwnerReferences) > 0 {
					require.Len(t, binding.OwnerReferences, 1)
					assert.Equal(t, "test-ns", binding.OwnerReferences[0].Name)
					assert.Equal(t, "Namespace", binding.OwnerReferences[0].Kind)
					assert.Equal(t, "v1", binding.OwnerReferences[0].APIVersion)
					assert.True(t, *binding.OwnerReferences[0].Controller)
					assert.True(t, *binding.OwnerReferences[0].BlockOwnerDeletion)
				}
			}

			if tt.expectNoEvent {
				select {
				case event := <-recorder.Events:
					t.Errorf("expected no event, but got: %s", event)
				default:
				}
			} else if tt.expectEvent != "" {
				select {
				case event := <-recorder.Events:
					assert.Contains(t, event, tt.expectEvent)
				default:
					t.Errorf("expected event containing %s, but got none", tt.expectEvent)
				}
			}
		})
	}
}

func TestNamespaceReconciler_Reconcile_Errors(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, akuityv1alpha1.AddToScheme(scheme))

	t.Run("handle client get error", func(t *testing.T) {
		ctx := context.Background()
		recorder := record.NewFakeRecorder(10)

		fakeClient := &errorClient{
			Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
			getErr: fmt.Errorf("fake client error"),
		}

		reconciler := &NamespaceReconciler{
			Client:   fakeClient,
			Scheme:   scheme,
			Recorder: recorder,
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "test-ns"},
		}

		result, err := reconciler.Reconcile(ctx, req)
		assert.Error(t, err)
		assert.Equal(t, ctrl.Result{}, result)
	})
}

type errorClient struct {
	client.Client
	getErr    error
	createErr error
	updateErr error
	deleteErr error
}

func (e *errorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if e.getErr != nil {
		return e.getErr
	}
	return e.Client.Get(ctx, key, obj, opts...)
}

func (e *errorClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if e.createErr != nil {
		return e.createErr
	}
	return e.Client.Create(ctx, obj, opts...)
}

func (e *errorClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if e.updateErr != nil {
		return e.updateErr
	}
	return e.Client.Update(ctx, obj, opts...)
}

func (e *errorClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if e.deleteErr != nil {
		return e.deleteErr
	}
	return e.Client.Delete(ctx, obj, opts...)
}
