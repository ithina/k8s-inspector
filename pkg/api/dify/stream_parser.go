package dify

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"go.uber.org/zap"
)

// 流式响应解析器
type StreamParser struct {
	logger             *zap.Logger
	mu                 sync.RWMutex
	metadata           map[string]any
	retrieverResources []any
	traceID            string
}

var parserPool = sync.Pool{
	New: func() interface{} {
		return &StreamParser{}
	},
}

// 构造函数
func NewStreamParser(logger *zap.Logger) *StreamParser {
	p := parserPool.Get().(*StreamParser)
	p.logger = logger
	p.metadata = make(map[string]any)
	p.retrieverResources = make([]any, 0)
	p.traceID = fmt.Sprintf("%d", time.Now().UnixNano())
	return p
}

// 释放解析器回到对象池
func (p *StreamParser) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.metadata = nil
	p.retrieverResources = nil
	p.logger = nil
	p.traceID = ""

	parserPool.Put(p)
}

// 安全地获取元数据
func (p *StreamParser) GetMetadata() map[string]any {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[string]any, len(p.metadata))
	for k, v := range p.metadata {
		result[k] = v
	}
	return result
}

// 安全地获取资源列表
func (p *StreamParser) GetResources() []any {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]any, len(p.retrieverResources))
	copy(result, p.retrieverResources)
	return result
}

// 安全地设置元数据
func (p *StreamParser) setMetadata(meta map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.metadata = meta
}

// 安全地添加资源
func (p *StreamParser) addResources(resources []any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.retrieverResources = append(p.retrieverResources, resources...)
}

