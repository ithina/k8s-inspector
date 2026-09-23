// Package inspector implements the core Kubernetes cluster inspection logic:
// node health, pod lifecycle, component status, and resource metric collection.
package inspector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ithina/k8s-inspector/pkg/api/prometheus"
	"github.com/ithina/k8s-inspector/pkg/config"
	"github.com/ithina/k8s-inspector/pkg/types"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	defaultInspectionNamespace = "kube-system"
)

var criticalComponents = []string{
	"kube-apiserver",
	"kube-controller-manager",
	"kube-scheduler",
	"etcd",
	"coredns",
}

// 巡检服务
type Inspector struct {
	clientset  *kubernetes.Clientset
	promClient prometheus.PrometheusClient
	config     *config.Config
	logger     *zap.Logger
	// excludeNS 需要跳过 Pod 巡检的命名空间集合（来自 EXCLUDE_NAMESPACES 配置）
	excludeNS map[string]struct{}
}

// 创建巡检服务实例
func NewInspector(cfg *config.Config, logger *zap.Logger) (*Inspector, error) {
	clientset, err := initKubeClient()
	if err != nil {
		return nil, fmt.Errorf("创建Kubernetes客户端失败: %w", err)
	}

	// Prometheus 为可选依赖：未配置时跳过指标采集，巡检降级运行
	var promClient prometheus.PrometheusClient
	if cfg.PrometheusURL != "" {
		promClient, err = prometheus.NewPrometheusClient(cfg.PrometheusURL)
		if err != nil {
			return nil, fmt.Errorf("创建Prometheus客户端失败: %w", err)
		}
	} else {
		logger.Warn("PROMETHEUS_URL 未配置，跳过指标采集，报告不包含资源使用率")
	}

	if err := os.MkdirAll(cfg.ReportOutputDir, 0755); err != nil {
		return nil, fmt.Errorf("创建报告目录失败: %w", err)
	}

	excludeNS := make(map[string]struct{}, len(cfg.ExcludeNamespaces))
	for _, ns := range cfg.ExcludeNamespaces {
		excludeNS[ns] = struct{}{}
	}

	return &Inspector{
		clientset:  clientset,
		promClient: promClient,
		config:     cfg,
		logger:     logger,
		excludeNS:  excludeNS,
	}, nil
}

// 执行巡检
func (i *Inspector) RunInspection(ctx context.Context) (*types.InspectionReport, error) {
	startTime := time.Now()
	report := &types.InspectionReport{
		Metadata: types.ReportMetadata{
			ClusterName:  i.config.ReportClusterName,
			GeneratedAt:  time.Now().In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05"),
			ReportFormat: "html",
		},
	}

	var (
		wg                               sync.WaitGroup
		components                       []types.ComponentStatus
		pods                             types.PodStatistics
		nodes                            []types.NodeStatus
		componentsErr, podsErr, nodesErr error
	)

	wg.Add(3)
	go func() {
		defer wg.Done()
		i.logger.Info("start to get component information")
		components, componentsErr = i.CheckComponents(ctx)
		i.logger.Info("component information acquisition completed")
	}()
	go func() {
		defer wg.Done()
		i.logger.Info("start to get Pod information")
		pods, podsErr = i.CheckPods(ctx)
		i.logger.Info("Pod information acquisition completed")
	}()
	go func() {
		defer wg.Done()
		i.logger.Info("start to get node information")
		nodes, nodesErr = i.CheckNodes(ctx)
		i.logger.Info("node information acquisition completed")
	}()
	wg.Wait()

	if componentsErr != nil {
		return nil, fmt.Errorf("组件检查失败: %w", componentsErr)
	}
	if podsErr != nil {
		return nil, fmt.Errorf("pod检查失败: %w", podsErr)
	}
	if nodesErr != nil {
		return nil, fmt.Errorf("节点检查失败: %w", nodesErr)
	}

	report.Components = components
	report.Pods = pods
	report.Nodes = nodes
	report.Metadata.Duration = time.Since(startTime).Round(time.Millisecond).String()
	report.Findings = i.AnalyzeFindings(report)

	// 集群级资源使用率（Prometheus 未配置时跳过）
	if i.promClient != nil {
		promCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()

		cpuUsage, err := i.promClient.QueryClusterCPUUsage(promCtx)
		if err != nil {
			i.logger.Error("Failed to query cluster CPU usage", zap.Error(err))
		} else {
			report.Metadata.ClusterCPUUsage = cpuUsage
		}

		memUsage, err := i.promClient.QueryClusterMemoryUsage(promCtx)
		if err != nil {
			i.logger.Error("Failed to query cluster memory usage", zap.Error(err))
		} else {
			report.Metadata.ClusterMemoryUsage = memUsage
		}
	}

	return report, nil
}

