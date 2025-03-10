package utils

// This is a copy of github.com/evanphx/json-patch v5.6.0+incompatible but that supports applying patches
// to internal objects. This is required to avoid sequential json marshal/unmarshal for each patch.
// (which make the behavior of the ApplyIgnoreNonExistentPaths method as much effective as ApplyStrict).

import (
	"encoding/json"
	"fmt"
	"strings"

	lazynode "github.com/deckhouse/module-sdk/pkg/utils/lazy-node"
	"github.com/pkg/errors"
)

const (
	eRaw = iota
	eDoc
	eAry
)

var (
	ErrTestFailed   = errors.New("test failed")
	ErrMissing      = errors.New("missing value")
	ErrUnknownType  = errors.New("unknown object type")
	ErrInvalid      = errors.New("invalid state detected")
	ErrInvalidIndex = errors.New("invalid index referenced")
)

type container interface {
	Get(key string) (*lazynode.LazyNode, error)
	Set(key string, val *lazynode.LazyNode) error
	Add(key string, val *lazynode.LazyNode) error
	Remove(key string) error
}

// Operation is a single JSON-Patch step, such as a single 'add' operation.
type Operation map[string]*json.RawMessage

// Kind reads the "op" field of the Operation.
func (o Operation) Kind() string {
	if obj, ok := o["op"]; ok && obj != nil {
		var op string

		err := json.Unmarshal(*obj, &op)
		if err != nil {
			return "unknown"
		}

		return op
	}

	return "unknown"
}

// Path reads the "path" field of the Operation.
func (o Operation) Path() (string, error) {
	if obj, ok := o["path"]; ok && obj != nil {
		var op string

		err := json.Unmarshal(*obj, &op)
		if err != nil {
			return "unknown", err
		}

		return op, nil
	}

	return "unknown", errors.Wrapf(ErrMissing, "operation missing path field")
}

// From reads the "from" field of the Operation.
func (o Operation) From() (string, error) {
	if obj, ok := o["from"]; ok && obj != nil {
		var op string

		err := json.Unmarshal(*obj, &op)
		if err != nil {
			return "unknown", err
		}

		return op, nil
	}

	return "unknown", errors.Wrapf(ErrMissing, "operation, missing from field")
}

func (o Operation) value() *lazynode.LazyNode {
	if obj, ok := o["value"]; ok {
		return lazynode.NewLazyNode(obj)
	}

	return nil
}

// ValueInterface decodes the operation value into an interface.
func (o Operation) ValueInterface() (any, error) {
	if obj, ok := o["value"]; ok && obj != nil {
		var v any

		err := json.Unmarshal(*obj, &v)
		if err != nil {
			return nil, err
		}

		return v, nil
	}

	return nil, errors.Wrapf(ErrMissing, "operation, missing value field")
}

// Patch is an ordered collection of Operations.
type Patch []Operation

// Apply mutates a JSON document according to the patch, and returns the new
// document.
func (p Patch) Apply(doc []byte) ([]byte, error) {
	return p.ApplyIndent(doc, "")
}

func (p Patch) ApplyContainer(pd container) (container, error) {
	if pd == nil {
		return nil, nil
	}

	var err error

	for _, op := range p {
		switch op.Kind() {
		case "add":
			err = p.add(&pd, op)
		case "remove":
			err = p.remove(&pd, op)
		case "replace":
			err = p.replace(&pd, op)
		case "move":
			err = p.move(&pd, op)
		case "test":
			err = p.test(&pd, op)
		case "copy":
			err = p.copy(&pd, op)
		default:
			err = fmt.Errorf("unexpected kind: %s", op.Kind())
		}

		if err != nil {
			return pd, err
		}
	}

	return pd, nil
}

// JSONEqual indicates if 2 JSON documents have the same structural equality.
func JSONEqual(a, b []byte) bool {
	ra := make(json.RawMessage, len(a))
	copy(ra, a)
	la := lazynode.NewLazyNode(&ra)

	rb := make(json.RawMessage, len(b))
	copy(rb, b)
	lb := lazynode.NewLazyNode(&rb)

	return la.Equal(lb)
}

