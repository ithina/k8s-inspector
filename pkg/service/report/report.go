// Package report generates HTML inspection reports and builds
// enterprise WeChat notification messages from inspection data.
package report

import (
	"fmt"
	"html/template"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ithina/k8s-inspector/pkg/config"
	"github.com/ithina/k8s-inspector/pkg/notify/wechat"
	"github.com/ithina/k8s-inspector/pkg/types"
	"github.com/ithina/k8s-inspector/templates"
)

// 模板函数映射
var templateFuncs = template.FuncMap{
	"split":     strings.Split,
	"contains":  strings.Contains,
	"add":       func(a, b, c, d int) int { return a + b + c + d },
	"toFloat64": func(i int) float64 { return float64(i) },
	"getConditionChinese": func(t string) string {
		mapping := map[string]string{
			"PIDPressure": "进程号压力",
			"Ready":       "节点就绪状态",
		}
		return mapping[t]
	},
	"statusToChinese": func(s string) string {
		return map[string]string{
			"True":  "异常",
			"False": "正常",
		}[s]
	},
	"reasonToChinese": func(r string) string {
		mapping := map[string]string{
			"KubeletHasSufficientPID": "进程号资源充足",
			"NodeStatusUnknown":       "节点状态未知（建议检查网络连接）",
			"KubeletNotReady":         "Kubelet服务异常",
		}
		if chs, ok := mapping[r]; ok {
			return chs
		}
		return r
	},
	"memoryAlert": func(usage float64, warn, crit int) string {
		if usage > float64(crit) {
			return "critical|🚨 内存使用率已超过严重阈值，建议:\n1. 通过 kubectl get pod <Pod名> -o yaml 检查Pod内存资源配置\n2. 分析top内存进程\n3. 考虑节点扩容"
		} else if usage > float64(warn) {
			return "warning|⚠️ 内存使用率已超过警告阈值，建议:\n1. 通过 kubectl get pod <Pod名> -o yaml 检查Pod内存资源配置\n2. 分析top内存进程 \n3. 手动执行 kubectl drain <超负荷节点> 迁移Pod至空闲节点"
		}
		return ""
	},
}

// 常量定义
const (
	DefaultReportFormat    = "html"
	ReportFileNameTemplate = "%s-%s.html"
)

// 容量优化指标阈值（内存使用率百分比 / 节点 Pod 数量），可按容量规划调整
const (
	capacityHighMemoryThreshold = 80 // 高内存节点：内存使用率高于该值（%）
	capacityLowMemoryThreshold  = 30 // 低内存节点：内存使用率低于该值（%）
	capacityHighPodThreshold    = 50 // 高密度节点：Pod 数量高于该值
	capacityLowPodThreshold     = 20 // 低密度节点：Pod 数量低于该值
	capacityMaxDisplayNodes     = 5  // 通知消息中每类最多展示的节点数
)

// 报告通知器
type Notifier struct {
	wechatNotifier *wechat.Notifier
}

type reportTemplateData struct {
	Report *types.InspectionReport
	Config *config.Config
}

// 创建通知器实例
func NewNotifier(webhook string) *Notifier {
	return &Notifier{
		wechatNotifier: wechat.NewNotifier(webhook),
	}
}

// 阈值归一化
func normalizeThresholds(config *config.Config) {
	if config.CriticalThreshold <= 0 || config.CriticalThreshold > 100 {
		config.CriticalThreshold = 90
	}
	if config.WarningThreshold <= 0 || config.WarningThreshold >= config.CriticalThreshold {
		config.WarningThreshold = 80
	}
}

// 生成巡检报告
//
// HTML 模板通过 embed 内置于二进制，渲染结果写入 config.ReportOutputDir，
// 完整路径通过 logger 输出。
func GenerateReport(report *types.InspectionReport, config *config.Config, logger io.Writer) error {
	normalizeThresholds(config)

	now := time.Now()
	report.Metadata.GeneratedAt = now.Format("2006-01-02 15:04:05")
	report.Metadata.ReportFile = fmt.Sprintf(
		ReportFileNameTemplate,
		strings.ToLower(strings.ReplaceAll(config.ClusterName, " ", "-")),
		now.Format("20060102-150405"),
	)

	reportOutputDir, err := filepath.Abs(config.ReportOutputDir)
	if err != nil {
		return fmt.Errorf("无法解析报告输出目录: %w", err)
	}
	reportPath := filepath.Join(reportOutputDir, report.Metadata.ReportFile)

	tmpl, err := template.New("report.html").Funcs(templateFuncs).ParseFS(templates.FS, "report.html")
	if err != nil {
		return fmt.Errorf("解析内置报告模板失败: %w", err)
	}

	if err := os.MkdirAll(reportOutputDir, 0755); err != nil {
		return fmt.Errorf("创建报告目录失败: %w", err)
	}

	file, err := os.Create(reportPath)
	if err != nil {
		return fmt.Errorf("创建报告文件失败: %w", err)
	}
	defer file.Close()

	data := reportTemplateData{Report: report, Config: config}
	if err := tmpl.ExecuteTemplate(file, "report.html", data); err != nil {
		return fmt.Errorf("模板渲染失败: %w", err)
	}

	if logger != nil {
		fmt.Fprintf(logger, "HTML 报告已生成: %s\n", reportPath)
	}
	return nil
}