// 检查集群组件状态
func (i *Inspector) CheckComponents(ctx context.Context) ([]types.ComponentStatus, error) {
	sem := make(chan struct{}, i.config.MaxConcurrency)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		results  []types.ComponentStatus
		firstErr error
		once     sync.Once
	)

	for _, component := range criticalComponents {
		wg.Add(1)
		go func(component string) {
			sem <- struct{}{}
			defer func() { <-sem }()
			defer wg.Done()

			i.logger.Info("Checking component", zap.String("component", component))
			status, err := i.checkSingleComponent(ctx, component)
			if err != nil {
				once.Do(func() { firstErr = fmt.Errorf("%s检查失败: %w", component, err) })
				i.logger.Error("Failed to check component", zap.String("component", component), zap.Error(err))
				return
			}

			mu.Lock()
			results = append(results, *status)
			mu.Unlock()
		}(component)
	}

	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results, nil
}

// 检查Pod状态
func (i *Inspector) CheckPods(ctx context.Context) (types.PodStatistics, error) {
	stats := types.PodStatistics{ByNamespace: make(map[string]types.NamespaceStats)}
	namespaces, err := i.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return stats, fmt.Errorf("获取命名空间列表失败: %w", err)
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
		once     sync.Once
		sem      = make(chan struct{}, i.config.MaxConcurrency)
	)

	for _, ns := range namespaces.Items {
		if _, skip := i.excludeNS[ns.Name]; skip {
			continue // 跳过配置中排除的命名空间
		}
		wg.Add(1)
		go func(namespace string) {
			defer wg.Done()
			sem <- struct{}{}        // 阻塞直到有空位
			defer func() { <-sem }() // 释放信号量

			pods, err := i.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
			if err != nil {
				once.Do(func() {
					firstErr = fmt.Errorf("获取%s命名空间的Pod失败: %w", namespace, err)
				})
				return
			}

			defer func() {
				if pods != nil {
					// 显式释放资源
					pods.Items = nil
				}
			}()
			nsStats := types.NamespaceStats{}
			var abnormalPods []types.PodStatus
			for _, pod := range pods.Items {
				status := getPodStatus(&pod)
				podStatus := types.PodStatus{
					Name:         pod.Name,
					Namespace:    pod.Namespace,
					Status:       status,
					RestartCount: int(getRestartCount(&pod)),
					Age:          getAge(pod.CreationTimestamp.Time),
					Message:      getPodMessage(&pod),
				}

				switch status {
				case "Running":
					nsStats.Running++
				case "Pending":
					nsStats.Pending++
				case "Failed":
					nsStats.Failed++
				default:
					nsStats.Other++
				}

				if isPodStatusAbnormal(status) {
					abnormalPods = append(abnormalPods, podStatus)
				}
			}
			mu.Lock()
			stats.TotalPods += len(pods.Items)
			stats.AbnormalPods += len(abnormalPods)
			stats.ByNamespace[namespace] = nsStats
			stats.AbnormalPodList = append(stats.AbnormalPodList, abnormalPods...)
			mu.Unlock()
		}(ns.Name)
	}

	wg.Wait()
	if firstErr != nil {
		return stats, firstErr
	}
	return stats, nil
}

