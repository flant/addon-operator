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
	Wrapped interface{}
}

var (
	ErrEmptyWrapped             = errors.New("empty filter result")
	ErrUnmarshalToTypesNotMatch = errors.New("unmarshal error: input and output types not match")
)

func (f Wrapped) UnmarhalTo(v any) error {
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
		return ErrUnmarshalToTypesNotMatch
	}

	rv.Elem().Set(rw.Elem())

	return nil
}

func (f Wrapped) String() string {
	buf := bytes.NewBuffer([]byte{})
	_ = json.NewEncoder(buf).Encode(f.Wrapped)

	res := buf.String()

	if strings.HasPrefix(res, "\"") {
		res = res[1 : len(res)-2]
	}

	return res
}

// type Snapshots map[string][]Wrapped
type Snapshots map[string][]sdkpkg.Snapshot

func (s Snapshots) Get(name string) []sdkpkg.Snapshot {
	return s[name]
}
