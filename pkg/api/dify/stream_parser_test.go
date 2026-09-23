package dify

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestStreamParser_Parse(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	// 动态构建指纹字符串
	fingerprintContent := "### 🤖 AI优化建议\nSuggestion"
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(fingerprintContent)))

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:    "基本消息",
			input:   `data: {"event":"message","message":{"content":"Hello"}}\ndata: {"event":"message_end","message":{"content":" World"}}\ndata: [DONE]`,
			want:    "Hello World",
			wantErr: false,
		},
		{
			name:    "错误响应",
			input:   `data: {"event":"error","error":"错误信息"}\ndata: [DONE]`,
			want:    "",
			wantErr: true,
		},
		{
			name:    "仅有DONE事件",
			input:   `data: [DONE]`,
			want:    "",
			wantErr: true, // Stream ended without any valid response
		},
		{
			name:    "无message_end但有内容",
			input:   `data: {"event":"message","message":{"content":"Partial content"}}\ndata: [DONE]`,
			want:    "Partial content",
			wantErr: false,
		},
		{
			name:    "包含think标签",
			input:   `data: {"event":"message","message":{"content":"Hello<think>thought</think>World"}}\ndata: {"event":"message_end","message":{"content":"!"}}\ndata: [DONE]`,
			want:    "HelloWorld!",
			wantErr: false,
		},
		{
			name:    "agent_message事件",
			input:   `data: {"event":"agent_message","message":{"content":"Agent says: "}}\ndata: {"event":"message","message":{"content":"Hello"}}\ndata: {"event":"message_end","message":{"content":" World"}}\ndata: [DONE]`,
			want:    "Agent says: Hello World",
			wantErr: false,
		},
		{
			name:    "message_end事件带指纹",
			input:   `data: {"event":"message","message":{"content":"Test"}}\ndata: {"event":"message_end","message":{"content":" Suggestion"},"metadata":{"fingerprint":"` + fingerprint + `"}}\ndata: [DONE]`,
			want:    "Test### 🤖 AI优化建议\nSuggestion",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewStreamParser(logger)
			defer parser.Release()

			reader := bytes.NewReader([]byte(tt.input))
			got, err := parser.Parse(reader)

			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && got != tt.want {
				t.Errorf("Parse() got = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMergeOptimizationPrefix(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "基本清理",
			content: "好的，根据您提供的巡检报告，优化建议如下：\n\n1. 优化A\n2. 优化B",
			want:    "### 🤖 AI优化建议\n1. 优化A\n2. 优化B",
		},
		{
			name:    "仅包含“好的”",
			content: "好的，这是您的报告。",
			want:    "### 🤖 AI优化建议\n这是您的报告。",
		},
		{
			name:    "无前缀",
			content: "直接的建议内容。",
			want:    "### 🤖 AI优化建议\n直接的建议内容。",
		},
		{
			name:    "包含星号列表",
			content: "好的，* 优化A\n* 优化B",
			want:    "### 🤖 AI优化建议\n* 优化A\n* 优化B",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MergeOptimizationPrefix(tt.content); got != tt.want {
				t.Errorf("MergeOptimizationPrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsValidContent(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want bool
	}{
		{"Valid content", "Hello World", true},
		{"Content with leading/trailing spaces", "  Hello World  ", true},
		{"Content with newlines", "Hello\nWorld", true},
		{"Empty string", "", false},
		{"Only spaces", "   ", false},
		{"Only punctuation", ".,!@", false}, // Removed # as it's not always punctuation
		{"Mixed punctuation and spaces", ". , ! ", false},
		{"Contains HTML tag", "<p>Hello</p>", false},
		{"Contains XML tag", "<tag>World</tag>", false},
		{"Contains partial tag", "Hello <World", false},
		{"Contains partial tag 2", "Hello World>", false},
		{"Chinese characters", "你好世界", true},
		{"Mixed Chinese and English", "Hello 你好 World", true},
		{"Long content (over 500 chars)", strings.Repeat("a", 600), true}, // Should now be true
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidContent(tt.s); got != tt.want {
				t.Errorf("isValidContent(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestExtractLastCompleteSuggestion(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "Single complete suggestion",
			content: "一些前言。\n优化建议：这是第一条建议。",
			want:    "\n优化建议：这是第一条建议",
		},
		{
			name:    "Multiple suggestions, extract last",
			content: "前言。\n优化建议：第一条。\n\n优化建议：第二条。\n\n优化建议：第三条。",
			want:    "\n优化建议：第三条",
		},
		{
			name:    "Incomplete suggestion at end",
			content: "前言。\n优化建议：第一条。\n\n优化建议：第二条不完整",
			want:    "\n优化建议：第二条不完整",
		},
		{
			name:    "No suggestion found",
			content: "只有普通文本。",
			want:    "",
		},
		{
			name:    "Suggestion with punctuation at end",
			content: "优化建议：这是一条建议！",
			want:    "优化建议：这是一条建议",
		},
		{
			name:    "Suggestion with multiple newlines",
			content: "优化建议：第一行。\n\n第二行。",
			want:    "优化建议：第一行。\n\n第二行",
		},
		{
			name:    "Suggestion starting at beginning of string",
			content: "优化建议：这是开头。",
			want:    "优化建议：这是开头",
		},
		{
			name:    "Suggestion with trailing spaces and newlines",
			content: "优化建议：内容。\n   \n",
			want:    "优化建议：内容",
		},
		{
			name:    "Suggestion with Chinese punctuation",
			content: "优化建议：这是一条建议！。",
			want:    "优化建议：这是一条建议",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractLastCompleteSuggestion(tt.content); got != tt.want {
				t.Errorf("extractLastCompleteSuggestion(%q) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}
}

func TestCleanConsecutiveNewlines(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want string
	}{
		{"No change", "Hello\nWorld", "Hello\nWorld"},
		{"Multiple newlines to two", "Hello\n\n\nWorld", "Hello\n\nWorld"},
		{"Leading/trailing newlines", "\n\nHello World\n\n", "Hello World"},
		{"Mixed spaces and newlines", "Hello\n  \n\nWorld", "Hello\n\nWorld"},
		{"Remove think tags", "Hello<think>some thought</think>World", "HelloWorld"},
		{"Remove think tags with newlines", "Hello\n<think>thought</think>\nWorld", "Hello\nWorld"},
		{"Optimization prefix with newlines", "优化建议：\n\n内容", "优化建议：\n内容"},
		{"Complex case", "  \nHello\n\n\nWorld<think>t</think>\n\n优化建议：\n\n最终内容。\n\n", "Hello\n\nWorld\n优化建议：\n最终内容。"},
		{"Only think tags", "<think>only thoughts</think>", ""},
		{"Think tags with content around", "Before<think>thought</think>After", "BeforeAfter"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanConsecutiveNewlines(tt.s); got != tt.want {
				t.Errorf("cleanConsecutiveNewlines(%q) = %q, want %q", tt.s, got, tt.want)
			}
		})
	}
}
