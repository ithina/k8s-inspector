// Package main 是 k8s-inspector 的入口。
//
// 支持两种运行模式：
//   - 单次执行（--once，默认用于 Kubernetes CronJob）
//   - 按固定间隔循环巡检（--interval，默认 1 小时）
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/ithina/k8s-inspector/pkg/api/dify"
	"github.com/ithina/k8s-inspector/pkg/config"
	"github.com/ithina/k8s-inspector/pkg/service/inspector"
	"github.com/ithina/k8s-inspector/pkg/service/report"
)

// version 由构建流程通过 -ldflags "-X main.version=..." 注入
var version = "dev"

const (
	// inspectionTimeout 单次巡检整体超时
	inspectionTimeout = 30 * time.Minute
	// wechatMessageLimit 企业微信 markdown 消息长度上限（字节）
	wechatMessageLimit = 4000
	// wechatTruncatedSuffix 消息截断时追加的提示
	wechatTruncatedSuffix = "\n\n...\n\n⚠️ 报告内容过长，已截断。请查看完整报告获取详细信息。"
)

func main() {
	var (
		once        = flag.Bool("once", false, "执行单次巡检后退出（适用于 Kubernetes CronJob）")
		interval    = flag.Duration("interval", time.Hour, "循环巡检间隔（未指定 --once 时生效）")
		showVersion = flag.Bool("version", false, "打印版本信息并退出")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("k8s-inspector %s\n", version)
		return
	}
	if !*once && *interval <= 0 {
		fmt.Fprintln(os.Stderr, "Error: --interval 必须大于 0")
		os.Exit(1)
	}

	// 监听 SIGINT/SIGTERM，支持优雅退出（容器滚动更新等场景）
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: 初始化日志失败: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	sugar := logger.Sugar()

	// 加载配置
	cfg, err := config.LoadConfig()
	if err != nil {
		sugar.Errorf("加载配置失败: %v", err)
		os.Exit(1)
	}
	sugar.Info("configuration loaded successfully")

	// 初始化巡检器（复用同一个实例，循环模式下每轮巡检重新采集数据）
	ins, err := inspector.NewInspector(cfg, logger)
	if err != nil {
		sugar.Errorf("初始化巡检器失败: %v", err)
		os.Exit(1)
	}
	sugar.Info("inspector initialized successfully")

	for {
		if err := run(ctx, cfg, ins, logger); err != nil {
			sugar.Errorf("巡检执行失败: %v", err)
			if *once {
				os.Exit(1)
			}
		}
		if *once {
			return
		}
		sugar.Infof("下次巡检将在 %v 后执行", *interval)
		select {
		case <-ctx.Done():
			sugar.Info("收到退出信号，停止巡检")
			return
		case <-time.After(*interval):
		}
	}
}

// run 执行一轮完整巡检：采集数据 -> 生成 HTML 报告 -> 构建通知摘要 -> AI 优化建议 -> 企业微信推送。
// 可选模块（Dify/企业微信）未配置或调用失败时降级运行，不中断主流程。
func run(ctx context.Context, cfg *config.Config, ins *inspector.Inspector, logger *zap.Logger) error {
	sugar := logger.Sugar()

	runCtx, cancel := context.WithTimeout(ctx, inspectionTimeout)
	defer cancel()

	// 执行巡检
	inspectionReport, err := ins.RunInspection(runCtx)
	if err != nil {
		return errors.Wrap(err, "failed to run inspection")
	}
	sugar.Info("inspection completed successfully")

	// 生成本地 HTML 报告（失败不中断后续通知流程）
	if err := report.GenerateReport(inspectionReport, cfg, os.Stdout); err != nil {
		sugar.Errorw("生成HTML报告失败", "error", err)
	}

	// 构建通知摘要
	summaryReport := report.BuildNotificationMessage(inspectionReport, cfg)
	if len(summaryReport) == 0 {
		return errors.New("inspection report summary is empty")
	}
	summaryReport += report.BuildCapacitySummary(inspectionReport)

	// 可选：请求 Dify 生成 AI 优化建议（失败时降级为基础报告）
	finalReport := summaryReport
	if cfg.DifyBaseURL != "" && cfg.DifyAPIKey != "" {
		difyClient := dify.NewClient(cfg.DifyBaseURL, cfg.DifyAPIKey, cfg.Timeout, logger)
		payload := map[string]interface{}{
			"inputs":          make(map[string]interface{}),
			"query":           summaryReport,
			"response_mode":   "streaming",
			"conversation_id": "",
			"user":            "cluster-inspector",
		}

		fullAnswer, err := difyClient.Request(runCtx, payload)
		if err != nil {
			sugar.Errorw("failed to request Dify, proceeding with basic report", "error", err)
		} else if len(fullAnswer) == 0 {
			sugar.Warnw("Dify returned empty content, proceeding with basic report")
		} else {
			// 合并原始报告和优化建议
			finalReport = fmt.Sprintf("%s\n\n%s", summaryReport, dify.MergeOptimizationPrefix(fullAnswer))
		}
	}

	// 可选：企业微信推送（未配置时仅输出到日志）
	if cfg.WechatWebhook != "" {
		wechatReport := truncateUTF8(finalReport, wechatMessageLimit-len(wechatTruncatedSuffix))
		if wechatReport != finalReport {
			wechatReport += wechatTruncatedSuffix
		}
		wechatNotifier := report.NewNotifier(cfg.WechatWebhook)
		stdLogger := log.New(os.Stdout, "", log.LstdFlags)
		report.SendWecomReport(wechatNotifier.WechatNotifier(), wechatReport, stdLogger)
	} else {
		sugar.Infof("最终报告:\n%s", finalReport)
	}

	return nil
}

// truncateUTF8 按 UTF-8 字符边界截断字符串，最多保留 maxBytes 字节，
// 避免按字节截断导致中文等多字节字符出现乱码。
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	end := 0
	for end < len(s) {
		_, size := utf8.DecodeRuneInString(s[end:])
		if end+size > maxBytes {
			break
		}
		end += size
	}
	return s[:end]
}