// 统计健康组件数量
func countHealthyComponents(components []types.ComponentStatus) int {
	count := 0
	for _, c := range components {
		if c.Healthy == c.Total {
			count++
		}
	}
	return count
}

// BuildCapacitySummary 构建集群容量优化指标摘要（企业微信 Markdown 片段）。
// 仅统计 Ready 节点；没有可用于统计的 Ready 节点时返回空字符串。
func BuildCapacitySummary(report *types.InspectionReport) string {
	var (
		totalMemoryUsage float64
		totalPodCount    int
		nodeCount        int
		highMemNodes     []types.NodeStatus
		lowMemNodes      []types.NodeStatus
		highPodNodes     []types.NodeStatus
		lowPodNodes      []types.NodeStatus
	)

	for _, node := range report.Nodes {
		if node.Status != "Ready" {
			continue
		}
		totalMemoryUsage += node.MemoryUsage
		totalPodCount += node.PodCount
		nodeCount++

		switch {
		case node.MemoryUsage > capacityHighMemoryThreshold:
			highMemNodes = append(highMemNodes, node)
		case node.MemoryUsage < capacityLowMemoryThreshold:
			lowMemNodes = append(lowMemNodes, node)
		}

		switch {
		case node.PodCount > capacityHighPodThreshold:
			highPodNodes = append(highPodNodes, node)
		case node.PodCount < capacityLowPodThreshold:
			lowPodNodes = append(lowPodNodes, node)
		}
	}

	if nodeCount == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("\n\n**📊 容量优化指标**\n")
	builder.WriteString(fmt.Sprintf("- 集群平均内存使用率: %.1f%%\n", totalMemoryUsage/float64(nodeCount)))
	builder.WriteString(fmt.Sprintf("- 集群平均POD密度: %d个/节点\n", totalPodCount/nodeCount))
	builder.WriteString(fmt.Sprintf("- 高内存使用率节点 (>%d%%): %d\n", capacityHighMemoryThreshold, len(highMemNodes)))
	builder.WriteString(fmt.Sprintf("- 低内存使用率节点 (<%d%%): %d\n", capacityLowMemoryThreshold, len(lowMemNodes)))
	builder.WriteString(fmt.Sprintf("- 高POD密度节点 (>%d个): %d\n", capacityHighPodThreshold, len(highPodNodes)))
	builder.WriteString(fmt.Sprintf("- 低POD密度节点 (<%d个): %d\n", capacityLowPodThreshold, len(lowPodNodes)))

	appendNodeList := func(title string, nodes []types.NodeStatus) {
		if len(nodes) == 0 {
			return
		}
		builder.WriteString(fmt.Sprintf("\n**⚠️ %s** (%d个)\n", title, len(nodes)))
		displayCount := len(nodes)
		if displayCount > capacityMaxDisplayNodes {
			displayCount = capacityMaxDisplayNodes
		}
		for i := 0; i < displayCount; i++ {
			builder.WriteString(fmt.Sprintf("- %s: 内存使用率 %.1f%%, POD数量 %d\n",
				nodes[i].Name, nodes[i].MemoryUsage, nodes[i].PodCount))
		}
		if len(nodes) > capacityMaxDisplayNodes {
			builder.WriteString(fmt.Sprintf("- ... 还有 %d 个节点，请查看完整报告\n", len(nodes)-capacityMaxDisplayNodes))
		}
	}

	// 高内存、高密度节点优先展示明细；低负载节点只提示数量
	appendNodeList("高内存使用率节点", highMemNodes)
	appendNodeList("高POD密度节点", highPodNodes)
	if len(lowMemNodes) > 0 {
		builder.WriteString(fmt.Sprintf("\n**⚠️ 低内存使用率节点** (%d个)，请查看完整报告了解详情\n", len(lowMemNodes)))
	}
	if len(lowPodNodes) > 0 {
		builder.WriteString(fmt.Sprintf("**⚠️ 低POD密度节点** (%d个)，请查看完整报告了解详情\n", len(lowPodNodes)))
	}

	builder.WriteString("\n💡 完整节点详细信息请查看HTML报告\n")
	return builder.String()
}

