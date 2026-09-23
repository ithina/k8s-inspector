package main

import (
	"fmt"
	"os"
	"time"

	"k8s-inspector/pkg/config"
	"k8s-inspector/pkg/service/report"
	"k8s-inspector/pkg/types"

	"go.uber.org/zap"
)

func main() {
	// 初始化zap日志
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Printf("初始化日志失败: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()
	sugar := logger.Sugar()

	// 加载配置
	cfg, err := loadTestConfig()
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		os.Exit(1)
	}
	sugar.Info("配置加载成功")

	// 打印配置信息
	fmt.Println("\n========================================")
	fmt.Println("          配置信息")
	fmt.Println("========================================")
	fmt.Printf("集群名称: %s\n", cfg.ClusterName)
	fmt.Printf("报告输出目录: %s\n", cfg.ReportOutputDir)
	fmt.Printf("警告阈值: %d%%\n", cfg.WarningThreshold)
	fmt.Printf("严重阈值: %d%%\n", cfg.CriticalThreshold)
	fmt.Println("========================================")

	// 创建模拟的巡检报告数据
	inspectionReport := createMockInspectionReport(cfg)

	// 确保报告目录存在
	if err := os.MkdirAll(cfg.ReportOutputDir, 0755); err != nil {
		fmt.Printf("创建报告目录失败: %v\n", err)
		os.Exit(1)
	}

	// 生成HTML报告
	fmt.Println("=== 开始生成HTML报告 ===")
	if err := report.GenerateReport(inspectionReport, cfg, os.Stdout); err != nil {
		sugar.Errorw("生成报告失败", "error", err)
		fmt.Printf("生成报告失败: %v\n", err)
		os.Exit(1)
	} else {
		sugar.Info("报告生成成功")
		fmt.Println("报告生成成功")
	}

	// 查找生成的报告文件
	fmt.Println("\n=== 查找生成的报告文件 ===")
	findReportFiles(cfg.ReportOutputDir)

	fmt.Println("\n=== 测试完成 ===")
}

// 加载测试配置
func loadTestConfig() (*config.Config, error) {
	// 设置环境变量
	os.Setenv("CLUSTER_NAME", "test-cluster")

	// 加载配置
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("加载配置失败: %w", err)
	}

	// 不修改报告输出目录，使用默认配置
	// cfg.ReportOutputDir = "reports"

	return cfg, nil
}

// 创建模拟的巡检报告数据
func createMockInspectionReport(cfg *config.Config) *types.InspectionReport {
	return &types.InspectionReport{
		Metadata: types.ReportMetadata{
			ClusterName:        cfg.ReportClusterName,
			GeneratedAt:        time.Now().Format("2006-01-02 15:04:05"),
			Duration:           "10.5s",
			ReportFormat:       "html",
			ClusterCPUUsage:    45.5,
			ClusterMemoryUsage: 67.8,
		},
		Nodes: []types.NodeStatus{
			{
				Name:           "node-1",
				Status:         "Ready",
				CPUUsage:       50.2,
				MemoryUsage:    70.5,
				DiskUsage:      45.8,
				KubeletVersion: "v1.28.0",
				PodCount:       32,
				Conditions:     []types.NodeCondition{},
			},
			{
				Name:           "node-2",
				Status:         "Ready",
				CPUUsage:       40.1,
				MemoryUsage:    65.3,
				DiskUsage:      50.2,
				KubeletVersion: "v1.28.0",
				PodCount:       28,
				Conditions:     []types.NodeCondition{},
			},
			{
				Name:           "node-3",
				Status:         "Ready",
				CPUUsage:       85.7,
				MemoryUsage:    90.3,
				DiskUsage:      75.6,
				KubeletVersion: "v1.28.0",
				PodCount:       65,
				Conditions:     []types.NodeCondition{},
			},
			{
				Name:           "node-4",
				Status:         "Ready",
				CPUUsage:       15.3,
				MemoryUsage:    25.8,
				DiskUsage:      30.1,
				KubeletVersion: "v1.28.0",
				PodCount:       15,
				Conditions:     []types.NodeCondition{},
			},
		},
		Components: []types.ComponentStatus{
			{
				Name:      "kube-apiserver",
				Namespace: "kube-system",
				Healthy:   3,
				Total:     3,
				Status:    "3/3 Running",
				Pods:      []types.PodStatus{},
			},
			{
				Name:      "kube-controller-manager",
				Namespace: "kube-system",
				Healthy:   3,
				Total:     3,
				Status:    "3/3 Running",
				Pods:      []types.PodStatus{},
			},
			{
				Name:      "coredns",
				Namespace: "kube-system",
				Healthy:   2,
				Total:     2,
				Status:    "2/2 Running",
				Pods:      []types.PodStatus{},
			},
		},
		Pods: types.PodStatistics{
			TotalPods:    140,
			AbnormalPods: 8,
			ByNamespace: map[string]types.NamespaceStats{
				"default":     {Running: 30, Pending: 2, Failed: 1, Other: 0},
				"kube-system": {Running: 50, Pending: 0, Failed: 0, Other: 0},
				"app":         {Running: 40, Pending: 1, Failed: 2, Other: 0},
			},
			AbnormalPodList: []types.PodStatus{
				{
					Name:         "app-pod-1",
					Namespace:    "app",
					Status:       "Failed",
					RestartCount: 5,
					Age:          "10m",
					Message:      "CrashLoopBackOff",
				},
				{
					Name:         "app-pod-2",
					Namespace:    "app",
					Status:       "Pending",
					RestartCount: 0,
					Age:          "5m",
					Message:      "Insufficient memory",
				},
			},
		},
		Findings: types.ReportFindings{
			CriticalComponents: []string{},
			CriticalNodes:      []string{"node-3"},
			WarningNodes:       []string{},
			FailedPods:         []types.PodStatus{},
			PendingPods:        []types.PodStatus{},
		},
	}
}

