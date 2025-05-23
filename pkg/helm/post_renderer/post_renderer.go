// Package post_renderer provides a Helm post-renderer for adding extra labels to manifests.
package post_renderer

import (
	"bytes"
	"fmt"
	"maps"

	"sigs.k8s.io/kustomize/kyaml/kio"
)

// PostRenderer adds extra labels to rendered Kubernetes manifests.
type PostRenderer struct {
	extraLabels map[string]string
}

// NewPostRenderer creates a new PostRenderer with the given extra labels.
func NewPostRenderer(extraLabels map[string]string) *PostRenderer {
	return &PostRenderer{
		extraLabels: extraLabels,
	}
}

// Run adds extra labels to all resources in the rendered manifests.
func (p *PostRenderer) Run(renderedManifests *bytes.Buffer) (*bytes.Buffer, error) {
	if len(p.extraLabels) == 0 {
		return renderedManifests, nil
	}

	nodes, err := kio.FromBytes(renderedManifests.Bytes())
	if err != nil {
		return nil, fmt.Errorf("parse rendered manifests failed: %w", err)
	}

	for _, node := range nodes {
		labels := node.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		maps.Copy(labels, p.extraLabels)

		_ = node.SetLabels(labels)
	}

	str, err := kio.StringAll(nodes)
	if err != nil {
		return nil, fmt.Errorf("string all nodes failed: %w", err)
	}

	return bytes.NewBufferString(str), nil
}