// 处理流式响应并返回完整回答
func (p *StreamParser) Parse(body io.Reader) (string, error) {
	var (
		messageEndCount   int
		bufferFingerprint string
		fullAnswer        strings.Builder
		buffer            strings.Builder
		streamEnded       bool
		lastEventTime     time.Time
	)

	const (
		flushInterval    = 500 * time.Millisecond
		maxEventWaitTime = 10 * time.Second // 最长等待事件时间
	)

	scanner := bufio.NewScanner(body)
	var flushTimer *time.Timer

	// 启动事件超时检查
	eventCheckTimer := time.NewTicker(1 * time.Second)
	defer eventCheckTimer.Stop()

	// 创建一个channel用于同步处理
	done := make(chan struct{})
	defer close(done)

	// 启动超时检查goroutine
	go func() {
		for {
			select {
			case <-done:
				return
			case <-eventCheckTimer.C:
				if !lastEventTime.IsZero() && time.Since(lastEventTime) > maxEventWaitTime {
					p.logger.Warn("长时间未收到事件，强制结束流式处理",
						zap.Duration("超时时间", maxEventWaitTime),
						zap.Time("最后事件时间", lastEventTime))
					streamEnded = true
					return
				}
			}
		}
	}()

	flushBuffer := func(reason string) {
		content := buffer.String()
		if content == "" {
			return
		}
		merged := MergeOptimizationPrefix(content)
		if bufferFingerprint != "" && !isValidMerge(merged, bufferFingerprint) {
			p.logger.Warn("缓冲区指纹校验失败", zap.String("预期指纹", bufferFingerprint))
		}
		fullAnswer.WriteString(merged)
		p.logger.Debug("刷新缓冲区", zap.String("原因", reason), zap.Int("长度", len(merged)))
		buffer.Reset()
		bufferFingerprint = ""
	}

	for scanner.Scan() {
		if streamEnded {
			break
		}

		line := scanner.Text()
		lastEventTime = time.Now() // 更新最后事件时间

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		event := strings.TrimPrefix(line, "data: ")
		if event == "[DONE]" {
			flushBuffer("DONE事件")
			streamEnded = true
			break
		}

		var data map[string]interface{}
		if err := json.Unmarshal([]byte(event), &data); err != nil {
			p.logger.Warn("JSON解析失败", zap.String("raw_event", event), zap.Error(err))
			continue
		}

		switch data["event"] {
		case "message":
			p.logger.Debug("处理message事件", zap.Any("event_data", data))
			if answer, ok := p.getAnswerFromData(data); ok && isValidContent(answer) {
				buffer.WriteString(answer)
			}
		case "agent_message":
			if answer, ok := p.getAnswerFromData(data); ok {
				fullAnswer.WriteString(answer)
				p.logger.Info("接收到原始响应内容", zap.String("raw_answer", answer))
			}
		case "message_end":
			messageEndCount++
			if flushTimer != nil {
				flushTimer.Stop()
			}
			if answer, ok := p.getAnswerFromData(data); ok && isValidContent(answer) {
				h := sha256.New()
				h.Write([]byte(answer))
				bufferFingerprint = fmt.Sprintf("%x", h.Sum(nil))
				buffer.WriteString(answer)
			}
			if lastSuggestion := extractLastCompleteSuggestion(buffer.String()); lastSuggestion != "" {
				merged := MergeOptimizationPrefix(lastSuggestion)
				fullAnswer.WriteString(merged)
				buffer.Reset()
				bufferFingerprint = ""
			} else {
				flushBuffer("message_end事件")
			}
		case "error":
			if errMsg, ok := data["error"].(string); ok {
				p.logger.Error("Dify服务端错误", zap.String("error", errMsg))
				return fullAnswer.String(), fmt.Errorf("服务端错误: %s", errMsg)
			}
		default:
			if data["error"] != nil {
				if errMsg, ok := data["error"].(string); ok {
					p.logger.Error("Dify服务端错误", zap.String("error", errMsg))
					return fullAnswer.String(), fmt.Errorf("服务端错误: %s", errMsg)
				}
			} else {
				p.logger.Warn("未处理的事件结构", zap.Any("event_data", data))
			}
		}

		// 定时刷新缓冲区
		if buffer.Len() > 0 {
			if flushTimer != nil {
				flushTimer.Stop()
			}
			flushTimer = time.AfterFunc(flushInterval, func() {
				flushBuffer("定时触发")
			})
		}
	}

	if err := scanner.Err(); err != nil {
		p.logger.Error("扫描流式响应时出错", zap.Error(err))
		return fullAnswer.String(), fmt.Errorf("扫描流式响应时出错: %w", err)
	}

	flushBuffer("最终检查")
	finalAnswer := cleanConsecutiveNewlines(fullAnswer.String())
	finalAnswer = regexp.MustCompile(`(?s)<think>.*?</think>`).ReplaceAllString(finalAnswer, "")

	p.logger.Debug("最终合并结果",
		zap.String("raw_output", finalAnswer),
		zap.Int("length", len(finalAnswer)),
		zap.Int("message_end_count", messageEndCount),
		zap.Bool("stream_ended", streamEnded))

	// 优化后的流结束判断逻辑
	if finalAnswer == "" {
		// 如果最终答案为空，且流已结束（收到DONE或超时），或者没有message_end事件
		if streamEnded || messageEndCount == 0 {
			return "", fmt.Errorf("stream ended without any valid response or message_end events")
		}
	}

	if messageEndCount == 0 && streamEnded && finalAnswer != "" {
		p.logger.Warn("流已结束但未收到message_end事件，仍返回已收集的响应",
			zap.Int("响应长度", len(finalAnswer)))
	}

	return finalAnswer, nil
}

// 过滤AI分析过程内容
func MergeOptimizationPrefix(content string) string {
	content = optimizationRegex.ReplaceAllString(content, "")
	return newlineRegex.ReplaceAllString(content, "\n\n")
}

// 验证合并内容的完整性
func isValidMerge(content, expectedFingerprint string) bool {
	h := sha256.New()
	if _, err := h.Write([]byte(content)); err != nil {
		return false
	}
	actualFingerprint := fmt.Sprintf("%x", h.Sum(nil))
	return actualFingerprint == expectedFingerprint
}

// 判断内容有效性
func isValidContent(s string) bool {
	if matched, _ := regexp.MatchString(`^[\pP\s]+$`, s); matched {
		return false
	}
	return len(strings.TrimSpace(s)) > 0 && !strings.ContainsAny(s, "<>")
}

// 清理多余换行和think标签
func cleanConsecutiveNewlines(s string) string {
	s = regexp.MustCompile(`(\n\s*)+`).ReplaceAllString(strings.TrimSpace(s), "\n")
	s = regexp.MustCompile(`(优化建议：)\n+`).ReplaceAllString(s, "$1")
	s = regexp.MustCompile(`(?s)<think>.*?</think>`).ReplaceAllString(s, "")
	return regexp.MustCompile(`\n{3,}`).ReplaceAllString(s, "\n\n")
}

