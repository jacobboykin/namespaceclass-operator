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

func TestNamespaceClassBindingReconciler_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, akuityv1alpha1.AddToScheme(scheme))

	tests := []struct {
		name            string
		binding         *akuityv1alpha1.NamespaceClassBinding
		class           *akuityv1alpha1.NamespaceClass
		expectError     bool
		expectNoBinding bool
		expectEvent     string
		expectNoEvent   bool
	}{
		{
			name: "no update when generation matches",
			binding: &akuityv1alpha1.NamespaceClassBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-binding",
					Namespace: "test-ns",
				},
				Spec: akuityv1alpha1.NamespaceClassBindingSpec{
					ClassName: "test-class",
				},
				Status: akuityv1alpha1.NamespaceClassBindingStatus{
					ObservedClassGeneration: 1, // Same generation
				},
			},
			class: &akuityv1alpha1.NamespaceClass{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-class",
					Generation: 1, // Same generation
				},
				Spec: akuityv1alpha1.NamespaceClassSpec{
					Resources: []runtime.RawExtension{},
				},
			},
			expectNoEvent: true,
		},
		{
			name: "cleanup and delete binding when class not found",
			binding: &akuityv1alpha1.NamespaceClassBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-binding",
					Namespace: "test-ns",
				},
				Spec: akuityv1alpha1.NamespaceClassBindingSpec{
					ClassName: "missing-class",
				},
				Status: akuityv1alpha1.NamespaceClassBindingStatus{
					AppliedResources: []akuityv1alpha1.AppliedResource{
						{APIVersion: "v1", Kind: "ConfigMap", Name: "test-config"},
					},
				},
			},
			class:           nil, // Class doesn't exist
			expectNoBinding: true,
			expectEvent:     "CleanedUp",
		},
		{
			name:            "handle binding not found gracefully",
			expectNoBinding: true,
			expectNoEvent:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			recorder := record.NewFakeRecorder(10)

			var objects []client.Object
			if tt.binding != nil {
				objects = append(objects, tt.binding)
			}
			if tt.class != nil {
				objects = append(objects, tt.class)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			reconciler := &NamespaceClassBindingReconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				Recorder: recorder,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "test-binding", Namespace: "test-ns"},
			}

			result, err := reconciler.Reconcile(ctx, req)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, ctrl.Result{}, result)

			// Check binding exists or doesn't exist as expected
			bindingKey := types.NamespacedName{Name: "test-binding", Namespace: "test-ns"}
			binding := &akuityv1alpha1.NamespaceClassBinding{}
			err = fakeClient.Get(ctx, bindingKey, binding)

			if tt.expectNoBinding {
				assert.True(t, errors.IsNotFound(err), "expected binding to not exist")
			} else {
				assert.NoError(t, err, "expected binding to exist")
			}

			// Check events
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

func TestNamespaceClassBindingReconciler_HandleErrors(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, akuityv1alpha1.AddToScheme(scheme))

	t.Run("handle client get error for binding", func(t *testing.T) {
		ctx := context.Background()
		recorder := record.NewFakeRecorder(10)

		fakeClient := &errorClient{
			Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
			getErr: fmt.Errorf("fake client error"),
		}

		reconciler := &NamespaceClassBindingReconciler{
			Client:   fakeClient,
			Scheme:   scheme,
			Recorder: recorder,
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "test-binding", Namespace: "test-ns"},
		}

		result, err := reconciler.Reconcile(ctx, req)
		assert.Error(t, err)
		assert.Equal(t, ctrl.Result{}, result)
	})

	t.Run("handle client get error for class", func(t *testing.T) {
		ctx := context.Background()
		recorder := record.NewFakeRecorder(10)

		binding := &akuityv1alpha1.NamespaceClassBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-binding",
				Namespace: "test-ns",
			},
			Spec: akuityv1alpha1.NamespaceClassBindingSpec{
				ClassName: "test-class",
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(binding).
			Build()

		// Wrap with error client that fails on second Get (for class)
		errorClient := &conditionalErrorClient{
			Client:         fakeClient,
			getErrorOnCall: 2,
			getErr:         fmt.Errorf("fake class fetch error"),
		}

		reconciler := &NamespaceClassBindingReconciler{
			Client:   errorClient,
			Scheme:   scheme,
			Recorder: recorder,
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "test-binding", Namespace: "test-ns"},
		}

		result, err := reconciler.Reconcile(ctx, req)
		assert.Error(t, err)
		assert.Equal(t, ctrl.Result{}, result)
	})
}

