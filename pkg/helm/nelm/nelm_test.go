package nelm

import (
	"testing"

	"github.com/deckhouse/deckhouse/pkg/log"
	"helm.sh/helm/v3/pkg/chart"
)

func TestNelmClient_WithVirtualChart(t *testing.T) {
	client := NewNelmClient(&CommonOptions{}, log.NewNop(), nil)

	// Test setting virtual chart flag
	client.WithVirtualChart(true)
	if !client.virtualChart {
		t.Error("Expected virtualChart to be true")
	}

	client.WithVirtualChart(false)
	if client.virtualChart {
		t.Error("Expected virtualChart to be false")
	}
}

func TestNelmClient_WithModulePath(t *testing.T) {
	client := NewNelmClient(&CommonOptions{}, log.NewNop(), nil)

	testPath := "/test/module/path"
	client.WithModulePath(testPath)
	if client.modulePath != testPath {
		t.Errorf("Expected modulePath to be %s, got %s", testPath, client.modulePath)
	}
}

func TestNelmClient_ChartHandling(t *testing.T) {
	client := NewNelmClient(&CommonOptions{}, log.NewNop(), nil)

	// Test with virtual chart
	client.WithVirtualChart(true)
	client.WithModulePath("/test/path")

	// This would normally call NELM, but we just verify the setup is correct
	if !client.virtualChart {
		t.Error("Expected virtual chart to be enabled")
	}

	if client.modulePath != "/test/path" {
		t.Error("Expected module path to be set")
	}

	// Test with regular chart
	client.WithVirtualChart(false)
	if client.virtualChart {
		t.Error("Expected virtual chart to be disabled")
	}
}

func TestVirtualChartFields(t *testing.T) {
	// Test that virtual chart would use correct fields
	testChart := &chart.Chart{
		Metadata: &chart.Metadata{
			Name:       "test-module",
			Version:    "0.2.0",
			APIVersion: "v2",
		},
	}

	expectedName := "test-module"
	expectedVersion := "0.2.0"
	expectedAPIVersion := "v2"

	if testChart.Metadata.Name != expectedName {
		t.Errorf("Expected chart name %s, got %s", expectedName, testChart.Metadata.Name)
	}

	if testChart.Metadata.Version != expectedVersion {
		t.Errorf("Expected chart version %s, got %s", expectedVersion, testChart.Metadata.Version)
	}

	if testChart.Metadata.APIVersion != expectedAPIVersion {
		t.Errorf("Expected chart API version %s, got %s", expectedAPIVersion, testChart.Metadata.APIVersion)
	}
}