func formatKubeletVersion(version string) string {
	if parts := strings.Split(version, "-"); len(parts) > 0 {
		return parts[0]
	}
	return version
}

// 检查节点状态
func (i *Inspector) CheckNodes(ctx context.Context) ([]types.NodeStatus, error) {
	nodeList, err := i.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("获取节点列表失败: %w", err)
	}
	promCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// 节点资源指标依赖 Prometheus，未配置或查询失败时降级：
	// 节点健康检查继续执行，仅缺少 CPU/内存/磁盘使用率数据
	promUsage := make(map[string]prometheus.NodeResourceUsage)
	if i.promClient != nil {
		var promErr error
		promUsage, promErr = i.promClient.QueryNodeResourceUsage(promCtx)
		if promErr != nil {
			i.logger.Warn("Prometheus 查询节点资源失败，跳过资源指标", zap.Error(promErr))
			promUsage = make(map[string]prometheus.NodeResourceUsage)
		}
	} else {
		i.logger.Warn("Prometheus 未配置，跳过节点资源指标采集")
	}

	// 获取所有运行中的Pod并按节点分组
	pods, err := i.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("获取Pod列表失败: %w", err)
	}

	// 按节点分组统计Pod数量
	podsByNode := make(map[string]int)
	for _, pod := range pods.Items {
		if pod.Spec.NodeName != "" && pod.Status.Phase == corev1.PodRunning {
			podsByNode[pod.Spec.NodeName]++
		}
	}

	var (
		nodes   = make([]types.NodeStatus, 0, len(nodeList.Items))
		wg      sync.WaitGroup
		mu      sync.Mutex
		errChan = make(chan error, len(nodeList.Items)*2)
	)
	sem := make(chan struct{}, i.config.MaxConcurrency)
	for _, node := range nodeList.Items {
		wg.Add(1)
		go func(node corev1.Node) {
			defer func() {
				if r := recover(); r != nil {
					select {
					case errChan <- fmt.Errorf("goroutine panic: %v", r):
					default:
					}
				}
				wg.Done()
			}()
			sem <- struct{}{}
			defer func() { <-sem }()
			status := types.NodeStatus{
				Name:                node.Name,
				KubeletVersion:      formatKubeletVersion(node.Status.NodeInfo.KubeletVersion),
				PrometheusAvailable: false,
				PodCount:            podsByNode[node.Name],
			}
			if usage, ok := promUsage[node.Name]; ok {
				status.PrometheusAvailable = true
				status.CPUUsed = usage.CPUUsed
				status.CPUCapacity = usage.CPUCapacity
				status.MemoryUsed = usage.MemoryUsed
				status.MemoryCapacity = usage.MemoryCapacity
				if status.CPUCapacity > 0 {
					status.CPUUsage = (status.CPUUsed / status.CPUCapacity) * 100
				}
				if status.MemoryCapacity > 0 {
					status.MemoryUsage = (status.MemoryUsed / status.MemoryCapacity) * 100
				}
				status.DiskUsage = usage.DiskUsage
			} else {
				status.PrometheusError = "未获取到Prometheus数据"
			}
			processedConditions := make(map[corev1.NodeConditionType]struct{})
			for _, cond := range node.Status.Conditions {
				if !isTargetCondition(cond.Type) {
					continue
				}
				if _, exists := processedConditions[cond.Type]; exists {
					continue
				}
				processedConditions[cond.Type] = struct{}{}
				displayText, icon := getConditionDisplay(cond)
				status.Conditions = append(status.Conditions, types.NodeCondition{
					Type:        string(cond.Type),
					Status:      string(cond.Status),
					DisplayText: displayText,
					Icon:        icon,
					Reason:      cond.Reason,
					Message:     cond.Message,
				})
			}
			var readyStatus string
			for _, cond := range node.Status.Conditions {
				if cond.Type == corev1.NodeReady {
					readyStatus = map[string]string{
						"True":  "Ready",
						"False": "NotReady",
					}[string(cond.Status)]
				}
			}
			status.Status = readyStatus
			var statusMessages []string
			if status.CPUUsage > float64(i.config.CriticalThreshold) {
				status.IsCritical = true
				statusMessages = append(statusMessages, fmt.Sprintf("CPU(%.1f%%)", status.CPUUsage))
			} else if status.CPUUsage > float64(i.config.WarningThreshold) {
				status.IsWarning = true
				statusMessages = append(statusMessages, fmt.Sprintf("CPU(%.1f%%)", status.CPUUsage))
			}
			if status.MemoryUsage > float64(i.config.CriticalThreshold) {
				status.IsCritical = true
				statusMessages = append(statusMessages, fmt.Sprintf("内存(%.1f%%)", status.MemoryUsage))
			} else if status.MemoryUsage > float64(i.config.WarningThreshold) {
				status.IsWarning = true
				statusMessages = append(statusMessages, fmt.Sprintf("内存(%.1f%%)", status.MemoryUsage))
			}
			if status.DiskUsage > float64(i.config.CriticalThreshold) {
				status.IsCritical = true
				statusMessages = append(statusMessages, fmt.Sprintf("磁盘(%.1f%%)", status.DiskUsage))
			} else if status.DiskUsage > float64(i.config.WarningThreshold) {
				status.IsWarning = true
				statusMessages = append(statusMessages, fmt.Sprintf("磁盘(%.1f%%)", status.DiskUsage))
			}
			status.ResourceAlerts = statusMessages
			status.HasResourceWarning = len(statusMessages) > 0
			mu.Lock()
			nodes = append(nodes, status)
			mu.Unlock()
		}(node)
	}
	wg.Wait()
	close(errChan)
	for err := range errChan {
		if err != nil {
			return nodes, err
		}
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].MemoryUsage > nodes[j].MemoryUsage
	})
	return nodes, nil
}

