package schema

import "github.com/go-openapi/spec"

type CopyTransformer struct{}

func (t *CopyTransformer) Transform(s *spec.Schema) *spec.Schema {
	tmpBytes, _ := s.MarshalJSON()
	res := new(spec.Schema)
	_ = res.UnmarshalJSON(tmpBytes)
	return res
}
