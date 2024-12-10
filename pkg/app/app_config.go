package app

import (
	"fmt"

	"github.com/caarlos0/env/v11"
	shapp "github.com/flant/shell-operator/pkg/app"
)

type appConfig struct {
	TmpDir                  string `env:"TMP_DIR"`
	PrometheusMetricsPrefix string `env:"PROMETHEUS_METRICS_PREFIX"`
}

func newAppConfig() *appConfig {
	return &appConfig{}
}

type Config struct {
	SOp       *shapp.Config `env:"-"`
	AppConfig *appConfig    `envPrefix:"ADDON_OPERATOR_"`

	ready bool
}

func newConfig() *Config {
	return &Config{
		AppConfig: newAppConfig(),
		SOp:       shapp.MustGetConfig(),
	}
}

func (cfg *Config) Parse() error {
	if cfg.ready {
		return nil
	}

	opts := env.Options{
		Prefix: "",
	}

	err := env.ParseWithOptions(cfg, opts)
	if err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	return nil
}

func (cfg *Config) InitConfig() {
	cfg.OverrideDefaults()
	cfg.SetupGlobalVars()

	cfg.SetReady()
}

func (cfg *Config) OverrideDefaults() {
	if cfg.IsReady() {
		return
	}

	setIfNotEmpty(&shapp.TempDir, DefaultTempDir)
	setIfNotEmpty(&shapp.PrometheusMetricsPrefix, DefaultPrometheusMetricsPrefix)
	setIfNotEmpty(&shapp.DebugUnixSocket, DefaultDebugUnixSocket)
}

func (cfg *Config) SetupGlobalVars() {
	if cfg.IsReady() {
		return
	}

	cfg.SOp.SetupGlobalVars()

	setIfNotEmpty(&shapp.TempDir, cfg.AppConfig.TmpDir)
	setIfNotEmpty(&shapp.PrometheusMetricsPrefix, cfg.AppConfig.PrometheusMetricsPrefix)
}

func (cfg *Config) IsReady() bool {
	return cfg.ready
}

func (cfg *Config) SetReady() {
	cfg.SOp.SetReady()
	cfg.ready = true
}

var configInstance *Config

func MustGetConfig() *Config {
	cfg, err := GetConfig()
	if err != nil {
		panic(err)
	}

	return cfg
}

func MustGetAndInitConfig() *Config {
	cfg, err := GetAndInitConfig()
	if err != nil {
		panic(err)
	}

	return cfg
}

func GetConfig() (*Config, error) {
	if configInstance != nil {
		return configInstance, nil
	}

	cfg := newConfig()
	err := cfg.Parse()
	if err != nil {
		return nil, err
	}

	configInstance = cfg

	return configInstance, nil
}

func GetAndInitConfig() (*Config, error) {
	cfg, err := GetConfig()
	if err != nil {
		return nil, err
	}

	cfg.InitConfig()

	return cfg, nil
}

func setIfNotEmpty[T comparable](v *T, env T) {
	if !isZero(env) {
		*v = env
	}
}

func setSliceIfNotEmpty[T any](v *[]T, env []T) {
	if len(env) != 0 {
		*v = env
	}
}

func isZero[T comparable](v T) bool {
	return v == *new(T)
}
