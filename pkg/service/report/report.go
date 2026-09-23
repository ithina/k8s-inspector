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

	"k8s-inspector/pkg/config"
	"k8s-inspector/pkg/notify/wechat"
	"k8s-inspector/pkg/types"
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
func GenerateReport(report *types.InspectionReport, config *config.Config, logger io.Writer) error {
	// 直接使用fmt.Println输出日志，确保日志可见
	fmt.Println("\n========================================")
	fmt.Println("          HTML报告生成过程")
	fmt.Println("========================================")
	currentDir, err := getCurrentDir()
	if err != nil {
		fmt.Printf("当前工作目录: 无法获取\n")
	} else {
		fmt.Printf("当前工作目录: %s\n", currentDir)
	}
	fmt.Printf("报告输出目录配置: %s\n", config.ReportOutputDir)

	normalizeThresholds(config)
	now := time.Now()
	report.Metadata.GeneratedAt = now.Format("2006-01-02 15:04:05")
	report.Metadata.ReportFile = fmt.Sprintf(
		ReportFileNameTemplate,
		strings.ToLower(strings.ReplaceAll(config.ClusterName, " ", "-")),
		now.Format("20060102-150405"),
	)

	// 处理报告输出目录，确保在不同操作系统下都能正确工作
	reportOutputDir := config.ReportOutputDir

	// 在Windows环境下，将Linux风格的路径转换为Windows风格的路径
	// 但保持/app/reports的目录结构
	if os.PathSeparator == '\\' {
		// 如果是Windows系统，将/app/reports转换为当前驱动器下的app\reports目录
		// 例如：D:\app\reports
		currentDrive, err := os.Getwd()
		if err == nil && len(currentDrive) > 1 && currentDrive[1] == ':' {
			// 获取当前驱动器
			reportOutputDir = currentDrive[:2] + "\\app\\reports"
		} else {
			// 如果无法获取当前驱动器，使用默认驱动器
			reportOutputDir = "C:\\app\\reports"
		}
	}

	// 获取报告输出目录的绝对路径
	reportOutputDir, err = filepath.Abs(reportOutputDir)
	if err != nil {
		fmt.Printf("无法获取报告输出目录的绝对路径: %v\n", err)
	}

	reportPath := filepath.Join(reportOutputDir, report.Metadata.ReportFile)

	// 获取报告文件的绝对路径
	reportAbsolutePath, err := filepath.Abs(reportPath)
	if err != nil {
		fmt.Printf("无法获取报告文件的绝对路径: %v\n", err)
		reportAbsolutePath = reportPath
	}

	// 输出报告路径，使用绝对路径
	fmt.Printf("\n📄 报告文件信息:")
	fmt.Printf("\n   - 文件名: %s", report.Metadata.ReportFile)
	fmt.Printf("\n   - 相对路径: %s", reportPath)
	fmt.Printf("\n   - 绝对路径: %s\n", reportAbsolutePath)

	data := reportTemplateData{Report: report, Config: config}

	// 加载外部模板文件
	fmt.Println("\n📁 加载HTML模板...")

	// 尝试多种路径查找模板文件
	var templatePath string
	possiblePaths := []string{
		filepath.Join("templates", "report.html"),
		filepath.Join("../..", "templates", "report.html"),
		filepath.Join("../../..", "templates", "report.html"),
		filepath.Join("../../../..", "templates", "report.html"),
	}

	// 检查是否存在当前目录的绝对路径
	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		possiblePaths = append(possiblePaths, filepath.Join(execDir, "templates", "report.html"))
		possiblePaths = append(possiblePaths, filepath.Join(execDir, "..", "templates", "report.html"))
		possiblePaths = append(possiblePaths, filepath.Join(execDir, "../..", "templates", "report.html"))
	}

	// 添加项目根目录的可能路径
	if currentDir, err := getCurrentDir(); err == nil {
		possiblePaths = append(possiblePaths, filepath.Join(currentDir, "templates", "report.html"))
		possiblePaths = append(possiblePaths, filepath.Join(currentDir, "..", "templates", "report.html"))
	}

	// 查找存在的模板文件
	var found bool
	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			templatePath = path
			found = true
			break
		}
	}

	if !found {
		fmt.Printf("❌ 未找到模板文件，尝试路径: %v\n", possiblePaths)
		return fmt.Errorf("未找到模板文件: report.html")
	}

	// 获取模板文件的绝对路径
	templateAbsolutePath, err := filepath.Abs(templatePath)
	if err != nil {
		templateAbsolutePath = templatePath
	}

	fmt.Printf("✅ 找到模板文件: %s\n", templateAbsolutePath)
	tmpl, err := template.New("report.html").Funcs(templateFuncs).ParseFiles(templatePath)
	if err != nil {
		fmt.Printf("❌ 模板解析失败: %v\n", err)
		return fmt.Errorf("模板解析失败: %w", err)
	}

	// 创建报告目录
	fmt.Println("\n📂 创建报告目录...")
	if err := os.MkdirAll(reportOutputDir, 0755); err != nil {
		fmt.Printf("❌ 创建报告目录失败: %v\n", err)
		return fmt.Errorf("创建报告目录失败: %w", err)
	}
	fmt.Printf("✅ 报告目录已创建或存在: %s\n", reportOutputDir)

	// 创建报告文件
	fmt.Println("\n📝 创建报告文件...")
	file, err := os.Create(reportPath)
	if err != nil {
		fmt.Printf("❌ 创建报告文件失败: %v\n", err)
		return fmt.Errorf("创建报告文件失败: %w", err)
	}
	defer file.Close()
	fmt.Printf("✅ 报告文件已创建: %s\n", reportPath)

	// 使用模板文件名渲染
	fmt.Println("\n🎨 渲染HTML报告...")
	if err := tmpl.ExecuteTemplate(file, "report.html", data); err != nil {
		fmt.Printf("❌ 模板渲染失败: %v\n", err)
		return fmt.Errorf("模板渲染失败: %w", err)
	}

	fmt.Println("\n========================================")
	fmt.Println("          HTML报告生成完成")
	fmt.Println("========================================")
	fmt.Printf("✅ 报告已成功生成！\n")
	fmt.Printf("📁 报告目录: %s\n", reportOutputDir)
	fmt.Printf("📄 报告文件: %s\n", report.Metadata.ReportFile)
	fmt.Printf("🔗 完整路径: %s\n", reportAbsolutePath)
	fmt.Println("========================================")
	return nil
}

// 获取当前工作目录
func getCurrentDir() (string, error) {
	return os.Getwd()
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

	// 报告链接
	builder.WriteString(fmt.Sprintf("\n[🔗 查看完整报告](http://k8s-reports.xx/reports/%s)", report.Metadata.ReportFile))

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
