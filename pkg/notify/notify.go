// Package notify provides notification abstractions for inspection reports.
package notify

import (
	"github.com/ithina/k8s-inspector/pkg/notify/wechat"
)

// 创建企业微信通知器实例
func NewWechatNotifier(webhook string) *wechat.Notifier {
	return wechat.NewNotifier(webhook)
}

// 构建企业微信通知消息，支持巡检结果渲染
func FormatWechatMessage(reportTitle, generatedAt, duration string, findings map[string][]string) string {
	return wechat.BuildMessage(reportTitle, generatedAt, duration, findings)
}