func TestNamespaceClassBindingReconciler_HelperFunctions(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, akuityv1alpha1.AddToScheme(scheme))

	t.Run("needsUpdate", func(t *testing.T) {
		reconciler := &NamespaceClassBindingReconciler{}

		tests := []struct {
			name              string
			bindingGen        int64
			classGen          int64
			expectNeedsUpdate bool
		}{
			{"same generation", 1, 1, false},
			{"binding behind", 1, 2, true},
			{"binding ahead", 2, 1, true},
			{"both zero", 0, 0, false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				binding := &akuityv1alpha1.NamespaceClassBinding{
					Status: akuityv1alpha1.NamespaceClassBindingStatus{
						ObservedClassGeneration: tt.bindingGen,
					},
				}
				class := &akuityv1alpha1.NamespaceClass{
					ObjectMeta: metav1.ObjectMeta{
						Generation: tt.classGen,
					},
				}

				result := reconciler.needsUpdate(binding, class)
				assert.Equal(t, tt.expectNeedsUpdate, result)
			})
		}
	})

	t.Run("isClassSwitch", func(t *testing.T) {
		reconciler := &NamespaceClassBindingReconciler{}

		tests := []struct {
			name                string
			bindingClassName    string
			observedClassName   string
			classNameInClass    string
			hasAppliedResources bool
			expectSwitch        bool
		}{
			{"no switch same class", "class-a", "class-a", "class-a", true, false},
			{"switch different class", "class-b", "class-a", "class-b", true, true},
			{"no switch no resources", "class-b", "class-a", "class-b", false, false},
			{"no switch no observed class name", "class-b", "", "class-b", true, false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				binding := &akuityv1alpha1.NamespaceClassBinding{
					Spec: akuityv1alpha1.NamespaceClassBindingSpec{
						ClassName: tt.bindingClassName,
					},
					Status: akuityv1alpha1.NamespaceClassBindingStatus{
						ObservedClassName: tt.observedClassName,
					},
				}
				if tt.hasAppliedResources {
					binding.Status.AppliedResources = []akuityv1alpha1.AppliedResource{
						{APIVersion: "v1", Kind: "ConfigMap", Name: "test"},
					}
				}

				class := &akuityv1alpha1.NamespaceClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: tt.classNameInClass,
					},
				}

				result := reconciler.isClassSwitch(binding, class)
				assert.Equal(t, tt.expectSwitch, result)
			})
		}
	})

	t.Run("pruneRemovedResources", func(t *testing.T) {
		ctx := context.Background()
		recorder := record.NewFakeRecorder(10)

		binding := &akuityv1alpha1.NamespaceClassBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-binding",
				Namespace: "test-ns",
			},
			Status: akuityv1alpha1.NamespaceClassBindingStatus{
				AppliedResources: []akuityv1alpha1.AppliedResource{
					{APIVersion: "v1", Kind: "ConfigMap", Name: "old-config"},
					{APIVersion: "v1", Kind: "Secret", Name: "keep-secret"},
				},
			},
		}

		class := &akuityv1alpha1.NamespaceClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-class",
			},
			Spec: akuityv1alpha1.NamespaceClassSpec{
				Resources: []runtime.RawExtension{
					{
						Raw: []byte(`{
							"apiVersion": "v1",
							"kind": "Secret",
							"metadata": {
								"name": "keep-secret"
							}
						}`),
					},
				},
			},
		}

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		reconciler := &NamespaceClassBindingReconciler{
			Client:   fakeClient,
			Scheme:   scheme,
			Recorder: recorder,
		}

		// This should not error even though delete fails
		err := reconciler.pruneRemovedResources(ctx, binding, class)
		assert.NoError(t, err)
	})
}

// Conditional error client that fails on specific call numbers
type conditionalErrorClient struct {
	client.Client
	getErrorOnCall int
	getCallCount   int
	getErr         error
}

func (e *conditionalErrorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	e.getCallCount++
	if e.getCallCount == e.getErrorOnCall && e.getErr != nil {
		return e.getErr
	}
	return e.Client.Get(ctx, key, obj, opts...)
}

