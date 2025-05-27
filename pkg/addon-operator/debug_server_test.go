package addon_operator

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestRemoveGenerationField(t *testing.T) {
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
			name: "object with generation field",
			input: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]any{
						"name":       "test-cm",
						"namespace":  "default",
						"generation": int64(5),
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
			name: "object without generation field",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeGenerationField(tt.input)

			// Check if the result is as expected
			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			// Convert both to unstructured for comparison
			expectedUnstructured, ok := tt.expected.(*unstructured.Unstructured)
			if !ok {
				t.Fatalf("expected result should be *unstructured.Unstructured")
			}

			resultUnstructured, ok := result.(*unstructured.Unstructured)
			if !ok {
				t.Fatalf("result should be *unstructured.Unstructured")
			}

			// Check that generation field is removed
			if metadata, found := resultUnstructured.Object["metadata"]; found {
				if metadataMap, ok := metadata.(map[string]any); ok {
					if _, hasGeneration := metadataMap["generation"]; hasGeneration {
						t.Errorf("generation field should be removed but was found")
					}
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
							// Original object should still have generation if it was there initially
							if tt.name == "object with generation field" {
								if _, hasGeneration := inputMetadataMap["generation"]; !hasGeneration {
									t.Errorf("original object should not be modified - generation field should still exist")
								}
							}
						}
					}
				}
			}
		})
	}
}
