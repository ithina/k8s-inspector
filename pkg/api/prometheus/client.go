// Package prometheus provides a client for querying cluster and node-level
// resource metrics (CPU, memory, disk) from a Prometheus server.
package prometheus

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type NodeResourceUsage struct {
	CPUUsed        float64
	CPUCapacity    float64
	MemoryUsed     float64
	MemoryCapacity float64
	CPUUsage       float64
	MemoryUsage    float64
	DiskUsage      float64
}

type PrometheusClient interface {
	QueryNodeResourceUsage(ctx context.Context) (map[string]NodeResourceUsage, error)
	QueryClusterCPUUsage(ctx context.Context) (float64, error)
	QueryClusterMemoryUsage(ctx context.Context) (float64, error)
}

type prometheusClient struct {
	api v1.API
}

// NewPrometheusClient 根据地址创建 Prometheus 客户端。
// 是否启用 Prometheus 由调用方根据配置决定。
func NewPrometheusClient(address string) (PrometheusClient, error) {
	if address == "" {
		return nil, fmt.Errorf("prometheus address is empty")
	}

	client, err := api.NewClient(api.Config{
		Address: address,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating prometheus client: %v", err)
	}

	return &prometheusClient{
		api: v1.NewAPI(client),
	}, nil
}

func (c *prometheusClient) QueryNodeResourceUsage(ctx context.Context) (map[string]NodeResourceUsage, error) {
	queries := map[string]string{
		"cpu":    `100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle",job="node-exporter"}[5m])) * 100)`,
		"mem":    `(1 - (node_memory_MemAvailable_bytes{job="node-exporter"} / node_memory_MemTotal_bytes{job="node-exporter"})) * 100`,
		"disk":   `max((node_filesystem_size_bytes{job=~"node-exporter",fstype=~"ext.?|xfs"}-node_filesystem_free_bytes{job=~"node-exporter",fstype=~"ext.?|xfs"}) *100/(node_filesystem_avail_bytes{job=~"node-exporter",fstype=~"ext.?|xfs"}+(node_filesystem_size_bytes{job=~"node-exporter",fstype=~"ext.?|xfs"}-node_filesystem_free_bytes{job=~"node-exporter",fstype=~"ext.?|xfs"})))by(instance)`,
		"cpuCap": `count by (instance) (node_cpu_seconds_total{mode="idle",job="node-exporter"})`,
		"memCap": `node_memory_MemTotal_bytes{job="node-exporter"}`,
	}

	results := make(map[string]map[string]float64)
	for k, q := range queries {
		result, err := c.query(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("查询%s失败: %w", k, err)
		}
		results[k] = result
	}

	nodeMap := make(map[string]NodeResourceUsage)

	// 合并所有查询结果
	for node, cpuUsage := range results["cpu"] {
		usage := nodeMap[node]
		usage.CPUUsage = cpuUsage
		nodeMap[node] = usage
	}

	for node, memUsage := range results["mem"] {
		usage := nodeMap[node]
		usage.MemoryUsage = memUsage
		nodeMap[node] = usage
	}

	for node, diskUsage := range results["disk"] {
		usage := nodeMap[node]
		usage.DiskUsage = diskUsage
		nodeMap[node] = usage
	}

	for node, cpuCap := range results["cpuCap"] {
		usage := nodeMap[node]
		usage.CPUCapacity = cpuCap
		nodeMap[node] = usage
	}

	for node, memCap := range results["memCap"] {
		usage := nodeMap[node]
		usage.MemoryCapacity = memCap
		nodeMap[node] = usage
	}

	// 计算实际使用量
	for node, usage := range nodeMap {
		if usage.CPUCapacity > 0 {
			usage.CPUUsed = usage.CPUUsage * usage.CPUCapacity / 100
		}
		if usage.MemoryCapacity > 0 {
			usage.MemoryUsed = usage.MemoryUsage * usage.MemoryCapacity / 100
		}
		nodeMap[node] = usage
	}

	return nodeMap, nil
}

func (c *prometheusClient) QueryClusterCPUUsage(ctx context.Context) (float64, error) {
	query := `avg(1-rate(node_cpu_seconds_total{mode="idle",job="node-exporter"}[5m])) * 100`
	result, err := c.query(ctx, query)
	if err != nil {
		return 0, err
	}

	// 获取集群级别的结果
	if value, ok := result["cluster"]; ok {
		return value, nil
	}

	return 0, fmt.Errorf("no data returned for cluster CPU usage query")
}

func (c *prometheusClient) QueryClusterMemoryUsage(ctx context.Context) (float64, error) {
	query := `sum(node_memory_MemTotal_bytes{job="node-exporter"} - node_memory_MemAvailable_bytes{job="node-exporter"}) / sum(node_memory_MemTotal_bytes{job="node-exporter"}) * 100`
	result, err := c.query(ctx, query)
	if err != nil {
		return 0, err
	}

	// 获取集群级别的结果
	if value, ok := result["cluster"]; ok {
		return value, nil
	}

	return 0, fmt.Errorf("no data returned for cluster memory usage query")
}

func (c *prometheusClient) query(ctx context.Context, query string) (map[string]float64, error) {
	var (
		result   model.Value
		warnings v1.Warnings
		err      error
		lastErr  error
	)

	for i := 0; i < 3; i++ {
		result, warnings, err = c.api.Query(ctx, query, time.Now())
		if err != nil {
			lastErr = fmt.Errorf("query failed: %w", err)
			time.Sleep(2 * time.Second)
			continue
		}
		// 如果成功，则退出循环
		lastErr = nil
		break
	}

	if lastErr != nil {
		return nil, fmt.Errorf("查询Prometheus失败，重试3次后放弃: %w", lastErr)
	}

	if len(warnings) > 0 {
		fmt.Printf("Warnings: %v\n", warnings)
	}

	vector, ok := result.(model.Vector)
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", result)
	}

	values := make(map[string]float64)
	for _, sample := range vector {
		// 尝试从不同的标签中获取节点标识
		node := string(sample.Metric["instance"])
		if node == "" {
			node = string(sample.Metric["node"])
		}
		if node == "" {
			node = "cluster" // 对于集群级别的指标
		}

		// 清理节点名称（去掉端口号等）
		if idx := strings.Index(node, ":"); idx > 0 {
			node = node[:idx]
		}

		value, err := strconv.ParseFloat(sample.Value.String(), 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse value %v: %v", sample.Value, err)
		}
		values[node] = value
	}

	return values, nil
}
