package config

import (
	"testing"
	"time"
)

// clearEnv 清理影响配置加载的环境变量，保证测试不受宿主环境影响
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		EnvClusterName, EnvReportClusterName, EnvWechatWebhook,
		EnvReportOutputDir, EnvReportBaseURL, EnvDifyBaseURL, EnvDifyAPIKey,
		EnvDifyTimeout, EnvCriticalThreshold, EnvWarningThreshold,
		EnvMaxConcurrency, EnvPrometheusURL, EnvExcludeNamespaces,
	} {
		t.Setenv(key, "")
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvClusterName, "test-cluster")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.ReportClusterName != "test-cluster" {
		t.Errorf("ReportClusterName = %q, want 同 ClusterName", cfg.ReportClusterName)
	}
	if cfg.CriticalThreshold != DefaultCriticalThreshold {
		t.Errorf("CriticalThreshold = %d, want %d", cfg.CriticalThreshold, DefaultCriticalThreshold)
	}
	if cfg.WarningThreshold != DefaultWarningThreshold {
		t.Errorf("WarningThreshold = %d, want %d", cfg.WarningThreshold, DefaultWarningThreshold)
	}
	if cfg.MaxConcurrency != DefaultMaxConcurrency {
		t.Errorf("MaxConcurrency = %d, want %d", cfg.MaxConcurrency, DefaultMaxConcurrency)
	}
	if cfg.ReportOutputDir != DefaultReportOutputDir {
		t.Errorf("ReportOutputDir = %q, want %q", cfg.ReportOutputDir, DefaultReportOutputDir)
	}
	if cfg.Timeout != DefaultTimeout {
		t.Errorf("Timeout = %v, want %v", cfg.Timeout, DefaultTimeout)
	}
	if len(cfg.ExcludeNamespaces) != 0 {
		t.Errorf("ExcludeNamespaces = %v, want 空", cfg.ExcludeNamespaces)
	}
}

func TestLoadConfig_MissingClusterName(t *testing.T) {
	clearEnv(t)
	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() 期望返回错误（缺少 CLUSTER_NAME），实际为 nil")
	}
}

func TestLoadConfig_ExcludeNamespaces(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvClusterName, "test-cluster")
	t.Setenv(EnvExcludeNamespaces, " kube-public, ,csi-cephfs ")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	want := []string{"kube-public", "csi-cephfs"}
	if len(cfg.ExcludeNamespaces) != len(want) {
		t.Fatalf("ExcludeNamespaces = %v, want %v", cfg.ExcludeNamespaces, want)
	}
	for i := range want {
		if cfg.ExcludeNamespaces[i] != want[i] {
			t.Errorf("ExcludeNamespaces[%d] = %q, want %q", i, cfg.ExcludeNamespaces[i], want[i])
		}
	}
}

func TestLoadConfig_InvalidDurationFallsBackToDefault(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvClusterName, "test-cluster")
	t.Setenv(EnvDifyTimeout, "not-a-duration")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Timeout != DefaultTimeout {
		t.Errorf("Timeout = %v, want 默认值 %v", cfg.Timeout, DefaultTimeout)
	}
}

func TestValidate(t *testing.T) {
	base := func() *Config {
		return &Config{
			ClusterName:       "test-cluster",
			CriticalThreshold: 90,
			WarningThreshold:  80,
			MaxConcurrency:    10,
			Timeout:           60 * time.Second,
		}
	}

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{name: "合法配置", mutate: func(c *Config) {}, wantErr: false},
		{name: "缺少集群名", mutate: func(c *Config) { c.ClusterName = "" }, wantErr: true},
		{name: "警告阈值不小于严重阈值", mutate: func(c *Config) { c.WarningThreshold = 95 }, wantErr: true},
		{name: "严重阈值超出范围", mutate: func(c *Config) { c.CriticalThreshold = 101 }, wantErr: true},
		{name: "并发数超上限", mutate: func(c *Config) { c.MaxConcurrency = 51 }, wantErr: true},
		{name: "超时过短", mutate: func(c *Config) { c.Timeout = time.Second }, wantErr: true},
		{
			name: "Dify 缺少 API Key",
			mutate: func(c *Config) {
				c.DifyBaseURL = "http://dify.example.com"
			},
			wantErr: true,
		},
		{
			name: "Dify 配置完整",
			mutate: func(c *Config) {
				c.DifyBaseURL = "http://dify.example.com"
				c.DifyAPIKey = "app-key"
			},
			wantErr: false,
		},
		{
			name:    "Prometheus 地址非法",
			mutate:  func(c *Config) { c.PrometheusURL = "://bad-url" },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base()
			tt.mutate(cfg)
			if err := cfg.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