// ApplyIndent mutates a JSON document according to the patch, and returns the new
// document indented.
func (p Patch) ApplyIndent(doc []byte, indent string) ([]byte, error) {
	if len(doc) == 0 {
		return doc, nil
	}

	var pd container
	if doc[0] == '[' {
		pd = &lazynode.PartialArray{}
	} else {
		pd = &lazynode.PartialDoc{}
	}

	err := json.Unmarshal(doc, pd)
	if err != nil {
		return nil, err
	}

	for _, op := range p {
		switch op.Kind() {
		case "add":
			err = p.add(&pd, op)
		case "remove":
			err = p.remove(&pd, op)
		case "replace":
			err = p.replace(&pd, op)
		case "move":
			err = p.move(&pd, op)
		case "test":
			err = p.test(&pd, op)
		case "copy":
			err = p.copy(&pd, op)
		default:
			err = fmt.Errorf("unexpected kind: %s", op.Kind())
		}

		if err != nil {
			return nil, err
		}
	}

	if indent != "" {
		return json.MarshalIndent(pd, "", indent)
	}

	return json.Marshal(pd)
}

func (p Patch) add(doc *container, op Operation) error {
	path, err := op.Path()
	if err != nil {
		return errors.Wrapf(ErrMissing, "add operation failed to decode path")
	}

	con, key := findObject(doc, path)

	if con == nil {
		return errors.Wrapf(ErrMissing, "add operation does not apply: doc is missing path: \"%s\"", path)
	}

	err = con.Add(key, op.value())
	if err != nil {
		return errors.Wrapf(err, "error in add for path: '%s'", path)
	}

	return nil
}

func (p Patch) remove(doc *container, op Operation) error {
	path, err := op.Path()
	if err != nil {
		return errors.Wrapf(ErrMissing, "remove operation failed to decode path")
	}

	con, key := findObject(doc, path)

	if con == nil {
		return errors.Wrapf(ErrMissing, "remove operation does not apply: doc is missing path: \"%s\"", path)
	}

	err = con.Remove(key)
	if err != nil {
		return errors.Wrapf(err, "error in remove for path: '%s'", path)
	}

	return nil
}

func (p Patch) replace(doc *container, op Operation) error {
	path, err := op.Path()
	if err != nil {
		return errors.Wrapf(err, "replace operation failed to decode path")
	}

	if path == "" {
		val := op.value()

		if val.Which() == eRaw {
			if !val.TryDoc() {
				if !val.TryAry() {
					return errors.Wrapf(err, "replace operation value must be object or array")
				}
			}
		}

		switch val.Which() {
		case eAry:
			pArray := val.GetArray()
			*doc = &pArray
		case eDoc:
			pdoc := val.GetDoc()
			*doc = &pdoc
		case eRaw:
			return errors.Wrapf(err, "replace operation hit impossible case")
		}

		return nil
	}

	con, key := findObject(doc, path)

	if con == nil {
		return errors.Wrapf(ErrMissing, "replace operation does not apply: doc is missing path: %s", path)
	}

	_, ok := con.Get(key)
	if ok != nil {
		return errors.Wrapf(ErrMissing, "replace operation does not apply: doc is missing key: %s", path)
	}

	err = con.Set(key, op.value())
	if err != nil {
		return errors.Wrapf(err, "error in remove for path: '%s'", path)
	}

	return nil
}

func (p Patch) move(doc *container, op Operation) error {
	from, err := op.From()
	if err != nil {
		return errors.Wrapf(err, "move operation failed to decode from")
	}

	con, key := findObject(doc, from)

	if con == nil {
		return errors.Wrapf(ErrMissing, "move operation does not apply: doc is missing from path: %s", from)
	}

	val, err := con.Get(key)
	if err != nil {
		return errors.Wrapf(err, "error in move for path: '%s'", key)
	}

	err = con.Remove(key)
	if err != nil {
		return errors.Wrapf(err, "error in move for path: '%s'", key)
	}

	path, err := op.Path()
	if err != nil {
		return errors.Wrapf(err, "move operation failed to decode path")
	}

	con, key = findObject(doc, path)

	if con == nil {
		return errors.Wrapf(ErrMissing, "move operation does not apply: doc is missing destination path: %s", path)
	}

	err = con.Add(key, val)
	if err != nil {
		return errors.Wrapf(err, "error in move for path: '%s'", path)
	}

	return nil
}

