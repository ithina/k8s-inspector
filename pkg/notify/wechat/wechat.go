// Package wechat implements an enterprise WeChat (企业微信) notifier
// that sends Markdown-formatted inspection reports via webhook.
package wechat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 企业微信机器人发送器
type Notifier struct {
	Webhook string
	Client  *http.Client
}

// 创建 Notifier 实例，支持自定义 http.Client
func NewNotifier(webhook string, client ...*http.Client) *Notifier {
	c := &http.Client{Timeout: 10 * time.Second}
	if len(client) > 0 && client[0] != nil {
		c = client[0]
	}
	return &Notifier{
		Webhook: webhook,
		Client:  c,
	}
}

// 发送 Markdown 格式的报告到企业微信
func (n *Notifier) SendMarkdownReport(content string) error {
	if n.Webhook == "" {
		return errors.New("企业微信Webhook未配置")
	}
	if len(content) > 4096 {
		return fmt.Errorf("消息长度超过企业微信限制(当前%d字节)", len(content))
	}
	payload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": content,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("请求体编码失败: %w", err)
	}

	var lastErr error
	for i := 0; i < 3; i++ {
		resp, err := n.Client.Post(n.Webhook, "application/json", bytes.NewBuffer(body))
		if err != nil {
			lastErr = fmt.Errorf("发送请求失败: %w", err)
			time.Sleep(2 * time.Second)
			continue
		}
		// 读取响应后立即关闭，避免重试循环中延迟释放连接
		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("读取响应失败: %w", readErr)
			time.Sleep(2 * time.Second)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("API返回错误: 状态码=%d 响应=%s", resp.StatusCode, string(respBody))
			time.Sleep(2 * time.Second)
			continue
		}
		if !strings.Contains(string(respBody), `"errcode":0`) {
			lastErr = fmt.Errorf("企业微信返回异常: %s", string(respBody))
			time.Sleep(2 * time.Second)
			continue
		}
		return nil
	}
	return fmt.Errorf("发送企业微信通知失败，重试3次后放弃: %w", lastErr)
}

// 构建企业微信消息模板，支持 findings 渲染
func BuildMessage(reportTitle, generatedAt, duration string, findings map[string][]string) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("### %s\n", reportTitle))
	builder.WriteString(fmt.Sprintf("**巡检时间**: %s\n", generatedAt))
	builder.WriteString(fmt.Sprintf("**执行耗时**: %s\n\n", duration))
	for k, v := range findings {
		if len(v) == 0 {
			continue
		}
		builder.WriteString(fmt.Sprintf("**%s**:\n", k))
		for _, item := range v {
			builder.WriteString(fmt.Sprintf("- %s\n", item))
		}
		builder.WriteString("\n")
	}
	return builder.String()
}