// 分析巡检结果
func (i *Inspector) AnalyzeFindings(report *types.InspectionReport) types.ReportFindings {
	findings := types.ReportFindings{}
	for _, comp := range report.Components {
		if comp.Healthy < comp.Total {
			findings.CriticalComponents = append(findings.CriticalComponents,
				fmt.Sprintf("%s: %s (%d/%d)", comp.Name, comp.Status, comp.Healthy, comp.Total))
		}
	}
	for _, node := range report.Nodes {
		if node.Status == "NotReady" {
			findings.CriticalNodes = append(findings.CriticalNodes,
				fmt.Sprintf("%s: %s", node.Name, node.Status))
		} else if node.HasResourceWarning {
			findings.WarningNodes = append(findings.WarningNodes,
				fmt.Sprintf("%s: %s (%s)", node.Name, node.Status, strings.Join(node.ResourceAlerts, ", ")))
		}
	}
	for _, pod := range report.Pods.AbnormalPodList {
		switch pod.Status {
		case "Failed":
			findings.FailedPods = append(findings.FailedPods, pod)
		case "Pending":
			findings.PendingPods = append(findings.PendingPods, pod)
		}
	}
	return findings
}

// 获取日志记录器
func (i *Inspector) Logger() *zap.Logger {
	return i.logger
}

// K8s客户端初始化
func initKubeClient() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		var kubeconfig string
		// 优先使用 KUBECONFIG 环境变量
		if envKubeconfig := os.Getenv("KUBECONFIG"); envKubeconfig != "" {
			kubeconfig = envKubeconfig
		} else {
			home, homeErr := os.UserHomeDir()
			if homeErr != nil {
				return nil, fmt.Errorf("无法获取用户主目录: %w", homeErr)
			}
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("无法加载 kubeconfig: %w", err)
		}
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("创建 Kubernetes ClientSet 失败: %w", err)
	}
	return clientset, nil
}

