package post_renderer

import (
	"bytes"
	"fmt"

	"sigs.k8s.io/kustomize/kyaml/kio"
)

type PostRenderer struct {
	ExtraLabels map[string]string
}

func (p *PostRenderer) Run(renderedManifests *bytes.Buffer) (*bytes.Buffer, error) {
	if len(p.ExtraLabels) == 0 {
		return renderedManifests, nil
	}

	nodes, err := kio.FromBytes(renderedManifests.Bytes())
	if err != nil {
		return nil, fmt.Errorf("parse rendered manifests failed: %w", err)
	}

	for _, node := range nodes {
		labels := node.GetLabels()
		for k, v := range p.ExtraLabels {
			labels[k] = v
		}
		node.SetLabels(labels)
	}

	str, err := kio.StringAll(nodes)
	if err != nil {
		return nil, fmt.Errorf("string all nodes failed: %w", err)
	}

	return bytes.NewBufferString(str), nil
}
