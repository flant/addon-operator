package post_renderer

import (
	"bytes"
	"fmt"

	"sigs.k8s.io/kustomize/kyaml/kio"
)

type PostRenderer struct {
	ExtraLabels map[string]string
}

func (p *PostRenderer) Run(renderedManifests *bytes.Buffer) (modifiedManifests *bytes.Buffer, err error) {
	if len(p.ExtraLabels) == 0 {
		return renderedManifests, nil
	}
	nodes, err := kio.FromBytes(renderedManifests.Bytes())
	if err != nil {
		return nil, fmt.Errorf("parse rendered manifests failed: %v", err)
	}
	for _, v := range nodes {
		labels := v.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		for k, v := range p.ExtraLabels {
			labels[k] = v
		}
		v.SetLabels(labels)
	}

	str, err := kio.StringAll(nodes)
	if err != nil {
		return nil, fmt.Errorf("string all nodes failed: %v", err)
	}

	return bytes.NewBufferString(str), nil
}