// 工具函数
func getComponentSelector(name string) (string, error) {
	switch name {
	case "kube-apiserver":
		return "component=kube-apiserver", nil
	case "kube-controller-manager":
		return "component=kube-controller-manager", nil
	case "kube-scheduler":
		return "component=kube-scheduler", nil
	case "etcd":
		return "component=etcd", nil
	case "coredns":
		return "k8s-app=kube-dns", nil
	default:
		return "", fmt.Errorf("未知组件: %s", name)
	}
}

func isTargetCondition(t corev1.NodeConditionType) bool {
	return t == corev1.NodePIDPressure
}

func getConditionDisplay(cond corev1.NodeCondition) (string, string) {
	var displayText, icon string
	switch cond.Type {
	case corev1.NodePIDPressure:
		displayText = map[string]string{
			"True":  "进程号不足⚠️",
			"False": "进程号充足✅",
		}[string(cond.Status)]
		icon = "🎯"
	}
	return displayText, icon
}

func isPodStatusAbnormal(status string) bool {
	normalTransientStates := map[string]struct{}{
		"ContainerCreating": {},
		"PodInitializing":   {},
		"Running":           {},
		"Completed":         {},
		"Succeeded":         {},
	}
	_, isNormal := normalTransientStates[status]
	return !isNormal
}

func getPodStatus(pod *corev1.Pod) string {
	for _, c := range pod.Status.ContainerStatuses {
		if c.State.Waiting != nil && c.State.Waiting.Reason != "" {
			return c.State.Waiting.Reason
		}
		if c.State.Terminated != nil && c.State.Terminated.Reason != "" {
			return c.State.Terminated.Reason
		}
	}

	switch pod.Status.Phase {
	case corev1.PodRunning:
		return "Running"
	case corev1.PodPending:
		return "Pending"
	case corev1.PodFailed:
		return "Failed"
	case corev1.PodSucceeded:
		return "Succeeded"
	case corev1.PodUnknown:
		return "Unknown"
	default:
		return string(pod.Status.Phase) // Fallback for any other unexpected phase
	}
}

func getRestartCount(pod *corev1.Pod) int32 {
	var count int32
	for _, cs := range pod.Status.ContainerStatuses {
		count += cs.RestartCount
	}
	return count
}

func getAge(t time.Time) string {
	return fmt.Sprintf("%dd%dh", int(time.Since(t).Hours())/24, int(time.Since(t).Hours())%24)
}

func getPodMessage(pod *corev1.Pod) string {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Message != "" {
			return cs.State.Waiting.Message
		}
		if cs.State.Terminated != nil && cs.State.Terminated.Message != "" {
			return cs.State.Terminated.Message
		}
	}
	return pod.Status.Message
}

// 检查单个组件状态
func (i *Inspector) checkSingleComponent(ctx context.Context, component string) (*types.ComponentStatus, error) {
	selector, err := getComponentSelector(component)
	if err != nil {
		return nil, err
	}

	pods, err := i.clientset.CoreV1().Pods(defaultInspectionNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("获取%s Pod列表失败: %w", component, err)
	}

	status := &types.ComponentStatus{
		Name:      component,
		Namespace: defaultInspectionNamespace,
		Total:     len(pods.Items),
	}

	var podStatuses []types.PodStatus
	for _, pod := range pods.Items {
		ps := types.PodStatus{
			Name:         pod.Name,
			Namespace:    pod.Namespace,
			Status:       string(pod.Status.Phase),
			RestartCount: int(getRestartCount(&pod)),
			Age:          getAge(pod.CreationTimestamp.Time),
		}
		podStatuses = append(podStatuses, ps)

		if isPodReady(&pod) && pod.Status.Phase == corev1.PodRunning {
			status.Healthy++
		}
	}

	if status.Healthy == status.Total {
		status.Status = "healthy"
	} else {
		status.Status = fmt.Sprintf("unhealthy (%d/%d)", status.Healthy, status.Total)
	}
	status.Pods = podStatuses

	return status, nil
}

func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}
