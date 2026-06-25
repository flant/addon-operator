package go_hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	sdkpkg "github.com/deckhouse/module-sdk/pkg"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type FilterFunc func(*unstructured.Unstructured) (FilterResult, error)

type FilterResult any

type Wrapped struct {
	Wrapped any
}

var (
	ErrEmptyWrapped             = errors.New("empty filter result")
	ErrUnmarshalToTypesNotMatch = errors.New("input and output types not match")
)

func (f *Wrapped) UnmarshalTo(v any) error {
	if f.Wrapped == nil {
		return ErrEmptyWrapped
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		// error replace with "not pointer"
		return fmt.Errorf("reflect.TypeOf(v): %s", reflect.TypeOf(v))
	}

	rw := reflect.ValueOf(f.Wrapped)
	if rw.Type().AssignableTo(rv.Elem().Type()) {
		rv.Elem().Set(rw)
		return nil
	}

	if rw.Kind() == reflect.Pointer {
		if rw.IsNil() {
			rv.Elem().Set(reflect.Zero(rv.Elem().Type()))
			return nil
		}
		rw = rw.Elem()
	}

	if !rw.Type().AssignableTo(rv.Elem().Type()) {
		return ErrUnmarshalToTypesNotMatch
	}

	rv.Elem().Set(rw)
	return nil
}

func (f *Wrapped) UnmarshalToWithoutAssignable(v any) error {
	if f.Wrapped == nil {
		return ErrEmptyWrapped
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		// error replace with "not pointer"
		return fmt.Errorf("reflect.TypeOf(v): %s", reflect.TypeOf(v))
	}

	rw := reflect.ValueOf(f.Wrapped)
	targetType := rv.Elem().Type()
	wrappedType := rw.Type()

	// Handle nil pointer case
	if rw.Kind() == reflect.Pointer && rw.IsNil() {
		rv.Elem().Set(reflect.Zero(targetType))
		return nil
	}

	// Check for exact type match
	if wrappedType == targetType {
		rv.Elem().Set(rw)
		return nil
	}

	// Handle pointer to value conversion
	if rw.Kind() == reflect.Pointer {
		rw = rw.Elem()
		wrappedType = rw.Type()
	}

	// Check for exact type match after dereferencing
	if wrappedType == targetType {
		rv.Elem().Set(rw)
		return nil
	}

	return ErrUnmarshalToTypesNotMatch
}

func (f *Wrapped) UnmarshalToOld(v any) error {
	if f.Wrapped == nil {
		return ErrEmptyWrapped
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		// error replace with "not pointer"
		return fmt.Errorf("reflect.TypeOf(v): %s", reflect.TypeOf(v))
	}

	rw := reflect.ValueOf(f.Wrapped)
	if rw.Kind() != reflect.Pointer || rw.IsNil() {
		rv.Elem().Set(rw)

		return nil
	}

	if rw.Type() != rv.Type() {
		return fmt.Errorf("input type: %s, wrapped type: %s: %w",
			rv.Type(), rw.Type(), ErrUnmarshalToTypesNotMatch)
	}

	rv.Elem().Set(rw.Elem())

	return nil
}

func (f *Wrapped) String() string {
	buf := bytes.NewBuffer([]byte{})
	_ = json.NewEncoder(buf).Encode(f.Wrapped)

	res := buf.String()

	res = strings.TrimSuffix(res, "\n")

	if strings.HasPrefix(res, "\"") {
		res = res[1 : len(res)-1]
	}

	return res
}

// type Snapshots map[string][]Wrapped
type Snapshots map[string][]sdkpkg.Snapshot

func (s Snapshots) Get(name string) []sdkpkg.Snapshot {
	return s[name]
}
