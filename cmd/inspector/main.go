package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"k8s-inspector/pkg/api/dify"
	"k8s-inspector/pkg/config"
	"k8s-inspector/pkg/service/inspector"
	"k8s-inspector/pkg/service/report"
	"k8s-inspector/pkg/types"
)

func run(ctx context.Context) error {
	// 初始化zap日志
	logger, err := zap.NewProduction()
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %v", err)
	}
	defer logger.Sync()
	sugar := logger.Sugar()

	// 加载配置
	cfg, err := config.LoadConfig()
	if err != nil {
		return errors.Wrap(err, "failed to load config")
	}
	sugar.Info("configuration loaded successfully")

	// 初始化巡检器
	ins, err := inspector.NewInspector(cfg, logger)
	if err != nil {
		return errors.Wrap(err, "failed to initialize inspector")
	}
	sugar.Info("inspector initialized successfully")

	// 执行巡检
	inspectionReport, err := ins.RunInspection(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to run inspection")
	}
	sugar.Info("inspection completed successfully")

	// 生成本地报告
	sugar.Info("开始生成本地报告")
	sugar.Infow("报告输出目录", "path", cfg.ReportOutputDir)

	// 直接使用标准输出作为logger，确保日志可见
	fmt.Println("=== HTML报告生成日志 ===")
	if err := report.GenerateReport(inspectionReport, cfg, os.Stdout); err != nil {
		sugar.Errorw("生成报告失败", "error", err)
		fmt.Printf("生成报告失败: %v\n", err)
		// 继续执行，不中断流程
	} else {
		sugar.Info("报告生成成功")
		fmt.Println("报告生成成功")
	}
	fmt.Println("=== HTML报告生成完成 ===")
	sugar.Info("生成本地报告完成")

	// 获取巡检报告摘要
	summaryReport := report.BuildNotificationMessage(inspectionReport, cfg)
	if len(summaryReport) == 0 {
		return errors.New("inspection report summary is empty")
	}

	// 添加容量相关指标，采用极简展示方式，企业微信只展示关键汇总信息
	summaryReport += "\n\n**📊 容量优化指标**\n"

	// 计算集群级别的聚合指标
	var totalMemoryUsage float64
	var totalPodCount int
	var nodeCount int
	var highMemNodes []types.NodeStatus // 内存使用率 > 80%
	var lowMemNodes []types.NodeStatus  // 内存使用率 < 30%
	var highPodNodes []types.NodeStatus // POD数量 > 50
	var lowPodNodes []types.NodeStatus  // POD数量 < 20
	var normalNodes []types.NodeStatus  // 正常节点

	for _, node := range inspectionReport.Nodes {
		if node.Status == "Ready" {
			totalMemoryUsage += node.MemoryUsage
			totalPodCount += node.PodCount
			nodeCount++

			// 分级统计节点
			if node.MemoryUsage > 80 {
				highMemNodes = append(highMemNodes, node)
			} else if node.MemoryUsage < 30 {
				lowMemNodes = append(lowMemNodes, node)
			}

			if node.PodCount > 50 {
				highPodNodes = append(highPodNodes, node)
			} else if node.PodCount < 20 {
				lowPodNodes = append(lowPodNodes, node)
			} else {
				normalNodes = append(normalNodes, node)
			}
		}
	}

	if nodeCount > 0 {
		averagePodCount := totalPodCount / nodeCount

		// 展示集群级别的聚合指标（企业微信只需要汇总信息）
		summaryReport += fmt.Sprintf("- 集群平均POD密度: %d个/节点\n", averagePodCount)
		summaryReport += fmt.Sprintf("- 高内存使用率节点 (>80%%): %d\n", len(highMemNodes))
		summaryReport += fmt.Sprintf("- 低内存使用率节点 (<30%%): %d\n", len(lowMemNodes))
		summaryReport += fmt.Sprintf("- 高POD密度节点 (>50个): %d\n", len(highPodNodes))
		summaryReport += fmt.Sprintf("- 低POD密度节点 (<20个): %d\n", len(lowPodNodes))

		// 只展示最严重的几个异常节点（最多5个），其他只显示数量
		maxDisplayNodes := 5

		// 高内存使用率节点（最严重，优先展示）
		if len(highMemNodes) > 0 {
			summaryReport += fmt.Sprintf("\n**⚠️ 高内存使用率节点** (%d个)\n", len(highMemNodes))
			displayCount := len(highMemNodes)
			if displayCount > maxDisplayNodes {
				displayCount = maxDisplayNodes
			}
			for i := 0; i < displayCount; i++ {
				node := highMemNodes[i]
				summaryReport += fmt.Sprintf("- %s: 内存使用率 %.1f%%, POD数量 %d\n",
					node.Name, node.MemoryUsage, node.PodCount)
			}
			if len(highMemNodes) > maxDisplayNodes {
				summaryReport += fmt.Sprintf("- ... 还有 %d 个节点，请查看完整报告\n", len(highMemNodes)-maxDisplayNodes)
			}
		}

		// 高POD密度节点（次严重）
		if len(highPodNodes) > 0 {
			summaryReport += fmt.Sprintf("\n**⚠️ 高POD密度节点** (%d个)\n", len(highPodNodes))
			displayCount := len(highPodNodes)
			if displayCount > maxDisplayNodes {
				displayCount = maxDisplayNodes
			}
			for i := 0; i < displayCount; i++ {
				node := highPodNodes[i]
				summaryReport += fmt.Sprintf("- %s: 内存使用率 %.1f%%, POD数量 %d\n",
					node.Name, node.MemoryUsage, node.PodCount)
			}
			if len(highPodNodes) > maxDisplayNodes {
				summaryReport += fmt.Sprintf("- ... 还有 %d 个节点，请查看完整报告\n", len(highPodNodes)-maxDisplayNodes)
			}
		}

		// 低内存使用率和低POD密度节点只显示数量，不展示详细列表
		if len(lowMemNodes) > 0 {
			summaryReport += fmt.Sprintf("\n**⚠️ 低内存使用率节点** (%d个)，请查看完整报告了解详情\n", len(lowMemNodes))
		}

		if len(lowPodNodes) > 0 {
			summaryReport += fmt.Sprintf("**⚠️ 低POD密度节点** (%d个)，请查看完整报告了解详情\n", len(lowPodNodes))
		}

		// 引导用户查看完整HTML报告获取详细信息
		summaryReport += "\n💡 完整节点详细信息请查看HTML报告\n"
	}

	// 初始化最终报告
	finalReport := summaryReport

	// 如果配置了Dify，请求获取优化建议
	if cfg.DifyBaseURL != "" && cfg.DifyAPIKey != "" {
		// 初始化 Dify 客户端
		difyClient := dify.NewClient(cfg.DifyBaseURL, cfg.DifyAPIKey, cfg.Timeout, logger)

		// 构建 Dify 请求参数
		payload := map[string]interface{}{
			"inputs":          make(map[string]interface{}),
			"query":           summaryReport,
			"response_mode":   "streaming",
			"conversation_id": "",
			"user":            "cluster-inspector",
		}

		// 请求 Dify 获取优化建议
		fullAnswer, err := difyClient.Request(ctx, payload)
		if err != nil {
			// 如果 Dify 请求失败，记录错误但不要中断流程
			sugar.Errorw("failed to request Dify, proceeding with basic report", "error", err)
		} else if len(fullAnswer) == 0 {
			sugar.Warnw("Dify returned empty content, proceeding with basic report")
		} else {
			// 合并原始报告和优化建议
			cleanedAnswer := dify.MergeOptimizationPrefix(fullAnswer)
			finalReport = fmt.Sprintf("%s\n\n%s", summaryReport, cleanedAnswer)
		}
	}

	// 处理企业微信消息发送
	if cfg.WechatWebhook != "" {
		// 企业微信消息长度限制为4096字符，需要进行处理
		wechatReport := finalReport
		if len(wechatReport) > 4000 {
			// 截断消息并添加提示
			wechatReport = wechatReport[:3900] + "\n\n...\n\n⚠️ 报告内容过长，已截断。请查看完整报告获取详细信息。"
		}
		wechatNotifier := report.NewNotifier(cfg.WechatWebhook)
		stdLogger := log.New(os.Stdout, "", log.LstdFlags)
		report.SendWecomReport(wechatNotifier.WechatNotifier(), wechatReport, stdLogger)
	} else {
		// 输出最终报告到控制台
		sugar.Info("最终报告:\n", finalReport)
	}

	return nil
}

func main() {
	// 创建根context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
