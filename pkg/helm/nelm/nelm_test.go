package nelm

import (
	"testing"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/stretchr/testify/assert"
)

func TestNewNelmClient(t *testing.T) {
	tests := []struct {
		name     string
		opts     *CommonOptions
		labels   map[string]string
		wantOpts *CommonOptions
	}{
		{
			name: "with default options",
			opts: nil,
			wantOpts: &CommonOptions{
				HistoryMax: 10,
			},
		},
		{
			name: "with custom options",
			opts: &CommonOptions{
				HistoryMax:  20,
				Timeout:     time.Second * 30,
				HelmDriver:  "secret",
				KubeContext: "test-context",
			},
			wantOpts: &CommonOptions{
				HistoryMax:  20,
				Timeout:     time.Second * 30,
				HelmDriver:  "secret",
				KubeContext: "test-context",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := log.NewLogger(log.Options{})
			client := NewNelmClient(tt.opts, logger, tt.labels)
			assert.NotNil(t, client)
			assert.Equal(t, tt.wantOpts.HistoryMax, client.opts.HistoryMax)
			assert.Equal(t, tt.wantOpts.Timeout, client.opts.Timeout)
			assert.Equal(t, tt.wantOpts.HelmDriver, client.opts.HelmDriver)
			assert.Equal(t, tt.wantOpts.KubeContext, client.opts.KubeContext)
		})
	}
}

func TestWithExtraLabels(t *testing.T) {
	tests := []struct {
		name           string
		initialLabels  map[string]string
		extraLabels    map[string]string
		expectedLabels map[string]string
	}{
		{
			name:           "add labels to empty map",
			initialLabels:  nil,
			extraLabels:    map[string]string{"key1": "value1"},
			expectedLabels: map[string]string{"key1": "value1"},
		},
		{
			name:           "merge labels",
			initialLabels:  map[string]string{"key1": "value1"},
			extraLabels:    map[string]string{"key2": "value2"},
			expectedLabels: map[string]string{"key1": "value1", "key2": "value2"},
		},
		{
			name:           "override existing labels",
			initialLabels:  map[string]string{"key1": "old"},
			extraLabels:    map[string]string{"key1": "new"},
			expectedLabels: map[string]string{"key1": "new"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &NelmClient{
				labels: tt.initialLabels,
			}
			client.WithExtraLabels(tt.extraLabels)
			assert.Equal(t, tt.expectedLabels, client.labels)
		})
	}
}

func TestWithLogLabels(t *testing.T) {
	logger := log.NewLogger(log.Options{})
	client := &NelmClient{
		logger: logger,
	}

	logLabels := map[string]string{
		"component": "test",
		"version":   "1.0",
	}

	client.WithLogLabels(logLabels)
	assert.NotNil(t, client.logger)
}

func createTestClient() *NelmClient {
	opts := &CommonOptions{
		HistoryMax:  10,
		Timeout:     time.Second * 30,
		HelmDriver:  "secret",
		KubeContext: "test-context",
	}
	opts.Namespace = stringPtr("default")
	return &NelmClient{
		logger: log.NewLogger(log.Options{}),
		opts:   opts,
		labels: make(map[string]string),
	}
}

func stringPtr(s string) *string {
	return &s
}

func TestGetReleaseLabels(t *testing.T) {
	tests := []struct {
		name      string
		release   string
		labelName string
		wantValue string
		wantError bool
	}{
		{
			name:      "non-existent release",
			release:   "non-existent",
			labelName: "test-label",
			wantError: true,
		},
	}

	client := createTestClient()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := client.GetReleaseLabels(tt.release, tt.labelName)
			if tt.wantError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantValue, value)
		})
	}
}

func TestLastReleaseStatus(t *testing.T) {
	tests := []struct {
		name       string
		release    string
		wantRev    string
		wantStatus string
		wantError  bool
	}{
		{
			name:      "non-existent release",
			release:   "non-existent",
			wantRev:   "0",
			wantError: true,
		},
	}

	client := createTestClient()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rev, status, err := client.LastReleaseStatus(tt.release)
			if tt.wantError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantRev, rev)
			assert.Equal(t, tt.wantStatus, status)
		})
	}
}

func TestIsReleaseExists(t *testing.T) {
	tests := []struct {
		name      string
		release   string
		want      bool
		wantError bool
	}{
		{
			name:      "non-existent release",
			release:   "non-existent",
			want:      false,
			wantError: true,
		},
	}

	client := createTestClient()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exists, err := client.IsReleaseExists(tt.release)
			if tt.wantError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, exists)
		})
	}
}

func TestListReleasesNames(t *testing.T) {
	client := createTestClient()

	names, err := client.ListReleasesNames()
	assert.Error(t, err)
	assert.Nil(t, names)
}

func TestUpgradeRelease(t *testing.T) {
	tests := []struct {
		name        string
		releaseName string
		chartName   string
		valuesPaths []string
		setValues   []string
		labels      map[string]string
		namespace   string
		wantError   bool
	}{
		{
			name:        "invalid chart",
			releaseName: "test-release",
			chartName:   "invalid-chart",
			valuesPaths: []string{},
			setValues:   []string{},
			labels:      map[string]string{},
			namespace:   "default",
			wantError:   true,
		},
	}

	client := createTestClient()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.UpgradeRelease(tt.releaseName, tt.chartName, tt.valuesPaths, tt.setValues, tt.labels, tt.namespace)
			if tt.wantError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestGetReleaseValues(t *testing.T) {
	tests := []struct {
		name      string
		release   string
		wantError bool
	}{
		{
			name:      "non-existent release",
			release:   "non-existent",
			wantError: true,
		},
	}

	client := createTestClient()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, err := client.GetReleaseValues(tt.release)
			if tt.wantError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, values)
		})
	}
}

func TestGetReleaseChecksum(t *testing.T) {
	tests := []struct {
		name      string
		release   string
		wantError bool
	}{
		{
			name:      "non-existent release",
			release:   "non-existent",
			wantError: true,
		},
	}

	client := createTestClient()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checksum, err := client.GetReleaseChecksum(tt.release)
			if tt.wantError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.NotEmpty(t, checksum)
		})
	}
}

func TestRender(t *testing.T) {
	tests := []struct {
		name        string
		releaseName string
		chartName   string
		valuesPaths []string
		setValues   []string
		namespace   string
		debug       bool
		wantError   bool
	}{
		{
			name:        "invalid chart",
			releaseName: "test-release",
			chartName:   "invalid-chart",
			valuesPaths: []string{},
			setValues:   []string{},
			namespace:   "default",
			debug:       false,
			wantError:   true,
		},
	}

	client := createTestClient()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := client.Render(tt.releaseName, tt.chartName, tt.valuesPaths, tt.setValues, tt.namespace, tt.debug)
			if tt.wantError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.NotEmpty(t, result)
		})
	}
}