func (p Patch) test(doc *container, op Operation) error {
	path, err := op.Path()
	if err != nil {
		return errors.Wrapf(err, "test operation failed to decode path")
	}

	if path == "" {
		var self lazynode.LazyNode

		switch sv := (*doc).(type) {
		case *lazynode.PartialDoc:
			self.SetDoc(*sv)
			self.SetWhich(eDoc)
		case *lazynode.PartialArray:
			self.SetArray(*sv)
			self.SetWhich(eAry)
		}

		if self.Equal(op.value()) {
			return nil
		}

		return errors.Wrapf(ErrTestFailed, "testing value %s failed", path)
	}

	con, key := findObject(doc, path)

	if con == nil {
		return errors.Wrapf(ErrMissing, "test operation does not apply: is missing path: %s", path)
	}

	val, err := con.Get(key)
	if err != nil {
		return errors.Wrapf(err, "error in test for path: '%s'", path)
	}

	if val == nil {
		if op.value().IsRawEmpty() {
			return nil
		}
		return errors.Wrapf(ErrTestFailed, "testing value %s failed", path)
	} else if op.value() == nil {
		return errors.Wrapf(ErrTestFailed, "testing value %s failed", path)
	}

	if val.Equal(op.value()) {
		return nil
	}

	return errors.Wrapf(ErrTestFailed, "testing value %s failed", path)
}

func (p Patch) copy(doc *container, op Operation) error {
	from, err := op.From()
	if err != nil {
		return errors.Wrapf(err, "copy operation failed to decode from")
	}

	con, key := findObject(doc, from)

	if con == nil {
		return errors.Wrapf(ErrMissing, "copy operation does not apply: doc is missing from path: %s", from)
	}

	val, err := con.Get(key)
	if err != nil {
		return errors.Wrapf(err, "error in copy for from: '%s'", from)
	}

	path, err := op.Path()
	if err != nil {
		return errors.Wrapf(ErrMissing, "copy operation failed to decode path")
	}

	con, key = findObject(doc, path)

	if con == nil {
		return errors.Wrapf(ErrMissing, "copy operation does not apply: doc is missing destination path: %s", path)
	}

	valCopy, _, err := lazynode.DeepCopy(val)
	if err != nil {
		return errors.Wrapf(err, "error while performing deep copy")
	}

	err = con.Add(key, valCopy)
	if err != nil {
		return errors.Wrapf(err, "error while adding value during copy")
	}

	return nil
}

func findObject(pd *container, path string) (container, string) {
	doc := *pd

	split := strings.Split(path, "/")

	if len(split) < 2 {
		return nil, ""
	}

	parts := split[1 : len(split)-1]

	key := split[len(split)-1]

	var err error

	for _, part := range parts {
		next, ok := doc.Get(decodePatchKey(part))

		if next == nil || ok != nil {
			return nil, ""
		}

		if next.IsArray() {
			doc, err = next.IntoAry()
			if err != nil {
				return nil, ""
			}
		} else {
			doc, err = next.IntoDoc()
			if err != nil {
				return nil, ""
			}
		}
	}

	return doc, decodePatchKey(key)
}

// From http://tools.ietf.org/html/rfc6901#section-4 :
//
// Evaluation of each reference token begins by decoding any escaped
// character sequence.  This is performed by first transforming any
// occurrence of the sequence '~1' to '/', and then transforming any
// occurrence of the sequence '~0' to '~'.

var rfc6901Decoder = strings.NewReplacer("~1", "/", "~0", "~")

func decodePatchKey(k string) string {
	return rfc6901Decoder.Replace(k)
}
