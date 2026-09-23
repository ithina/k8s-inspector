package dify

import (
	"regexp"
	"time"
)

const (
	// 时间相关常量
	DefaultFlushInterval    = 500 * time.Millisecond
	DefaultMaxEventWaitTime = 10 * time.Second
	// 缓冲区相关常量，大缓冲区大小（1MB）
	MaxBufferSize = 1024 * 1024
)

// 预编译的正则表达式
var (
	OptimizationRegex       = regexp.MustCompile(`(?:\n*>.*?\n)|(?:\n+|^)优化建议：[^\n]*[\p{Han}\w]`)
	NewlineRegex            = regexp.MustCompile(`\n{3,}`)
	ThinkTagRegex           = regexp.MustCompile(`(?s)<think>.*?</think>`)
	ConsecutiveNewlineRegex = regexp.MustCompile(`(\n\s*)+`)
	SuggestionRegex         = regexp.MustCompile(`(?s)((?:\n优化建议：|^优化建议：).*?)(([.。！!]\s*)|(\n{2,})|$)`)
)

// 默认搜索路径
var DefaultSearchPaths = []string{
	"content.text",
	"message.content",
	"output.text",
	"answer",
}

// Agent消息搜索路径
var AgentMessageSearchPaths = []string{
	"message.content.text",
	"content",
}

// 消息结束搜索路径
var MessageEndSearchPaths = []string{
	"metadata.final_answer",
	"output.final",
}
