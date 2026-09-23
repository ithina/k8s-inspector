package inspector

import (
	"strings"
	"testing"
	"time"

	"github.com/ithina/k8s-inspector/pkg/types"
	corev1 "k8s.io/api/core/v1"
)

func podWithPhase(phase corev1.PodPhase, cs ...corev1.ContainerStatus) *corev1.Pod {
	return &corev1.Pod{
		Status: corev1.PodStatus{Phase: phase, ContainerStatuses: cs},
	}
}

func TestGetPodStatus(t *testing.T) {
	tests := []struct {
		name string
		pod  *corev1.Pod
		want string
	}{
		{
			name: "Waiting原因优先（CrashLoopBackOff）",
			pod: podWithPhase(corev1.PodRunning, corev1.ContainerStatus{
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
			}),
			want: "CrashLoopBackOff",
		},
		{
			name: "Terminated原因优先（OOMKilled）",
			pod: podWithPhase(corev1.PodRunning, corev1.ContainerStatus{
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled"}},
			}),
			want: "OOMKilled",
		},
		{name: "Running阶段", pod: podWithPhase(corev1.PodRunning), want: "Running"},
		{name: "Pending阶段", pod: podWithPhase(corev1.PodPending), want: "Pending"},
		{name: "Failed阶段", pod: podWithPhase(corev1.PodFailed), want: "Failed"},
		{name: "Succeeded阶段", pod: podWithPhase(corev1.PodSucceeded), want: "Succeeded"},
		{name: "Unknown阶段", pod: podWithPhase(corev1.PodUnknown), want: "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getPodStatus(tt.pod); got != tt.want {
				t.Errorf("getPodStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsPodStatusAbnormal(t *testing.T) {
	for _, s := range []string{"Running", "ContainerCreating", "PodInitializing", "Completed", "Succeeded"} {
		if isPodStatusAbnormal(s) {
			t.Errorf("isPodStatusAbnormal(%q) = true, want false", s)
		}
	}
	for _, s := range []string{"Pending", "Failed", "CrashLoopBackOff", "ImagePullBackOff", "Unknown"} {
		if !isPodStatusAbnormal(s) {
			t.Errorf("isPodStatusAbnormal(%q) = false, want true", s)
		}
	}
}

func TestGetRestartCount(t *testing.T) {
	pod := podWithPhase(corev1.PodRunning,
		corev1.ContainerStatus{RestartCount: 3},
		corev1.ContainerStatus{RestartCount: 4},
	)
	if got := getRestartCount(pod); got != 7 {
		t.Errorf("getRestartCount() = %d, want 7", got)
	}
}

func TestGetAge(t *testing.T) {
	if got := getAge(time.Now().Add(-49 * time.Hour)); got != "2d1h" {
		t.Errorf("getAge() = %q, want %q", got, "2d1h")
	}
}

func TestAnalyzeFindings(t *testing.T) {
	ins := &Inspector{}
	report := &types.InspectionReport{
		Components: []types.ComponentStatus{
			{Name: "coredns", Status: "unhealthy (1/2)", Healthy: 1, Total: 2},
			{Name: "etcd", Status: "healthy", Healthy: 3, Total: 3},
		},
		Nodes: []types.NodeStatus{
			{Name: "node-1", Status: "NotReady"},
			{Name: "node-2", Status: "Ready", HasResourceWarning: true, ResourceAlerts: []string{"内存(85.0%)"}},
			{Name: "node-3", Status: "Ready"},
		},
		Pods: types.PodStatistics{
			AbnormalPodList: []types.PodStatus{
				{Name: "pod-failed", Status: "Failed"},
				{Name: "pod-pending", Status: "Pending"},
				{Name: "pod-crash", Status: "CrashLoopBackOff"},
			},
		},
	}

	findings := ins.AnalyzeFindings(report)

	if len(findings.CriticalComponents) != 1 || !strings.Contains(findings.CriticalComponents[0], "coredns") {
		t.Errorf("CriticalComponents = %v, want 仅含 coredns", findings.CriticalComponents)
	}
	if len(findings.CriticalNodes) != 1 || findings.CriticalNodes[0] != "node-1: NotReady" {
		t.Errorf("CriticalNodes = %v, want [node-1: NotReady]", findings.CriticalNodes)
	}
	if len(findings.WarningNodes) != 1 || !strings.Contains(findings.WarningNodes[0], "node-2") {
		t.Errorf("WarningNodes = %v, want 仅含 node-2", findings.WarningNodes)
	}
	if len(findings.FailedPods) != 1 || findings.FailedPods[0].Name != "pod-failed" {
		t.Errorf("FailedPods = %v, want 仅含 pod-failed", findings.FailedPods)
	}
	if len(findings.PendingPods) != 1 || findings.PendingPods[0].Name != "pod-pending" {
		t.Errorf("PendingPods = %v, want 仅含 pod-pending", findings.PendingPods)
	}
}