// 提取最后完整建议段落
func extractLastCompleteSuggestion(content string) string {
	r := regexp.MustCompile(`(?s)((?:\n优化建议：|^优化建议：).*?)(([.。！!]\\s*)|(\n{2,})|$)`)
	matches := r.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return ""
	}
	lastMatch := matches[len(matches)-1][0]
	return strings.TrimRightFunc(lastMatch, func(r rune) bool {
		return r == '\n' || unicode.IsPunct(r)
	})
}

// 正则变量
var (
	optimizationRegex = regexp.MustCompile(`(?:\n*>.*?\\n)|(?:\\n+|^)优化建议：[^\\n]*[\\p{Han}\\w]`)
	newlineRegex      = regexp.MustCompile(`\\n{3,}`)
)

// 支持点分隔符获取嵌套字段
func deepGet(m map[string]interface{}, path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	var current interface{} = m
	for _, part := range parts {
		if currMap, ok := current.(map[string]interface{}); ok {
			if val, exists := currMap[part]; exists {
				current = val
			} else {
				return nil, false
			}
		} else {
			return nil, false
		}
	}
	return current, true
}

// 从流式数据中提取答案
func (p *StreamParser) getAnswerFromData(data map[string]interface{}) (string, bool) {
	// 元数据提取
	if meta, ok := data["metadata"].(map[string]interface{}); ok {
		p.setMetadata(meta)
	}
	if resources, ok := data["retriever_resources"].([]interface{}); ok {
		p.addResources(resources)
	}
	p.logger.Debug("开始解析响应数据", zap.Any("full_structure", data))

	// 辅助函数：从指定字段中获取字符串类型的 answer
	var extractAnswer = func(m map[string]interface{}, field string) (string, bool) {
		if val, exists := deepGet(m, field); exists {
			if str, ok := val.(string); ok && str != "" {
				return str, true
			}
			p.logger.Debug("字段类型异常",
				zap.String("期望类型", "string"),
				zap.String("路径", field),
				zap.Any("原始值", val))
			return "", false
		}
		logLevel := zap.WarnLevel
		switch fmt.Sprintf("%v", m["event"]) {
		case "agent_thought", "message_progress":
			logLevel = zap.DebugLevel
		case "message_end":
			logLevel = zap.InfoLevel
		}
		p.logger.Log(logLevel, "字段缺失",
			zap.String("字段路径", field),
			zap.String("事件类型", fmt.Sprintf("%v", m["event"])),
			zap.Any("上下文", m))
		return "", false
	}

	// 优先从 output.map 中获取 answer
	if outputVal, ok := data["output"]; ok {
		if outputMap, ok := outputVal.(map[string]interface{}); ok {
			if answer, ok := extractAnswer(outputMap, "answer"); ok {
				return strings.TrimSpace(answer), true
			}
		} else {
			p.logger.Error("output字段类型异常",
				zap.String("期望类型", "map[string]interface{}"),
				zap.String("实际类型", fmt.Sprintf("%T", outputVal)),
				zap.Any("原始值", outputVal))
			return "", false
		}
	}

	// 对message.content字段的解析
	if messageVal, ok := data["message"].(map[string]interface{}); ok {
		if contentVal, ok := messageVal["content"].(map[string]interface{}); ok {
			if answer, ok := extractAnswer(contentVal, "text"); ok {
				return answer, true
			}
		}
		if answer, ok := extractAnswer(messageVal, "content"); ok {
			return answer, true
		}
		if answer, ok := extractAnswer(messageVal, "answer"); ok {
			return answer, true
		}
	}

	// 多路径搜索策略
	searchPaths := []string{
		"content.text",
		"message.content",
		"output.text",
		"answer",
	}
	if eventType, ok := data["event"]; ok {
		switch eventType {
		case "agent_message":
			searchPaths = append([]string{"message.content.text", "content"}, searchPaths...)
		case "message_end":
			searchPaths = append([]string{"metadata.final_answer", "output.final"}, searchPaths...)
		}
	}
	for _, field := range searchPaths {
		if answer, ok := extractAnswer(data, field); ok {
			return answer, true
		}
	}
	return "", false
}
