// Package dify implements a streaming client for the Dify AI API,
// supporting SSE-based response parsing and optimization suggestion extraction.
package dify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	defaultTimeout = 30 * time.Second
	defaultBaseURL = "https://api.dify.ai"
	defaultPath    = "/v1/chat-messages"
)

// Dify API客户端
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	logger     *zap.Logger
}

// 创建新的Dify客户端
func NewClient(baseURL, apiKey string, timeout time.Duration, logger *zap.Logger) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if timeout == 0 {
		timeout = defaultTimeout
	}
	if logger == nil {
		logger, _ = zap.NewProduction()
	}

	// 确保baseURL末尾没有斜杠
	baseURL = strings.TrimRight(baseURL, "/")

	// 如果baseURL不包含/v1/chat-messages路径，则添加
	if !strings.HasSuffix(baseURL, defaultPath) {
		baseURL = baseURL + defaultPath
	}

	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		logger: logger,
	}
}

// 发送请求到Dify API
func (c *Client) Request(ctx context.Context, payload map[string]interface{}) (string, error) {
	// 验证URL格式
	_, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	// 序列化请求体
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request payload: %w", err)
	}

	var lastErr error
	for i := 0; i < 3; i++ {
		// 创建请求
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}

		// 设置请求头
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))

		// 发送请求
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to send request: %w", err)
			time.Sleep(2 * time.Second)
			continue
		}
		// 检查响应状态码
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
			time.Sleep(2 * time.Second)
			continue
		}

		// 解析流式响应，解析完成后立即释放资源（避免重试循环中延迟关闭）
		parser := NewStreamParser(c.logger)
		answer, parseErr := parser.Parse(resp.Body)
		resp.Body.Close()
		parser.Release()
		if parseErr != nil {
			lastErr = fmt.Errorf("failed to parse response: %w", parseErr)
			time.Sleep(2 * time.Second)
			continue
		}

		return answer, nil
	}

	return "", fmt.Errorf("请求Dify失败，重试3次后放弃: %w", lastErr)
}
