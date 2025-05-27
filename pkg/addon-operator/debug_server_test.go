package addon_operator

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// mockObject is a mock object that can't be converted to unstructured
type mockObject struct {
	// This field contains a function which can't be serialized
	InvalidField func()
}

func (m *mockObject) GetObjectKind() schema.ObjectKind {
	return nil
}

func (m *mockObject) DeepCopyObject() runtime.Object {
	return &mockObject{
		InvalidField: m.InvalidField,
	}
}

func TestRemoveServerManagedFields(t *testing.T) {
	tests := []struct {
		name     string
		input    runtime.Object
		expected runtime.Object
	}{
		{
			name:     "nil object",
			input:    nil,
			expected: nil,
		},
		{
			name: "object with server-managed fields",
			input: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]any{
						"name":              "test-cm",
						"namespace":         "default",
						"generation":        int64(5),
						"resourceVersion":   "12345",
						"uid":               "abc-123-def",
						"creationTimestamp": "2023-01-01T00:00:00Z",
						"managedFields":     []any{},
						"labels": map[string]any{
							"app": "test",
						},
					},
					"data": map[string]any{
						"key": "value",
					},
					"status": map[string]any{
						"phase": "Active",
					},
				},
			},
			expected: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]any{
						"name":              "test-cm",
						"namespace":         "default",
						"resourceVersion":   "12345",
						"uid":               "abc-123-def",
						"creationTimestamp": "2023-01-01T00:00:00Z",
						"labels": map[string]any{
							"app": "test",
						},
					},
					"data": map[string]any{
						"key": "value",
					},
					"status": map[string]any{
						"phase": "Active",
					},
				},
			},
		},
		{
			name: "object without server-managed fields",
			input: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]any{
						"name":      "test-cm",
						"namespace": "default",
						"labels": map[string]any{
							"app": "test",
						},
					},
					"data": map[string]any{
						"key": "value",
					},
				},
			},
			expected: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]any{
						"name":      "test-cm",
						"namespace": "default",
						"labels": map[string]any{
							"app": "test",
						},
					},
					"data": map[string]any{
						"key": "value",
					},
				},
			},
		},
		{
			name: "object with empty metadata",
			input: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata":   map[string]any{},
					"data": map[string]any{
						"key": "value",
					},
				},
			},
			expected: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata":   map[string]any{},
					"data": map[string]any{
						"key": "value",
					},
				},
			},
		},
		{
			name: "object that fails conversion to unstructured",
			input: &mockObject{
				InvalidField: func() {},
			},
			expected: &mockObject{
				InvalidField: func() {},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeServerManagedFields(tt.input)

			// Check if the result is as expected
			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			// Handle case where conversion to unstructured failed
			if tt.name == "object that fails conversion to unstructured" {
				// The function should return the original copy when conversion fails
				if result == tt.input {
					t.Errorf("function should return a copy, not the original object")
				}
				// Both should be mockObject type
				if _, ok := result.(*mockObject); !ok {
					t.Errorf("expected result to be *mockObject, got %T", result)
				}
				return
			}

			// Convert both to unstructured for comparison
			expectedUnstructured, expectedIsUnstructured := tt.expected.(*unstructured.Unstructured)
			resultUnstructured, resultIsUnstructured := result.(*unstructured.Unstructured)

			if !expectedIsUnstructured || !resultIsUnstructured {
				t.Fatalf("expected and result should both be *unstructured.Unstructured for this test case")
			}

			// Check that only specified server-managed fields are removed
			if metadata, found := resultUnstructured.Object["metadata"]; found {
				if metadataMap, ok := metadata.(map[string]any); ok {
					// These fields should be removed
					removedFields := []string{"generation", "managedFields"}
					for _, field := range removedFields {
						if _, hasField := metadataMap[field]; hasField {
							t.Errorf("server-managed field '%s' should be removed but was found", field)
						}
					}

					// These fields should be preserved
					preservedFields := []string{"resourceVersion", "uid", "creationTimestamp"}
					for _, field := range preservedFields {
						if tt.name == "object with server-managed fields" {
							if _, hasField := metadataMap[field]; !hasField {
								t.Errorf("field '%s' should be preserved but was not found", field)
							}
						}
					}
				}
			}

			// Check that status field is preserved (not removed by this function)
			if tt.name == "object with server-managed fields" {
				if _, hasStatus := resultUnstructured.Object["status"]; !hasStatus {
					t.Errorf("status field should be preserved but was not found")
				}
			}

			// Check that other fields are preserved
			if resultUnstructured.GetAPIVersion() != expectedUnstructured.GetAPIVersion() {
				t.Errorf("apiVersion mismatch: expected %s, got %s",
					expectedUnstructured.GetAPIVersion(), resultUnstructured.GetAPIVersion())
			}

			if resultUnstructured.GetKind() != expectedUnstructured.GetKind() {
				t.Errorf("kind mismatch: expected %s, got %s",
					expectedUnstructured.GetKind(), resultUnstructured.GetKind())
			}

			if resultUnstructured.GetName() != expectedUnstructured.GetName() {
				t.Errorf("name mismatch: expected %s, got %s",
					expectedUnstructured.GetName(), resultUnstructured.GetName())
			}

			if resultUnstructured.GetNamespace() != expectedUnstructured.GetNamespace() {
				t.Errorf("namespace mismatch: expected %s, got %s",
					expectedUnstructured.GetNamespace(), resultUnstructured.GetNamespace())
			}

			// Verify that the original object is not modified (deep copy was made)
			if tt.input != nil {
				inputUnstructured, ok := tt.input.(*unstructured.Unstructured)
				if ok {
					if inputMetadata, found := inputUnstructured.Object["metadata"]; found {
						if inputMetadataMap, ok := inputMetadata.(map[string]any); ok {
							// Original object should still have server-managed fields if they were there initially
							if tt.name == "object with server-managed fields" {
								if _, hasGeneration := inputMetadataMap["generation"]; !hasGeneration {
									t.Errorf("original object should not be modified - generation field should still exist")
								}
								if _, hasManagedFields := inputMetadataMap["managedFields"]; !hasManagedFields {
									t.Errorf("original object should not be modified - managedFields should still exist")
								}
							}
						}
					}
				}
			}
		})
	}
}
