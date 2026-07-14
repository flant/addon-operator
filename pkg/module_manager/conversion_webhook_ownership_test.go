package module_manager

import (
	"testing"

	"github.com/flant/kube-client/manifest"
	"github.com/stretchr/testify/assert"
)

func TestSetHelmOwnershipMetadata(t *testing.T) {
	const (
		releaseName = "log-shipper"
		releaseNs   = "d8-system"
	)

	assertOwnership := func(t *testing.T, m manifest.Manifest) {
		t.Helper()

		meta := m["metadata"].(map[string]interface{})

		labels := meta["labels"].(map[string]interface{})
		assert.Equal(t, "Helm", labels[helmManagedByLabel])

		annotations := meta["annotations"].(map[string]interface{})
		assert.Equal(t, releaseName, annotations[helmReleaseNameAnnotation])
		assert.Equal(t, releaseNs, annotations[helmReleaseNamespaceAnnotation])
	}

	t.Run("adds metadata when labels and annotations are absent", func(t *testing.T) {
		m := manifest.Manifest{
			"apiVersion": "deckhouse.io/v1alpha1",
			"kind":       "ConversionWebhook",
			"metadata": map[string]interface{}{
				"name": "clusterlogdestinations.deckhouse.io",
			},
		}

		setHelmOwnershipMetadata(m, releaseName, releaseNs)

		assertOwnership(t, m)
	})

	t.Run("preserves existing labels and annotations", func(t *testing.T) {
		m := manifest.Manifest{
			"kind": "ConversionWebhook",
			"metadata": map[string]interface{}{
				"name": "clusterlogdestinations.deckhouse.io",
				"labels": map[string]interface{}{
					"heritage": "deckhouse",
					"module":   "log-shipper",
				},
				"annotations": map[string]interface{}{
					"some.io/keep": "value",
				},
			},
		}

		setHelmOwnershipMetadata(m, releaseName, releaseNs)

		assertOwnership(t, m)
		meta := m["metadata"].(map[string]interface{})
		labels := meta["labels"].(map[string]interface{})
		assert.Equal(t, "deckhouse", labels["heritage"])
		assert.Equal(t, "log-shipper", labels["module"])
		annotations := meta["annotations"].(map[string]interface{})
		assert.Equal(t, "value", annotations["some.io/keep"])
	})

	t.Run("is idempotent on an already-owned object", func(t *testing.T) {
		m := manifest.Manifest{
			"kind": "ConversionWebhook",
			"metadata": map[string]interface{}{
				"name": "clusterlogdestinations.deckhouse.io",
				"labels": map[string]interface{}{
					helmManagedByLabel: "Helm",
				},
				"annotations": map[string]interface{}{
					helmReleaseNameAnnotation:      releaseName,
					helmReleaseNamespaceAnnotation: releaseNs,
				},
			},
		}

		setHelmOwnershipMetadata(m, releaseName, releaseNs)

		assertOwnership(t, m)
	})

	t.Run("creates metadata when it is missing entirely", func(t *testing.T) {
		m := manifest.Manifest{
			"kind": "ConversionWebhook",
		}

		setHelmOwnershipMetadata(m, releaseName, releaseNs)

		assertOwnership(t, m)
	})
}