// 构建企业微信通知消息
func BuildNotificationMessage(report *types.InspectionReport, config *config.Config) string {
	var builder strings.Builder

	// 标题和基本信息
	builder.WriteString(fmt.Sprintf("### %s巡检报告\n", report.Metadata.ClusterName))
	builder.WriteString(fmt.Sprintf("**巡检时间**: %s\n", report.Metadata.GeneratedAt))
	builder.WriteString(fmt.Sprintf("**执行耗时**: %s\n\n", report.Metadata.Duration))

	// 关键问题摘要
	var warnNodes []string
	for _, node := range report.Nodes {
		if node.MemoryUsage > float64(config.WarningThreshold) || node.CPUUsage > float64(config.WarningThreshold) || node.DiskUsage > float64(config.WarningThreshold) {
			warnNodes = append(warnNodes, fmt.Sprintf("- %s: CPU(%.1f%%), 内存(%.1f%%), 磁盘(%.1f%%)",
				node.Name, node.CPUUsage, node.MemoryUsage, node.DiskUsage))
		}
	}
	if len(warnNodes) > 0 {
		builder.WriteString("**⚠️ 节点警告**\n")
		for _, line := range warnNodes {
			builder.WriteString(line + "\n")
		}
		builder.WriteString("\n")
	}

	// 核心组件异常
	if len(report.Findings.CriticalComponents) > 0 {
		builder.WriteString("**❗ 核心组件异常**\n")
		for _, comp := range report.Findings.CriticalComponents {
			builder.WriteString(fmt.Sprintf("- %s\n", comp))
		}
		builder.WriteString("\n")
	}

	// 统计持续重启Pod（>5次）
	var highRestartPods []types.PodStatus
	for _, pod := range report.Pods.AbnormalPodList {
		if pod.RestartCount > 5 {
			highRestartPods = append(highRestartPods, pod)
		}
	}
	if len(highRestartPods) > 0 {
		builder.WriteString(fmt.Sprintf("**🔄 持续重启Pod（>5次）: %d**\n", len(highRestartPods)))
		for _, pod := range highRestartPods {
			builder.WriteString(fmt.Sprintf("- %s/%s: %s (重启%d次, 运行%s)\n",
				pod.Namespace, pod.Name, pod.Status, pod.RestartCount, pod.Age))
		}
		builder.WriteString("\n")
	}

	// 统计信息
	builder.WriteString("**📊 集群概览**\n")
	builder.WriteString(fmt.Sprintf("- 节点总数: %d\n", len(report.Nodes)))
	builder.WriteString(fmt.Sprintf("- Pod总数: %d\n", report.Pods.TotalPods))
	builder.WriteString(fmt.Sprintf("- 异常Pod: %d\n", report.Pods.AbnormalPods))
	builder.WriteString(fmt.Sprintf("- 持续重启Pod（>5次）: %d\n", len(highRestartPods)))
	builder.WriteString(fmt.Sprintf("- 核心组件: %d/%d 健康\n",
		countHealthyComponents(report.Components), len(report.Components)))

	// 报告链接（配置 REPORT_BASE_URL 后展示）
	if config.ReportBaseURL != "" {
		builder.WriteString(fmt.Sprintf("\n[🔗 查看完整报告](%s/%s)",
			strings.TrimRight(config.ReportBaseURL, "/"), report.Metadata.ReportFile))
	}

	if builder.Len() == 0 {
		return "⚠️ 巡检报告内容为空，请检查数据源"
	}
	return builder.String()
}

// 发送企业微信通知
func (n *Notifier) SendReport(report *types.InspectionReport, config *config.Config) error {
	message := BuildNotificationMessage(report, config)
	return n.wechatNotifier.SendMarkdownReport(message)
}

// 发送企业微信报告并记录日志
func SendWecomReport(sender *wechat.Notifier, report string, logger *log.Logger) {
	err := sender.SendMarkdownReport(report)
	if err != nil {
		logger.Printf("企业微信消息发送失败: %v", err)
		return
	}
	preview := []rune(report)
	if len(preview) > 100 {
		preview = preview[:100]
	}
	logger.Printf("企业微信消息发送成功, 报告长度: %d, 前100字符: %s", len(report), string(preview))
}

// 获取企业微信通知器实例
func (n *Notifier) WechatNotifier() *wechat.Notifier {
	return n.wechatNotifier
}
