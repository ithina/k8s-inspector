// Package types defines shared data models for inspection reports,
// node status, pod statistics, and component health.
package types

import (
	"encoding/json"
	"fmt"
)

// 报告主结构
type InspectionReport struct {
	Metadata   ReportMetadata    `json:"metadata"`
	Components []ComponentStatus `json:"components"`
	Pods       PodStatistics     `json:"pods"`
	Nodes      []NodeStatus      `json:"nodes"`
	Findings   ReportFindings    `json:"findings"`
}

// 报告元数据
type ReportMetadata struct {
	ClusterName        string  `json:"clusterName"`
	GeneratedAt        string  `json:"generatedAt"`
	Duration           string  `json:"duration"`
	ReportFile         string  `json:"reportFile"`
	ReportFormat       string  `json:"reportFormat" validate:"oneof=html markdown"`
	ClusterCPUUsage    float64 `json:"clusterCpuUsage"`
	ClusterMemoryUsage float64 `json:"clusterMemoryUsage"`
}

// 报告发现项
type ReportFindings struct {
	CriticalComponents []string    `json:"criticalComponents"`
	CriticalNodes      []string    `json:"criticalNodes"`
	WarningNodes       []string    `json:"warningNodes"`
	FailedPods         []PodStatus `json:"failedPods"`
	PendingPods        []PodStatus `json:"pendingPods"`
}

// 节点状态
type NodeStatus struct {
	Name                string          `json:"name"`
	CPUUsage            float64         `json:"cpuUsage"`
	MemoryUsage         float64         `json:"memoryUsage"`
	DiskUsage           float64         `json:"diskUsage"`
	PodCount            int             `json:"podCount"`
	Conditions          []NodeCondition `json:"conditions"`
	Status              string          `json:"status"`
	IsCritical          bool            `json:"isCritical"`
	IsWarning           bool            `json:"isWarning"`
	KubeletVersion      string          `json:"kubeletVersion"`
	ResourceAlerts      []string        `json:"resourceAlerts"`
	HasResourceWarning  bool            `json:"hasResourceWarning"`
	CPUUsed             float64         `json:"cpuUsed"`
	CPUCapacity         float64         `json:"cpuCapacity"`
	MemoryUsed          float64         `json:"memoryUsed"`
	MemoryCapacity      float64         `json:"memoryCapacity"`
	MetricsAvailable    bool            `json:"metricsAvailable"`
	PrometheusAvailable bool            `json:"prometheusAvailable"`
	PrometheusError     string          `json:"prometheusError"`
}

// 节点条件
type NodeCondition struct {
	Type        string `json:"type"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	Message     string `json:"message"`
	DisplayText string `json:"displayText"`
	Icon        string `json:"icon"`
}

// 组件状态
type ComponentStatus struct {
	Name      string      `json:"name"`
	Namespace string      `json:"namespace"`
	Healthy   int         `json:"healthy"`
	Total     int         `json:"total"`
	Pods      []PodStatus `json:"pods"`
	Status    string      `json:"status"`
}

// Pod 状态
type PodStatus struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Status       string `json:"status"`
	RestartCount int    `json:"restartCount"`
	Age          string `json:"age"`
	Message      string `json:"message,omitempty"`
}

// Pod 统计
type PodStatistics struct {
	TotalPods       int                       `json:"totalPods"`
	AbnormalPods    int                       `json:"abnormalPods"`
	ByNamespace     map[string]NamespaceStats `json:"byNamespace"`
	AbnormalPodList []PodStatus               `json:"abnormalPodList"`
}

// 命名空间 Pod 统计
type NamespaceStats struct {
	Running int `json:"running"`
	Pending int `json:"pending"`
	Failed  int `json:"failed"`
	Other   int `json:"other"`
}

// String 实现 fmt.Stringer 接口，返回格式化后的报告内容
func (r *InspectionReport) String() string {
	if r == nil {
		return ""
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Sprintf("巡检报告序列化失败: %v", err)
	}
	return string(b)
}
