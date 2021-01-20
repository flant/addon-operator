package validation

import (
	"io/ioutil"
	"testing"

	. "github.com/onsi/gomega"
)

func Test_Add_Schema(t *testing.T) {
	g := NewWithT(t)

	st := NewSchemaStorage()

	schemaBytes, err := ioutil.ReadFile("testdata/test-schema-ok.yaml")
	g.Expect(err).ShouldNot(HaveOccurred())

	err = st.AddGlobalValuesSchemas(schemaBytes, nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	s := st.GlobalValuesSchema(ConfigValuesSchema)
	g.Expect(s).ShouldNot(BeNil(), "schema for config should be in cache")

	s = st.GlobalValuesSchema(ValuesSchema)
	g.Expect(s).Should(BeNil(), "schema for values should not be in cache")

	s = st.GlobalValuesSchema(HelmValuesSchema)
	g.Expect(s).Should(BeNil(), "schema for helm should not be in cache")
}

// go-openapi packages have no method to validate schema without full swagger document.
func Test_Add_Schema_Bad(t *testing.T) {
	t.SkipNow()
	g := NewWithT(t)
	st := NewSchemaStorage()

	schemaBytes, err := ioutil.ReadFile("testdata/test-schema-bad.yaml")
	g.Expect(err).ShouldNot(HaveOccurred())

	err = st.AddGlobalValuesSchemas(nil, schemaBytes)
	t.Logf("%v", err)
	g.Expect(err).Should(HaveOccurred(), "invalid schema should not be loaded")

}
