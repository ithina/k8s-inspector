package report

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ithina/k8s-inspector/pkg/config"
	"github.com/ithina/k8s-inspector/pkg/types"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		ClusterName:       "test-cluster",
		ReportClusterName: "测试集群",
		ReportOutputDir:   t.TempDir(),
		CriticalThreshold: 90,
		WarningThreshold:  80,
		MaxConcurrency:    10,
	}
}

func TestBuildCapacitySummary(t *testing.T) {
	report := &types.InspectionReport{
		Nodes: []types.NodeStatus{
			{Name: "high-mem", Status: "Ready", MemoryUsage: 91, PodCount: 30},
			{Name: "high-pod", Status: "Ready", MemoryUsage: 50, PodCount: 60},
			{Name: "low-load", Status: "Ready", MemoryUsage: 10, PodCount: 5},
			{Name: "normal", Status: "Ready", MemoryUsage: 50, PodCount: 30},
			{Name: "not-ready", Status: "NotReady", MemoryUsage: 99, PodCount: 99},
		},
	}

	summary := BuildCapacitySummary(report)

	for _, want := range []string{
		"容量优化指标",
		"高内存使用率节点",
		"high-mem",
		"高POD密度节点",
		"high-pod",
		"低内存使用率节点",
		"低POD密度节点",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("BuildCapacitySummary() 缺少 %q:\n%s", want, summary)
		}
	}
	if strings.Contains(summary, "not-ready") {
		t.Errorf("BuildCapacitySummary() 不应统计非 Ready 节点:\n%s", summary)
	}
}

func TestBuildCapacitySummary_NoReadyNodes(t *testing.T) {
	report := &types.InspectionReport{
		Nodes: []types.NodeStatus{{Name: "n1", Status: "NotReady"}},
	}
	if got := BuildCapacitySummary(report); got != "" {
		t.Errorf("BuildCapacitySummary() = %q, want 空字符串", got)
	}
}

func TestBuildNotificationMessage_ReportLink(t *testing.T) {
	report := &types.InspectionReport{
		Metadata: types.ReportMetadata{
			ClusterName: "测试集群",
			GeneratedAt: "2026-01-01 10:00:00",
			Duration:    "1s",
		},
	}

	withLink := testConfig(t)
	withLink.ReportBaseURL = "https://reports.example.com/reports/"
	msg := BuildNotificationMessage(report, withLink)
	if !strings.Contains(msg, "https://reports.example.com/reports/") {
		t.Errorf("配置 REPORT_BASE_URL 后应包含报告链接:\n%s", msg)
	}

	withoutLink := testConfig(t)
	msg = BuildNotificationMessage(report, withoutLink)
	if strings.Contains(msg, "查看完整报告") {
		t.Errorf("未配置 REPORT_BASE_URL 时不应包含报告链接:\n%s", msg)
	}
}

func TestGenerateReport(t *testing.T) {
	cfg := testConfig(t)
	report := &types.InspectionReport{
		Metadata: types.ReportMetadata{ClusterName: cfg.ReportClusterName},
		Nodes: []types.NodeStatus{
			{Name: "node-1", Status: "Ready", MemoryUsage: 55.5, CPUUsage: 30.2, DiskUsage: 40, PodCount: 20},
		},
		Components: []types.ComponentStatus{
			{Name: "coredns", Healthy: 2, Total: 2, Status: "healthy"},
		},
		Pods: types.PodStatistics{
			TotalPods:   10,
			ByNamespace: map[string]types.NamespaceStats{"default": {Running: 10}},
		},
	}

	if err := GenerateReport(report, cfg, io.Discard); err != nil {
		t.Fatalf("GenerateReport() error = %v", err)
	}
	if report.Metadata.ReportFile == "" {
		t.Fatal("GenerateReport() 未设置 ReportFile")
	}

	content, err := os.ReadFile(filepath.Join(cfg.ReportOutputDir, report.Metadata.ReportFile))
	if err != nil {
		t.Fatalf("读取报告文件失败: %v", err)
	}
	if !strings.Contains(string(content), cfg.ReportClusterName) {
		t.Errorf("报告内容应包含集群名 %q", cfg.ReportClusterName)
	}
}