// 查找生成的报告文件
func findReportFiles(reportDir string) {
	// 处理报告目录，确保在不同操作系统下都能正确工作
	processedReportDir := reportDir

	// 在Windows环境下，将Linux风格的路径转换为Windows风格的路径
	if os.PathSeparator == '\\' {
		// 如果是Windows系统，将/app/reports转换为当前驱动器下的app\reports目录
		// 例如：D:\app\reports
		currentDrive, err := os.Getwd()
		if err == nil && len(currentDrive) > 1 && currentDrive[1] == ':' {
			// 获取当前驱动器
			processedReportDir = currentDrive[:2] + "\\app\\reports"
		} else {
			// 如果无法获取当前驱动器，使用默认驱动器
			processedReportDir = "C:\\app\\reports"
		}
	}

	// 获取报告目录的绝对路径
	reportDirAbs, err := getAbsolutePath(processedReportDir)
	if err != nil {
		fmt.Printf("无法获取报告目录的绝对路径: %v\n", err)
		reportDirAbs = processedReportDir
	}

	fmt.Printf("报告目录: %s\n", reportDirAbs)

	// 查找HTML文件
	files, err := os.ReadDir(reportDirAbs)
	if err != nil {
		fmt.Printf("无法读取报告目录: %v\n", err)
		return
	}

	htmlFiles := []string{}
	for _, file := range files {
		if !file.IsDir() && hasSuffixIgnoreCase(file.Name(), ".html") {
			htmlFiles = append(htmlFiles, file.Name())
		}
	}

	if len(htmlFiles) == 0 {
		fmt.Println("❌ 未找到HTML报告文件")
		return
	}

	fmt.Printf("✅ 找到 %d 个HTML报告文件:\n", len(htmlFiles))
	for _, file := range htmlFiles {
		filePath := reportDirAbs + string(os.PathSeparator) + file
		fmt.Printf("   - %s\n", filePath)

		// 检查文件大小
		fileInfo, err := os.Stat(filePath)
		if err != nil {
			fmt.Printf("     无法获取文件信息: %v\n", err)
			continue
		}
		fmt.Printf("     大小: %d 字节\n", fileInfo.Size())
		fmt.Printf("     创建时间: %s\n", fileInfo.ModTime().Format("2006-01-02 15:04:05"))
	}

	// 提供打开报告的指导
	fmt.Println("\n📝 如何打开报告:")
	if len(htmlFiles) > 0 {
		latestFile := htmlFiles[len(htmlFiles)-1]
		latestFilePath := reportDirAbs + string(os.PathSeparator) + latestFile
		fmt.Printf("1. 复制以下路径到文件资源管理器地址栏:\n   %s\n", reportDirAbs)
		fmt.Printf("2. 双击打开文件: %s\n", latestFile)
		fmt.Printf("3. 或直接复制完整路径到浏览器地址栏:\n   file://%s\n", latestFilePath)
	}
}

// 获取文件或目录的绝对路径
func getAbsolutePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("路径不能为空")
	}

	// 如果已经是绝对路径，直接返回
	if path[0] == '/' || (len(path) > 1 && path[1] == ':') {
		return path, nil
	}

	// 获取当前工作目录
	currentDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("获取当前工作目录失败: %w", err)
	}

	// 拼接绝对路径
	absPath := currentDir + "/" + path
	return absPath, nil
}

// 忽略大小写检查文件后缀
func hasSuffixIgnoreCase(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}

	sLower := s[len(s)-len(suffix):]
	for i := 0; i < len(sLower); i++ {
		c1 := sLower[i]
		c2 := suffix[i]
		if c1 >= 'A' && c1 <= 'Z' {
			c1 += 'a' - 'A'
		}
		if c2 >= 'A' && c2 <= 'Z' {
			c2 += 'a' - 'A'
		}
		if c1 != c2 {
			return false
		}
	}

	return true
}
