package validation

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-openapi/swag"
	. "github.com/onsi/gomega"
	"github.com/tidwall/gjson"
)

func Test_Add_Schema(t *testing.T) {
	g := NewWithT(t)

	st := NewSchemaStorage()

	schemaBytes, err := os.ReadFile("testdata/test-schema-ok.yaml")
	g.Expect(err).ShouldNot(HaveOccurred())

	err = st.AddGlobalValuesSchemas(schemaBytes, nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	s := st.GlobalValuesSchema(ConfigValuesSchema)
	g.Expect(s).ShouldNot(BeNil(), "schema for config should be in cache")

	res, err := toGJSON(s)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Check own fields.
	g.Expect(res.Get("properties.clusterName").Exists()).Should(BeTrue(), "should load clusterName, got: %s", res.Raw)
	g.Expect(res.Get("properties.project.properties.version").Exists()).Should(BeTrue(), "should load project.version, got: %s", res.Raw)
	g.Expect(res.Get("properties.project.properties.name").Exists()).Should(BeTrue(), "should load project.name, got: %s", res.Raw)
	// Check anchored fields.
	g.Expect(res.Get("properties.activeProject.properties.version").Exists()).Should(BeTrue(), "should load anchored activeProject.version, got: %s", res.Raw)
	g.Expect(res.Get("properties.archive.items.properties.version").Exists()).Should(BeTrue(), "should load anchored archive.items.version, got: %s", res.Raw)
	g.Expect(res.Get("properties.archive.items.properties.description").Exists()).Should(BeTrue(), "should load archive.items.description, got: %s", res.Raw)
	// Check ref-ed schema with anchors.
	g.Expect(res.Get("properties.externalProjects").Exists()).Should(BeTrue(), "should exists 'externalProjects' field")
	g.Expect(res.Get("properties.externalProjects.properties.activeProject.properties.version").Exists()).Should(BeTrue(), "should load anchored externalProjects.activeProject.version, got: %s", res.Raw)
	g.Expect(res.Get("properties.externalProjects.properties.archive.items.properties.version").Exists()).Should(BeTrue(), "should load anchored externalProjects.archive.items.version, got: %s", res.Raw)
	g.Expect(res.Get("properties.externalProjects.properties.archive.items.properties.description").Exists()).Should(BeTrue(), "should load externalProjects.archive.items.description, got: %s", res.Raw)
	// Check ref-ed fragment with anchors.
	g.Expect(res.Get("properties.fragmentedProjects").Exists()).Should(BeTrue(), "should exists 'fragmentedProjects' field")
	g.Expect(res.Get("properties.fragmentedProjects.properties.activeProject.properties.version").Exists()).Should(BeTrue(), "should load anchored fragmentedProjects.activeProject.version, got: %s", res.Raw)
	g.Expect(res.Get("properties.fragmentedProjects.properties.archive.items.properties.version").Exists()).Should(BeTrue(), "should load anchored and local ref-ed fragmentedProjects.archive.items.version, got: %s", res.Raw)
	g.Expect(res.Get("properties.fragmentedProjects.properties.archive.items.properties.version.type").String()).Should(Equal("number"), "should load type for anchored and local ref-ed fragmentedProjects.archive.items.version, got: %s", res.Raw)
	g.Expect(res.Get("properties.fragmentedProjects.properties.archive.items.properties.description").Exists()).Should(BeTrue(), "should load local ref-ed fragmentedProjects.archive.items.description, got: %s", res.Raw)
	g.Expect(res.Get("properties.fragmentedProjects.properties.archive.items.properties.description.type").String()).Should(Equal("string"), "should load type for local ref-ed fragmentedProjects.archive.items.description, got: %s", res.Raw)

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

	schemaBytes, err := os.ReadFile("testdata/test-schema-bad.yaml")
	g.Expect(err).ShouldNot(HaveOccurred())

	err = st.AddGlobalValuesSchemas(nil, schemaBytes)
	t.Logf("%v", err)
	g.Expect(err).Should(HaveOccurred(), "invalid schema should not be loaded")
}

func TestMapMergeAnchor(t *testing.T) {
	// 'project' has two properties: 'name' and 'version', they are "catch" by an anchor.
	// 'activeProject' uses anchor to "copy" all properties from the 'project'.
	// 'archive' items are projects with additional property 'descriptio'. So anchor is used
	// in "merge map" mode to "copy" 'name' and 'version' and then add 'description'.
	schemaWithMapMerge := `
type: object
properties:
  project:
    type: object
    properties: &common_project
      name:
        type: string
      version:
        type: number
  activeProject:
    type: object
    properties: *common_project
  archive:
    type: array
    items:
      type: object
      properties:
        <<: *common_project
        description:
          type: string
`

	g := NewWithT(t)

	// Check swag loader via yaml.MapSlice.
	yml, err := swag.BytesToYAMLDoc([]byte(schemaWithMapMerge))
	g.Expect(err).ShouldNot(HaveOccurred())
	doc, err := swag.YAMLToJSON(yml)
	g.Expect(err).ShouldNot(HaveOccurred())
	res, err := toGJSON(doc)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(res.Get("properties.activeProject.properties.version").Exists()).Should(BeTrue(), "should load anchored activeProject.version, got: %s", res.Raw)
	g.Expect(res.Get("properties.archive.items.properties.version").Exists()).Should(BeFalse(), "WOW! go-openapi/swag is now fixed: it loads anchored archive.items.version, remove additional loader in schemas.go")
	g.Expect(res.Get("properties.archive.items.properties.description").Exists()).Should(BeTrue(), "should load archive.items.description, got: %s", res.Raw)

	// Check custom loader via interface{}.
	doc, err = YAMLBytesToJSONDoc([]byte(schemaWithMapMerge))
	g.Expect(err).ShouldNot(HaveOccurred())
	res, err = toGJSON(doc)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(res.Get("properties.activeProject.properties.version").Exists()).Should(BeTrue(), "should load anchored activeProject.version, got: %s", res.Raw)
	g.Expect(res.Get("properties.archive.items.properties.version").Exists()).Should(BeTrue(), "should load anchored archive.items.version, got: %s", res.Raw)
	g.Expect(res.Get("properties.archive.items.properties.description").Exists()).Should(BeTrue(), "should load archive.items.description, got: %s", res.Raw)
}

func toGJSON(obj interface{}) (*gjson.Result, error) {
	// Marshal to bytes and put into gjson.
	schemaBytes, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}

	res := gjson.ParseBytes(schemaBytes)
	return &res, nil
}

func TestMe(t *testing.T) {
	cb := ``

	vb := ``

	vv := NewValuesValidator()
	err := vv.SchemaStorage.AddModuleValuesSchemas("parca", []byte(cb), []byte(vb))
	require.NoError(t, err)

	fmt.Println(vv.SchemaStorage.ModuleValuesSchema("parca", ConfigValuesSchema))
}
