package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateUTF8(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		maxBytes int
		want     string
	}{
		{"短字符串不截断", "hello", 10, "hello"},
		{"长度恰好等于上限", "hello", 5, "hello"},
		{"按字符边界截断中文", "巡检报告内容", 10, "巡检报"},
		{"不切断多字节字符", "a你好", 3, "a"},
		{"全部为中文且超限", "你好", 4, "你"},
		{"上限为零", "abc", 0, ""},
		{"空字符串", "", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateUTF8(tt.s, tt.maxBytes)
			if got != tt.want {
				t.Errorf("truncateUTF8(%q, %d) = %q, want %q", tt.s, tt.maxBytes, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("truncateUTF8(%q, %d) 产生了非法 UTF-8 序列", tt.s, tt.maxBytes)
			}
			if len(got) > tt.maxBytes {
				t.Errorf("truncateUTF8(%q, %d) 结果长度 %d 超过上限", tt.s, tt.maxBytes, len(got))
			}
		})
	}
}

// TestWechatTruncatedMessageWithinLimit 验证实际使用方式：
// truncateUTF8 截断后追加提示语，整体不超过企业微信消息长度限制。
func TestWechatTruncatedMessageWithinLimit(t *testing.T) {
	long := strings.Repeat("巡检报告", wechatMessageLimit) // 远超限制
	truncated := truncateUTF8(long, wechatMessageLimit-len(wechatTruncatedSuffix))
	if truncated == long {
		t.Fatal("超长内容应被截断")
	}

	msg := truncated + wechatTruncatedSuffix
	if len(msg) > wechatMessageLimit {
		t.Errorf("截断后消息长度 %d 超过限制 %d", len(msg), wechatMessageLimit)
	}
	if !utf8.ValidString(msg) {
		t.Error("截断后消息不是合法 UTF-8")
	}
}
