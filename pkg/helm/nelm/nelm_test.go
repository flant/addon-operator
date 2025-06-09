package nelm

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	helmrelease "github.com/werf/3p-helm/pkg/release"
	"github.com/werf/nelm/pkg/action"
)

func strPtr(s string) *string {
	return &s
}

// MockNelmActions is a mock implementation of NelmActions interface
type MockNelmActions struct {
	mock.Mock
}

func (m *MockNelmActions) ReleaseGet(ctx context.Context, releaseName string, namespace string, opts action.ReleaseGetOptions) (*action.ReleaseGetResultV1, error) {
	args := m.Called(ctx, releaseName, namespace, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*action.ReleaseGetResultV1), args.Error(1)
}

func (m *MockNelmActions) ReleaseInstall(ctx context.Context, releaseName string, namespace string, opts action.ReleaseInstallOptions) error {
	args := m.Called(ctx, releaseName, namespace, opts)
	return args.Error(0)
}

func (m *MockNelmActions) ReleaseUninstall(ctx context.Context, releaseName string, namespace string, opts action.ReleaseUninstallOptions) error {
	args := m.Called(ctx, releaseName, namespace, opts)
	return args.Error(0)
}

func (m *MockNelmActions) ReleaseList(ctx context.Context, opts action.ReleaseListOptions) (*action.ReleaseListResultV1, error) {
	args := m.Called(ctx, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*action.ReleaseListResultV1), args.Error(1)
}

func (m *MockNelmActions) ChartRender(ctx context.Context, opts action.ChartRenderOptions) (*action.ChartRenderResultV1, error) {
	args := m.Called(ctx, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*action.ChartRenderResultV1), args.Error(1)
}

func (m *MockNelmActions) ReleasePlanInstall(ctx context.Context, releaseName string, namespace string, opts action.ReleasePlanInstallOptions) error {
	args := m.Called(ctx, releaseName, namespace, opts)
	return args.Error(0)
}

func TestNewNelmClient(t *testing.T) {
	tests := []struct {
		name     string
		opts     *CommonOptions
		labels   map[string]string
		actions  NelmActions
		wantOpts *CommonOptions
	}{
		{
			name: "with default options",
			opts: &CommonOptions{
				HistoryMax:  5,
				Timeout:     time.Second * 30,
				HelmDriver:  "secret",
				KubeContext: "test-context",
			},
			labels: map[string]string{"test": "label"},
			wantOpts: &CommonOptions{
				HistoryMax:  5,
				Timeout:     time.Second * 30,
				HelmDriver:  "secret",
				KubeContext: "test-context",
			},
		},
		{
			name: "with nil options",
			opts: nil,
			wantOpts: &CommonOptions{
				HistoryMax: 10,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := log.NewLogger(log.Options{})
			client := NewNelmClient(tt.opts, logger, tt.labels)
			client.actions = tt.actions
			assert.NotNil(t, client)
			if tt.opts != nil {
				assert.Equal(t, tt.wantOpts.HistoryMax, client.opts.HistoryMax)
				assert.Equal(t, tt.wantOpts.Timeout, client.opts.Timeout)
				assert.Equal(t, tt.wantOpts.HelmDriver, client.opts.HelmDriver)
				assert.Equal(t, tt.wantOpts.KubeContext, client.opts.KubeContext)
			} else {
				assert.Equal(t, tt.wantOpts.HistoryMax, client.opts.HistoryMax)
			}
		})
	}
}

