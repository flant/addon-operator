package diff

import (
	"io"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kubectldiff "k8s.io/kubectl/pkg/cmd/diff"
	"sigs.k8s.io/yaml"
)

// Re-export types from kubectl diff package
type (
	DiffProgram = kubectldiff.DiffProgram
	InfoObject  = kubectldiff.InfoObject
	Object      = kubectldiff.Object
)

// Differ wraps the kubectl Differ with custom printer support
type Differ struct {
	*kubectldiff.Differ
}

// NewDiffer creates a new Differ instance
func NewDiffer(from, to string) (*Differ, error) {
	d, err := kubectldiff.NewDiffer(from, to)
	if err != nil {
		return nil, err
	}
	return &Differ{Differ: d}, nil
}

// Diff performs diff with custom printer that removes metadata.generation
func (d *Differ) Diff(obj Object, printer Printer, showManagedFields bool) error {
	// Use standard kubectl printer, but modify the object first
	kubectlPrinter := kubectldiff.Printer{}

	// Create a wrapper object that pre-processes the Live and Merged methods
	objWrapper := &objectWrapper{
		Object:  obj,
		printer: printer,
	}

	return d.Differ.Diff(objWrapper, kubectlPrinter, showManagedFields)
}

// objectWrapper wraps an Object to preprocess objects before printing
type objectWrapper struct {
	Object
	printer Printer
}

func (ow *objectWrapper) Live() runtime.Object {
	obj := ow.Object.Live()
	return ow.preprocessObject(obj)
}

func (ow *objectWrapper) Merged() (runtime.Object, error) {
	obj, err := ow.Object.Merged()
	if err != nil {
		return nil, err
	}
	return ow.preprocessObject(obj), nil
}

func (ow *objectWrapper) preprocessObject(obj runtime.Object) runtime.Object {
	if obj == nil {
		return obj
	}

	// Remove metadata.generation field if present
	if u, err := toUnstructured(obj); err == nil && u != nil {
		unstructured.RemoveNestedField(u.Object, "metadata", "generation")
		return u
	}

	return obj
}

// Printer is used to print an object with custom metadata.generation handling
type Printer struct{}

// Print the object inside the writer w.
// This differs from the standard kubectl diff Printer by removing metadata.generation field
func (p *Printer) Print(obj runtime.Object, w io.Writer) error {
	if obj == nil {
		return nil
	}

	// Remove metadata.generation field if present
	if u, err := toUnstructured(obj); err == nil && u != nil {
		unstructured.RemoveNestedField(u.Object, "metadata", "generation")
		obj = u
	}

	data, err := yaml.Marshal(obj)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// toUnstructured converts a runtime.Object into an unstructured.Unstructured object.
func toUnstructured(obj runtime.Object) (*unstructured.Unstructured, error) {
	if unstr, ok := obj.(*unstructured.Unstructured); ok {
		return unstr, nil
	}
	data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: data}, nil
}
