<div align="center">

# K8s Inspector

### Kubernetes 集群智能巡检系统

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-Compatible-326CE5?logo=kubernetes&logoColor=white)](https://kubernetes.io)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

自动化 Kubernetes 集群巡检工具，集成 Prometheus 指标采集、Dify AI 智能分析和企业微信通知，提供集群健康度全景报告。

</div>

---

## 目录

- [概述](#概述)
- [核心功能](#核心功能)
- [架构设计](#架构设计)
- [快速开始](#快速开始)
- [配置说明](#配置说明)
- [部署指南](#部署指南)
- [开发指南](#开发指南)
- [技术栈](#技术栈)

## 概述

K8s Inspector 是一个面向 Kubernetes 集群的自动化巡检工具，通过采集集群节点、Pod、核心组件的运行状态和资源指标，生成可视化 HTML 报告，并通过 AI 引擎提供智能优化建议。支持定时巡检、分级告警和多渠道通知。

## 核心功能

### 巡检能力

| 模块 | 功能 | 数据来源 |
|------|------|----------|
| 节点巡检 | CPU/Memory/Disk 使用率、PID 压力、Pod 密度 | Kubernetes API + Prometheus |
| Pod 巡检 | 生命周期状态、重启次数、异常检测 | Kubernetes API |
| 组件巡检 | kube-apiserver、etcd、coredns 等核心组件健康度 | Kubernetes API |
| 容量分析 | 集群平均 Pod 密度、高/低内存节点分布 | Prometheus |

### AI 智能分析

- 异常日志根因分析
- 资源瓶颈智能定位
- 容量优化建议生成
- 自然语言巡检摘要

### 通知与报告

- HTML 可视化报告（含异常高亮、资源趋势）
- 企业微信 Markdown 摘要推送（自动截断、分级告警）
- Dify AI 优化建议嵌入

## 架构设计

```
┌─────────────────────────────────────────────────────────┐
│                    Entry Point                          │
│                 cmd/inspector/main.go                   │
└──────────────────────────┬──────────────────────────────┘
                           │
         ┌─────────────────┼─────────────────┐
         ▼                 ▼                 ▼
┌─────────────┐  ┌──────────────┐  ┌──────────────┐
│  Inspector  │  │   Config     │  │   Dify AI    │
│  Service    │  │   Manager    │  │   Client     │
│             │  │              │  │              │
│ CheckNodes  │  │ Env Vars     │  │ Stream Parse │
│ CheckPods   │  │ Validation   │  │ Optimization │
│ CheckComps  │  │ Defaults     │  │ Suggestions  │
└──────┬──────┘  └──────────────┘  └──────────────┘
       │
       ▼
┌─────────────┐  ┌──────────────┐  ┌──────────────┐
│  Kubernetes │  │  Prometheus  │  │   WeChat     │
│  Client     │  │   Client     │  │   Notifier   │
│             │  │              │  │              │
│ Nodes       │  │ CPU/Mem      │  │ Markdown     │
│ Pods        │  │ Disk         │  │ 4096 Limit   │
│ Components  │  │ Pod Density  │  │ Auto Trunc   │
└─────────────┘  └──────────────┘  └──────────────┘
                       │
                       ▼
              ┌──────────────┐
              │  Report Gen  │
              │  (HTML)      │
              └──────────────┘
```

### 数据流

```
K8s API ─┐
Prometheus ─┤──► Inspector ──► Report ──► HTML Output
Dify AI  ─┤──► (Merge)    └──► WeChat Push
Config   ─┘
```

## 快速开始

### 前置条件

- Go 1.23+
- 可访问的 Kubernetes 集群（`~/.kube/config` 或 InCluster 模式）
- Prometheus 监控服务（可选）
- Dify AI 服务（可选）
- 企业微信机器人（可选）

### 本地运行

```bash
# 克隆仓库
git clone https://github.com/ithina/k8s-inspector.git
cd k8s-inspector

# 设置环境变量
export CLUSTER_NAME="kubernetes"
export REPORT_CLUSTER_NAME="K8s测试集群"
export PROMETHEUS_URL="http://prometheus-k8s-monitoring-apps-sit.xx"
export DIFY_API_KEY="app-xxx"
export DIFY_BASE_URL="http://k8s-dify-uat.xx/v1/chat-messages"
export WECHAT_WEBHOOK="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"

# 运行巡检
go run ./cmd/inspector/main.go
```

### Docker 运行

```bash
docker build -t k8s-inspector .
docker run --rm \
  -e CLUSTER_NAME=kubernetes \
  -e PROMETHEUS_URL=http://prometheus:9090 \
  -v ~/.kube/config:/home/vmuser/.kube/config \
  -v /app/reports:/app/reports \
  k8s-inspector
```

## 配置说明

所有配置通过环境变量注入，支持可选模块降级运行。

### 核心配置

| 变量 | 必填 | 默认值 | 说明 |
|------|:----:|--------|------|
| `CLUSTER_NAME` | 是 | — | 集群标识 |
| `REPORT_CLUSTER_NAME` | 否 | 同 `CLUSTER_NAME` | 报告显示名称 |
| `REPORT_OUTPUT_DIR` | 否 | `/app/reports` | HTML 报告输出目录 |
| `MAX_CONCURRENCY` | 否 | `10` | 并发巡检数（1-50） |
| `CRITICAL_THRESHOLD` | 否 | `90` | 严重告警阈值（%） |
| `WARNING_THRESHOLD` | 否 | `80` | 警告阈值（%） |
| `DIFY_TIMEOUT` | 否 | `60s` | AI 分析超时（10s-300s） |

### 可选集成

| 变量 | 默认行为 | 说明 |
|------|----------|------|
| `PROMETHEUS_URL` | 跳过指标采集 | Prometheus 查询地址 |
| `DIFY_API_KEY` + `DIFY_BASE_URL` | 生成基础报告 | Dify AI 服务配置 |
| `WECHAT_WEBHOOK` | 输出到控制台 | 企业微信机器人地址 |

> 未配置可选模块时，系统自动降级运行，不报错。

## 部署指南

### Kubernetes CronJob 部署

```bash
kubectl apply -f deploy/kubernetes/cronjob.yaml
```

默认每天 09:30 和 18:30 执行巡检，通过 K8s Secret 注入敏感配置：

```bash
kubectl create secret generic dify-key \
  --from-literal=api_key='app-xxx' -n k8s

kubectl create secret generic dify-url \
  --from-literal=base_url='http://k8s-dify-uat.xx/v1/chat-messages' -n k8s
```

### 命名空间过滤

系统默认跳过以下命名空间的 Pod 巡检：
- `k8s`（巡检工具自身命名空间）
- `csi-cephfs`（存储插件命名空间）

如需调整，修改 [inspector.go](pkg/service/inspector/inspector.go) 中的 `excludeNamespaces`。

## 开发指南

### 项目结构

```
k8s-inspector/
├── cmd/
│   └── inspector/         # 主程序入口
├── deploy/
│   └── kubernetes/        # K8s 部署清单
├── docs/
│   └── ARCHITECTURE.md    # 架构文档
├── pkg/
│   ├── api/
│   │   ├── dify/          # Dify AI 客户端（流式解析）
│   │   └── prometheus/    # Prometheus 查询客户端
│   ├── config/            # 环境变量配置管理
│   ├── notify/
│   │   └── wechat/        # 企业微信通知
│   ├── service/
│   │   ├── inspector/     # 巡检核心逻辑
│   │   └── report/        # 报告生成与发送
│   └── types/             # 共享数据模型
├── templates/
│   └── report.html        # HTML 报告模板
├── Dockerfile             # 多阶段容器构建
├── go.mod
└── go.sum
```

### 本地开发

```bash
# 依赖下载
go mod download

# 构建
go build -o bin/inspector ./cmd/inspector

# 测试
go test ./...

# 代码检查
go vet ./...
go fmt ./...
```

### 测试报告生成

```bash
# 使用 mock 数据生成测试报告
go run ./cmd/full-test/main.go
```

## 技术栈

| 组件 | 技术 | 版本 |
|------|------|------|
| 语言 | Go | 1.23+ |
| K8s 客户端 | client-go | v0.32.3 |
| 指标采集 | Prometheus Client | v1.22.0 |
| 日志 | Zap | v1.27.0 |
| AI 分析 | Dify API | Streaming SSE |
| 容器 | Docker Multi-stage | Alpine 3.19 |

## License

[MIT](LICENSE)