func TestGetReleaseLabels(t *testing.T) {
	tests := []struct {
		name        string
		releaseName string
		labelName   string
		mockResult  *action.ReleaseGetResultV1
		mockError   error
		want        string
		wantErr     bool
	}{
		{
			name:        "successful get label",
			releaseName: "test-release",
			labelName:   "test",
			mockResult: &action.ReleaseGetResultV1{
				Release: &action.ReleaseGetResultRelease{
					Name:        "test-release",
					Namespace:   "test-namespace",
					Annotations: map[string]string{"test": "label"},
				},
			},
			mockError: nil,
			want:      "label",
			wantErr:   false,
		},
		{
			name:        "label not found",
			releaseName: "test-release",
			labelName:   "nonexistent",
			mockResult: &action.ReleaseGetResultV1{
				Release: &action.ReleaseGetResultRelease{
					Name:        "test-release",
					Namespace:   "test-namespace",
					Annotations: map[string]string{},
				},
			},
			mockError: nil,
			want:      "",
			wantErr:   true,
		},
		{
			name:        "release not found",
			releaseName: "test-release",
			labelName:   "test",
			mockResult:  nil,
			mockError:   &action.ReleaseNotFoundError{},
			want:        "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockActions := new(MockNelmActions)
			mockActions.On("ReleaseGet", mock.Anything, tt.releaseName, mock.MatchedBy(func(ns string) bool { return true }), mock.Anything).
				Return(tt.mockResult, tt.mockError)

			client := NewNelmClient(&CommonOptions{ConfigFlags: genericclioptions.ConfigFlags{Namespace: strPtr("default")}}, log.NewLogger(log.Options{}), nil)
			client.actions = mockActions
			got, err := client.GetReleaseLabels(tt.releaseName, tt.labelName)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestLastReleaseStatus(t *testing.T) {
	tests := []struct {
		name        string
		releaseName string
		mockResult  *action.ReleaseGetResultV1
		mockError   error
		wantRev     string
		wantStatus  string
		wantErr     bool
	}{
		{
			name:        "successful get status",
			releaseName: "test-release",
			mockResult: &action.ReleaseGetResultV1{
				Release: &action.ReleaseGetResultRelease{
					Name:      "test-release",
					Namespace: "test-namespace",
					Revision:  1,
					Status:    helmrelease.StatusDeployed,
				},
			},
			mockError:  nil,
			wantRev:    "1",
			wantStatus: string(helmrelease.StatusDeployed),
			wantErr:    false,
		},
		{
			name:        "release not found",
			releaseName: "test-release",
			mockResult:  nil,
			mockError:   &action.ReleaseNotFoundError{},
			wantRev:     "",
			wantStatus:  "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockActions := new(MockNelmActions)
			mockActions.On("ReleaseGet", mock.Anything, tt.releaseName, mock.MatchedBy(func(ns string) bool { return true }), mock.Anything).
				Return(tt.mockResult, tt.mockError)

			client := NewNelmClient(&CommonOptions{ConfigFlags: genericclioptions.ConfigFlags{Namespace: strPtr("default")}}, log.NewLogger(log.Options{}), nil)
			client.actions = mockActions
			gotRev, gotStatus, err := client.LastReleaseStatus(tt.releaseName)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantRev, gotRev)
				assert.Equal(t, tt.wantStatus, gotStatus)
			}
		})
	}
}