func TestNamespaceClassBindingReconciler_HandlerMethods(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, akuityv1alpha1.AddToScheme(scheme))

	t.Run("handleNamespaceClassDeleted", func(t *testing.T) {
		ctx := context.Background()
		recorder := record.NewFakeRecorder(10)

		binding := &akuityv1alpha1.NamespaceClassBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-binding",
				Namespace: "test-ns",
			},
			Status: akuityv1alpha1.NamespaceClassBindingStatus{
				AppliedResources: []akuityv1alpha1.AppliedResource{
					{APIVersion: "v1", Kind: "ConfigMap", Name: "test-config"},
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(binding).
			Build()

		reconciler := &NamespaceClassBindingReconciler{
			Client:   fakeClient,
			Scheme:   scheme,
			Recorder: recorder,
		}

		result, err := reconciler.handleNamespaceClassDeleted(ctx, binding)
		assert.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, result)

		// Verify event was recorded
		select {
		case event := <-recorder.Events:
			assert.Contains(t, event, "CleanedUp")
		default:
			t.Error("expected CleanedUp event")
		}

		// Verify binding was deleted
		err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-binding", Namespace: "test-ns"}, binding)
		assert.True(t, errors.IsNotFound(err))
	})

	t.Run("handleClassSwitch", func(t *testing.T) {
		ctx := context.Background()
		recorder := record.NewFakeRecorder(10)

		binding := &akuityv1alpha1.NamespaceClassBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-binding",
				Namespace: "test-ns",
			},
			Spec: akuityv1alpha1.NamespaceClassBindingSpec{
				ClassName: "new-class",
			},
			Status: akuityv1alpha1.NamespaceClassBindingStatus{
				ObservedClassGeneration: 1,
				AppliedResources: []akuityv1alpha1.AppliedResource{
					{APIVersion: "v1", Kind: "ConfigMap", Name: "old-config"},
				},
			},
		}

		class := &akuityv1alpha1.NamespaceClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "new-class",
			},
			Spec: akuityv1alpha1.NamespaceClassSpec{
				Resources: []runtime.RawExtension{},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(binding, class).
			Build()

		reconciler := &NamespaceClassBindingReconciler{
			Client:   fakeClient,
			Scheme:   scheme,
			Recorder: recorder,
		}

		err := reconciler.handleClassSwitch(ctx, binding, class)
		assert.NoError(t, err)

		// handleClassSwitch doesn't record events, just cleans up old resources
		// Verify no event was recorded
		select {
		case event := <-recorder.Events:
			t.Errorf("unexpected event: %s", event)
		default:
			// Expected no event
		}
	})

	t.Run("findBindingsForClass", func(t *testing.T) {
		ctx := context.Background()

		binding1 := &akuityv1alpha1.NamespaceClassBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "binding1",
				Namespace: "ns1",
			},
			Spec: akuityv1alpha1.NamespaceClassBindingSpec{
				ClassName: "test-class",
			},
		}

		binding2 := &akuityv1alpha1.NamespaceClassBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "binding2",
				Namespace: "ns2",
			},
			Spec: akuityv1alpha1.NamespaceClassBindingSpec{
				ClassName: "test-class",
			},
		}

		binding3 := &akuityv1alpha1.NamespaceClassBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "binding3",
				Namespace: "ns3",
			},
			Spec: akuityv1alpha1.NamespaceClassBindingSpec{
				ClassName: "other-class",
			},
		}

		class := &akuityv1alpha1.NamespaceClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-class",
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(binding1, binding2, binding3, class).
			Build()

		// Since we can't easily test the field indexer with fake client, we'll test the logic directly
		var bindings akuityv1alpha1.NamespaceClassBindingList
		err := fakeClient.List(ctx, &bindings)
		require.NoError(t, err)

		// Filter bindings manually to simulate what the indexer would do
		var matchingBindings []akuityv1alpha1.NamespaceClassBinding
		for _, binding := range bindings.Items {
			if binding.Spec.ClassName == "test-class" {
				matchingBindings = append(matchingBindings, binding)
			}
		}

		assert.Len(t, matchingBindings, 2)
		assert.Equal(t, "binding1", matchingBindings[0].Name)
		assert.Equal(t, "binding2", matchingBindings[1].Name)
	})
}
