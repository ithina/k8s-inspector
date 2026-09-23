// Package config manages environment-based configuration with validation and defaults.
//
// Configuration is loaded from environment variables, validated against
// business rules (threshold ranges, concurrency limits), and populated
// with sensible defaults for optional fields.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/pkg/errors"
)

const (
	// 环境变量名称
	EnvClusterName       = "CLUSTER_NAME"
	EnvReportClusterName = "REPORT_CLUSTER_NAME"
	EnvWechatWebhook     = "WECHAT_WEBHOOK"
	EnvReportOutputDir   = "REPORT_OUTPUT_DIR"
	EnvDifyBaseURL       = "DIFY_BASE_URL"
	EnvDifyAPIKey        = "DIFY_API_KEY"
	EnvDifyTimeout       = "DIFY_TIMEOUT"
	EnvCriticalThreshold = "CRITICAL_THRESHOLD"
	EnvWarningThreshold  = "WARNING_THRESHOLD"
	EnvMaxConcurrency    = "MAX_CONCURRENCY"

	// 默认值
	DefaultTimeout           = 60 * time.Second
	DefaultReportOutputDir   = "/app/reports"
	DefaultCriticalThreshold = 90
	DefaultWarningThreshold  = 80
	DefaultMaxConcurrency    = 10

	// 限制值
	MinThreshold   = 0
	MaxThreshold   = 100
	MinConcurrency = 1
	MaxConcurrency = 50
	MinTimeout     = 10 * time.Second
	MaxTimeout     = 300 * time.Second
)

// 配置相关
type Config struct {
	ClusterName         string        `json:"clusterName"`
	ReportClusterName   string        `json:"reportClusterName"`
	WechatWebhook       string        `json:"wechatWebhook"`
	CriticalThreshold   int           `json:"criticalThreshold"`
	WarningThreshold    int           `json:"warningThreshold"`
	InspectionNamespace string        `json:"inspectionNamespace"`
	MaxConcurrency      int           `json:"maxConcurrency"`
	ReportOutputDir     string        `json:"reportOutputDir"`
	DifyBaseURL         string        `json:"difyBaseUrl"`
	DifyAPIKey          string        `json:"difyApiKey"`
	Timeout             time.Duration `json:"timeout"`
}

// 验证配置的必要字段
func (c *Config) Validate() error {
	// 验证必填字段
	if c.ClusterName == "" {
		return errors.New("clusterName is required")
	}

	// 验证Dify配置
	if c.DifyBaseURL != "" {
		if _, err := url.Parse(c.DifyBaseURL); err != nil {
			return errors.Wrap(err, "invalid difyBaseURL")
		}
		if c.DifyAPIKey == "" {
			return errors.New("difyAPIKey is required when difyBaseURL is provided")
		}
	}

	// 验证企业微信配置
	if c.WechatWebhook != "" {
		if _, err := url.Parse(c.WechatWebhook); err != nil {
			return errors.Wrap(err, "invalid wechatWebhook URL")
		}
	}

	// 验证阈值范围
	if c.CriticalThreshold < MinThreshold || c.CriticalThreshold > MaxThreshold {
		return fmt.Errorf("criticalThreshold must be between %d and %d", MinThreshold, MaxThreshold)
	}
	if c.WarningThreshold < MinThreshold || c.WarningThreshold > MaxThreshold {
		return fmt.Errorf("warningThreshold must be between %d and %d", MinThreshold, MaxThreshold)
	}
	if c.WarningThreshold >= c.CriticalThreshold {
		return errors.New("warningThreshold must be less than criticalThreshold")
	}

	// 验证并发数
	if c.MaxConcurrency < MinConcurrency || c.MaxConcurrency > MaxConcurrency {
		return fmt.Errorf("maxConcurrency must be between %d and %d", MinConcurrency, MaxConcurrency)
	}

	// 验证超时时间
	if c.Timeout < MinTimeout || c.Timeout > MaxTimeout {
		return fmt.Errorf("timeout must be between %v and %v", MinTimeout, MaxTimeout)
	}

	return nil
}

// 设置默认值
func (c *Config) setDefaults() {
	if c.ReportClusterName == "" {
		c.ReportClusterName = c.ClusterName
	}
	if c.ReportOutputDir == "" {
		c.ReportOutputDir = DefaultReportOutputDir
	}
	if c.CriticalThreshold == 0 {
		c.CriticalThreshold = DefaultCriticalThreshold
	}
	if c.WarningThreshold == 0 {
		c.WarningThreshold = DefaultWarningThreshold
	}
	if c.MaxConcurrency == 0 {
		c.MaxConcurrency = DefaultMaxConcurrency
	}
	if c.Timeout == 0 {
		c.Timeout = DefaultTimeout
	}
}

// 从环境变量获取时间duration，带错误处理
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// 从环境变量获取整数，带错误处理
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// 从环境变量加载配置
func LoadConfig() (*Config, error) {
	cfg := &Config{
		ClusterName:       os.Getenv(EnvClusterName),
		ReportClusterName: os.Getenv(EnvReportClusterName),
		WechatWebhook:     os.Getenv(EnvWechatWebhook),
		ReportOutputDir:   os.Getenv(EnvReportOutputDir),
		DifyBaseURL:       os.Getenv(EnvDifyBaseURL),
		DifyAPIKey:        os.Getenv(EnvDifyAPIKey),
		Timeout:           getEnvDuration(EnvDifyTimeout, DefaultTimeout),
		CriticalThreshold: getEnvInt(EnvCriticalThreshold, DefaultCriticalThreshold),
		WarningThreshold:  getEnvInt(EnvWarningThreshold, DefaultWarningThreshold),
		MaxConcurrency:    getEnvInt(EnvMaxConcurrency, DefaultMaxConcurrency),
	}

	// 设置默认值
	cfg.setDefaults()

	// 验证配置
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("配置验证失败: %w", err)
	}

	return cfg, nil
}