func TestIsReleaseExists(t *testing.T) {
	tests := []struct {
		name        string
		releaseName string
		mockResult  *action.ReleaseGetResultV1
		mockError   error
		want        bool
		wantErr     bool
	}{
		{
			name:        "release exists",
			releaseName: "test-release",
			mockResult: &action.ReleaseGetResultV1{
				Release: &action.ReleaseGetResultRelease{
					Revision: 1,
					Status:   "deployed",
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name:        "release not found",
			releaseName: "non-existent",
			mockError:   &action.ReleaseNotFoundError{},
			want:        false,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockActions := new(MockNelmActions)
			mockActions.On("ReleaseGet", mock.Anything, tt.releaseName, mock.MatchedBy(func(ns string) bool { return true }), mock.Anything).
				Return(tt.mockResult, tt.mockError)

			client := NewNelmClient(&CommonOptions{ConfigFlags: genericclioptions.ConfigFlags{Namespace: strPtr("default")}}, log.NewLogger(log.Options{}), nil)
			client.actions = mockActions
			got, err := client.IsReleaseExists(tt.releaseName)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
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

func TestUpgradeRelease(t *testing.T) {
	tests := []struct {
		name        string
		releaseName string
		chartName   string
		valuesPaths []string
		setValues   []string
		labels      map[string]string
		namespace   string
		mockError   error
		wantErr     bool
	}{
		{
			name:        "successful upgrade",
			releaseName: "test-release",
			chartName:   "test-chart",
			valuesPaths: []string{"values.yaml"},
			setValues:   []string{"key=value"},
			labels:      map[string]string{"test": "label"},
			namespace:   "test-namespace",
			mockError:   nil,
			wantErr:     false,
		},
		{
			name:        "upgrade error",
			releaseName: "test-release",
			chartName:   "test-chart",
			valuesPaths: []string{"values.yaml"},
			setValues:   []string{"key=value"},
			labels:      map[string]string{"test": "label"},
			namespace:   "test-namespace",
			mockError:   fmt.Errorf("upgrade error"),
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockActions := new(MockNelmActions)
			mockActions.On("ReleaseGet", mock.Anything, tt.releaseName, tt.namespace, mock.Anything).Return(&action.ReleaseGetResultV1{Release: &action.ReleaseGetResultRelease{Name: tt.releaseName, Namespace: tt.namespace}}, nil)
			mockActions.On("ReleasePlanInstall", mock.Anything, tt.releaseName, tt.namespace, mock.Anything).
				Return(tt.mockError)

			client := NewNelmClient(&CommonOptions{ConfigFlags: genericclioptions.ConfigFlags{Namespace: strPtr("default")}}, log.NewLogger(log.Options{}), nil)
			client.actions = mockActions
			err := client.UpgradeRelease(tt.releaseName, tt.chartName, tt.valuesPaths, tt.setValues, tt.labels, tt.namespace)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDeleteRelease(t *testing.T) {
	tests := []struct {
		name        string
		releaseName string
		mockError   error
		wantErr     bool
	}{
		{
			name:        "successful delete",
			releaseName: "test-release",
			mockError:   nil,
			wantErr:     false,
		},
		{
			name:        "release not found",
			releaseName: "non-existent",
			mockError:   &action.ReleaseNotFoundError{},
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockActions := new(MockNelmActions)
			mockActions.On("ReleaseUninstall", mock.Anything, tt.releaseName, mock.MatchedBy(func(ns string) bool { return true }), mock.Anything).
				Return(tt.mockError)

			client := NewNelmClient(&CommonOptions{ConfigFlags: genericclioptions.ConfigFlags{Namespace: strPtr("default")}}, log.NewLogger(log.Options{}), nil)
			client.actions = mockActions
			err := client.DeleteRelease(tt.releaseName)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestListReleasesNames(t *testing.T) {
	tests := []struct {
		name       string
		mockResult *action.ReleaseListResultV1
		mockError  error
		want       []string
		wantErr    bool
	}{
		{
			name: "successful list releases",
			mockResult: &action.ReleaseListResultV1{
				Releases: []*action.ReleaseListResultRelease{
					{
						Name:      "release-1",
						Namespace: "test-namespace",
					},
					{
						Name:      "release-2",
						Namespace: "test-namespace",
					},
				},
			},
			mockError: nil,
			want:      []string{"release-1", "release-2"},
			wantErr:   false,
		},
		{
			name:       "list error",
			mockResult: nil,
			mockError:  fmt.Errorf("list error"),
			want:       nil,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockActions := new(MockNelmActions)
			mockActions.On("ReleaseList", mock.Anything, mock.Anything).
				Return(tt.mockResult, tt.mockError)

			client := NewNelmClient(&CommonOptions{ConfigFlags: genericclioptions.ConfigFlags{Namespace: strPtr("default")}}, log.NewLogger(log.Options{}), nil)
			client.actions = mockActions
			got, err := client.ListReleasesNames()

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestRender(t *testing.T) {
	tests := []struct {
		name        string
		chartName   string
		namespace   string
		valuesPaths []string
		setValues   []string
		releaseName string
		showCRDs    bool
		mockResult  *action.ChartRenderResultV1
		mockError   error
		want        string
		wantErr     bool
	}{
		{
			name:        "successful render",
			chartName:   "test-chart",
			namespace:   "test-namespace",
			valuesPaths: []string{"values.yaml"},
			setValues:   []string{"key=value"},
			releaseName: "test-release",
			showCRDs:    false,
			mockResult: &action.ChartRenderResultV1{
				Resources: []map[string]interface{}{
					{"kind": "Deployment", "metadata": map[string]interface{}{"name": "test"}},
				},
			},
			mockError: nil,
			want:      "kind: Deployment\nmetadata:\n  name: test\n",
			wantErr:   false,
		},
		{
			name:        "render error",
			chartName:   "test-chart",
			namespace:   "test-namespace",
			valuesPaths: []string{"values.yaml"},
			setValues:   []string{"key=value"},
			releaseName: "test-release",
			showCRDs:    false,
			mockResult:  nil,
			mockError:   fmt.Errorf("render error"),
			want:        "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockActions := new(MockNelmActions)
			mockActions.On("ChartRender", mock.Anything, mock.Anything).
				Return(tt.mockResult, tt.mockError)

			client := NewNelmClient(&CommonOptions{ConfigFlags: genericclioptions.ConfigFlags{Namespace: strPtr("default")}}, log.NewLogger(log.Options{}), nil)
			client.actions = mockActions
			got, err := client.Render(tt.chartName, tt.namespace, tt.valuesPaths, tt.setValues, tt.releaseName, tt.showCRDs)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestGetReleaseValues(t *testing.T) {
	tests := []struct {
		name        string
		releaseName string
		mockResult  *action.ReleaseGetResultV1
		mockError   error
		want        map[string]interface{}
		wantErr     bool
	}{
		{
			name:        "successful get values",
			releaseName: "test-release",
			mockResult: &action.ReleaseGetResultV1{
				Release: &action.ReleaseGetResultRelease{
					Name:      "test-release",
					Namespace: "test-namespace",
				},
				Values: map[string]interface{}{"key": "value"},
			},
			mockError: nil,
			want:      map[string]interface{}{"key": "value"},
			wantErr:   false,
		},
		{
			name:        "release not found",
			releaseName: "test-release",
			mockResult:  nil,
			mockError:   &action.ReleaseNotFoundError{},
			want:        nil,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockActions := new(MockNelmActions)
			mockActions.On("ReleaseGet", mock.Anything, tt.releaseName, mock.MatchedBy(func(ns string) bool { return true }), mock.Anything).
				Return(tt.mockResult, tt.mockError)

			client := NewNelmClient(&CommonOptions{ConfigFlags: genericclioptions.ConfigFlags{Namespace: strPtr("default")}}, log.NewLogger(log.Options{}), nil)
			client.actions = mockActions
			got, err := client.GetReleaseValues(tt.releaseName)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, map[string]interface{}(got))
			}
		})
	}
}

func TestGetReleaseChecksum(t *testing.T) {
	tests := []struct {
		name        string
		releaseName string
		mockResult  *action.ReleaseGetResultV1
		mockError   error
		want        string
		wantErr     bool
	}{
		{
			name:        "successful get checksum",
			releaseName: "test-release",
			mockResult: &action.ReleaseGetResultV1{
				Release: &action.ReleaseGetResultRelease{
					Name:        "test-release",
					Namespace:   "test-namespace",
					Annotations: map[string]string{"moduleChecksum": "test-checksum"},
				},
			},
			mockError: nil,
			want:      "test-checksum",
			wantErr:   false,
		},
		{
			name:        "checksum not found",
			releaseName: "test-release",
			mockResult: &action.ReleaseGetResultV1{
				Release: &action.ReleaseGetResultRelease{
					Name:        "test-release",
					Namespace:   "test-namespace",
					Annotations: map[string]string{},
				},
			},
			mockError: nil,
			want:      "",
			wantErr:   true,
		},
		{
			name:        "release not found",
			releaseName: "test-release",
			mockResult:  nil,
			mockError:   &action.ReleaseNotFoundError{},
			want:        "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockActions := new(MockNelmActions)
			mockActions.On("ReleaseGet", mock.Anything, tt.releaseName, mock.MatchedBy(func(ns string) bool { return true }), mock.Anything).
				Return(tt.mockResult, tt.mockError)

			client := NewNelmClient(&CommonOptions{ConfigFlags: genericclioptions.ConfigFlags{Namespace: strPtr("default")}}, log.NewLogger(log.Options{}), nil)
			client.actions = mockActions
			got, err := client.GetReleaseChecksum(tt.releaseName)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
