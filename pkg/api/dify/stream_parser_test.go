package dify

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// 说明：Parse 使用 bufio.Scanner 按真实换行逐行解析 SSE 流，
// 因此测试输入统一通过 strings.Join(..., "\n") 构建真实多行文本。

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
			name: "基本消息",
			input: strings.Join([]string{
				`data: {"event":"message","message":{"content":"Hello"}}`,
				`data: {"event":"message_end","message":{"content":" World"}}`,
				`data: [DONE]`,
			}, "\n"),
			want:    "Hello World",
			wantErr: false,
		},
		{
			name: "错误响应",
			input: strings.Join([]string{
				`data: {"event":"error","error":"错误信息"}`,
				`data: [DONE]`,
			}, "\n"),
			want:    "",
			wantErr: true,
		},
		{
			name:    "仅有DONE事件",
			input:   `data: [DONE]`,
			want:    "",
			wantErr: true, // 流结束但没有有效响应或 message_end 事件
		},
		{
			name: "无message_end但有内容",
			input: strings.Join([]string{
				`data: {"event":"message","message":{"content":"Partial content"}}`,
				`data: [DONE]`,
			}, "\n"),
			want:    "Partial content",
			wantErr: false,
		},
		{
			name: "含尖括号与纯标点的内容被过滤",
			input: strings.Join([]string{
				`data: {"event":"message","message":{"content":"Hello<think>thought</think>World"}}`,
				`data: {"event":"message_end","message":{"content":"!"}}`,
				`data: [DONE]`,
			}, "\n"),
			// isValidContent 会过滤含 <> 的内容，"!" 属于纯标点同样被过滤，最终无有效响应
			want:    "",
			wantErr: true,
		},
		{
			name: "agent_message事件",
			input: strings.Join([]string{
				`data: {"event":"agent_message","message":{"content":"Agent says: "}}`,
				`data: {"event":"message","message":{"content":"Hello"}}`,
				`data: {"event":"message_end","message":{"content":" World"}}`,
				`data: [DONE]`,
			}, "\n"),
			want:    "Agent says: Hello World",
			wantErr: false,
		},
		{
			name: "agent_message中的think标签被最终清理",
			input: strings.Join([]string{
				`data: {"event":"agent_message","message":{"content":"Think<think>t</think>End"}}`,
				`data: [DONE]`,
			}, "\n"),
			// agent_message 不做 isValidContent 过滤，<think> 标签由最终清理阶段移除
			want:    "ThinkEnd",
			wantErr: false,
		},
		{
			name: "message_end事件带指纹",
			input: strings.Join([]string{
				`data: {"event":"message","message":{"content":"Test"}}`,
				`data: {"event":"message_end","message":{"content":" Suggestion"},"metadata":{"fingerprint":"` + fingerprint + `"}}`,
				`data: [DONE]`,
			}, "\n"),
			// 实现基于 message_end 内容自身计算指纹，metadata 指纹不匹配时仅记录告警，不阻断
			want:    "Test Suggestion",
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

// TestMergeOptimizationPrefix 验证 AI 输出清理逻辑：
// 删除 Markdown 引用行（复述原始报告的过程内容），
// 压缩三个及以上连续空行；优化建议内容完整保留。
func TestMergeOptimizationPrefix(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "引用行被删除",
			content: "分析结论。\n> 引用原始报告内容\n优化建议：保留建议。",
			want:    "分析结论。\n优化建议：保留建议。",
		},
		{
			name:    "连续引用行全部删除",
			content: "前言。\n> 引用一\n> 引用二\n优化建议：建议内容。",
			want:    "前言。\n优化建议：建议内容。",
		},
		{
			name:    "三个及以上连续换行压缩为两个",
			content: "第一段。\n\n\n\n第二段。",
			want:    "第一段。\n\n第二段。",
		},
		{
			name:    "引用行删除与空行压缩组合",
			content: "前言。\n> 引用一\n> 引用二\n\n\n优化建议：最终建议。",
			want:    "前言。\n\n优化建议：最终建议。",
		},
		{
			name:    "优化建议行完整保留",
			content: "好的，根据您提供的巡检报告，优化建议如下：\n\n1. 优化A\n2. 优化B",
			want:    "好的，根据您提供的巡检报告，优化建议如下：\n\n1. 优化A\n2. 优化B",
		},
		{
			name:    "普通文本原样返回",
			content: "直接的建议内容。",
			want:    "直接的建议内容。",
		},
		{
			name:    "行内引用符号不受影响",
			content: "a > b 不是行首引用",
			want:    "a > b 不是行首引用",
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
		{"Only punctuation", ".,!@", false},
		{"Mixed punctuation and spaces", ". , ! ", false},
		{"Contains HTML tag", "<p>Hello</p>", false},
		{"Contains XML tag", "<tag>World</tag>", false},
		{"Contains partial tag", "Hello <World", false},
		{"Contains partial tag 2", "Hello World>", false},
		{"Chinese characters", "你好世界", true},
		{"Mixed Chinese and English", "Hello 你好 World", true},
		{"Long content (over 500 chars)", strings.Repeat("a", 600), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidContent(tt.s); got != tt.want {
				t.Errorf("isValidContent(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

// TestExtractLastCompleteSuggestion 验证建议段提取：
// 片段以 "\n优化建议：" 或文本开头 "优化建议：" 起始，
// 边界为句末标点（含后继空白）、连续两个换行或文本结尾；
// 末尾标点由 TrimRightFunc 统一去除。
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
			name: "Incomplete suggestion at end",
			// 前一条建议以 "\n\n" 结尾已被整体匹配，末尾片段缺少匹配起点
			content: "前言。\n优化建议：第一条。\n\n优化建议：第二条不完整",
			want:    "\n优化建议：第一条",
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
			want:    "优化建议：第一行",
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
			name:    "句末标点后跟空格构成段落边界",
			content: "优化建议：第一条。 后续内容不再属于建议",
			want:    "优化建议：第一条",
		},
		{
			name:    "句末标点后跟换行构成段落边界",
			content: "前言。\n优化建议：完整建议。\n后续分析内容。",
			want:    "\n优化建议：完整建议",
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
		{"Multiple newlines to one", "Hello\n\n\nWorld", "Hello\nWorld"},
		{"Leading/trailing newlines", "\n\nHello World\n\n", "Hello World"},
		{"Mixed spaces and newlines", "Hello\n  \n\nWorld", "Hello\nWorld"},
		{"Remove think tags", "Hello<think>some thought</think>World", "HelloWorld"},
		{"Remove think tags with newlines", "Hello\n<think>thought</think>\nWorld", "Hello\n\nWorld"},
		{"Optimization prefix with newlines", "优化建议：\n\n内容", "优化建议：内容"},
		{"Complex case", "  \nHello\n\n\nWorld<think>t</think>\n\n优化建议：\n\n最终内容。\n\n", "Hello\nWorld\n优化建议：最终内容。"},
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
