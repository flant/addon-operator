package post_renderer

import (
	"bytes"
	"testing"

	. "github.com/onsi/gomega"
)

func Test_Run(t *testing.T) {
	t.Run("explicit Helm3", func(t *testing.T) {
		g := NewWithT(t)
		inputManifests := `apiVersion: ""
kind: pod
metadata:
  name: pod1
---
apiVersion: ""
kind: statefulset
metadata:
  name: pod2
  annotations:
    new: pod
  labels:
    new: pod
---
apiVersion: ""
kind: deployment
metadata:
  name: pod3
  labels:
    heritage: legacy`

		expectedManifests := `apiVersion: ""
kind: pod
metadata:
  name: pod1
  labels:
    heritage: addon-operator
---
apiVersion: ""
kind: statefulset
metadata:
  name: pod2
  annotations:
    new: pod
  labels:
    heritage: addon-operator
    new: pod
---
apiVersion: ""
kind: deployment
metadata:
  name: pod3
  labels:
    heritage: addon-operator
`
		buf := bytes.NewBufferString(inputManifests)
		renderer := NewPostRenderer(map[string]string{
			"heritage": "addon-operator",
		})
		out, err := renderer.Run(buf)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(out.String()).Should(Equal(expectedManifests))
	})
}
