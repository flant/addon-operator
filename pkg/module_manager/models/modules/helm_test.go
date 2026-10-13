package modules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildConversionWebhookChartMirrorsWebhooks(t *testing.T) {
	modulePath := t.TempDir()
	dst := t.TempDir()

	files := map[string]string{
		"templates/_helpers.tpl":                  `{{- define "x" }}x{{- end }}`,
		"templates/webhooks/conversion.yaml":      "kind: ConversionWebhook",
		"templates/deployment.yaml":               "kind: Deployment",
		"charts/helm_lib/Chart.yaml":              "name: helm_lib",
		"webhooks/conversion/projecttemplates.py": "print('ok')",
	}

	for rel, content := range files {
		path := filepath.Join(modulePath, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}

	require.NoError(t, buildConversionWebhookChart(modulePath, dst, []string{"webhooks/conversion.yaml"}))

	for _, rel := range []string{
		"templates/_helpers.tpl",
		"templates/webhooks/conversion.yaml",
		"charts/helm_lib/Chart.yaml",
		"webhooks/conversion/projecttemplates.py",
	} {
		got, err := os.ReadFile(filepath.Join(dst, rel))
		require.NoError(t, err, rel)
		require.Equal(t, files[rel], string(got), rel)
	}

	require.NoFileExists(t, filepath.Join(dst, "templates/deployment.yaml"))
}

func TestBuildConversionWebhookChartWithoutOptionalDirs(t *testing.T) {
	modulePath := t.TempDir()
	dst := t.TempDir()

	tpl := filepath.Join(modulePath, "templates", "conversion.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(tpl), 0o755))
	require.NoError(t, os.WriteFile(tpl, []byte("kind: ConversionWebhook"), 0o644))

	require.NoError(t, buildConversionWebhookChart(modulePath, dst, []string{"conversion.yaml"}))
	require.NoDirExists(t, filepath.Join(dst, "webhooks"))
	require.NoDirExists(t, filepath.Join(dst, "charts"))
}
