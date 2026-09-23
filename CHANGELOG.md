# Changelog

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 与 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

## [1.0.0] - 2026-09-23

### Added

- 新增 CLI 标志：`--once`（单次执行）、`--interval`（循环巡检间隔，默认 1h）、`--version`（版本信息）
- 新增 `EXCLUDE_NAMESPACES` 环境变量：逗号分隔配置需跳过 Pod 巡检的命名空间
- 新增 `REPORT_BASE_URL` 环境变量：通知消息中生成完整报告访问链接
- 新增单元测试：config、inspector、report、dify 流式解析、CLI 截断逻辑
- 新增 GitHub Actions CI（gofmt / vet / build / test -race / Docker 构建）
- 新增开源治理文档：CONTRIBUTING、SECURITY、CHANGELOG、Issue / PR 模板

### Changed

- 模块路径更新为 `github.com/ithina/k8s-inspector`
- HTML 报告模板改为 `go:embed` 内置，二进制不再依赖外部模板文件
- Prometheus 改为可选依赖：未配置 `PROMETHEUS_URL` 时降级运行（跳过指标采集）
- 命名空间排除列表从硬编码改为 `EXCLUDE_NAMESPACES` 配置
- 支持 SIGINT/SIGTERM 优雅退出，单次执行整体超时保护（30 分钟）
- 部署清单重构：独立 Namespace、最小权限 RBAC、PVC 持久化报告、Secret 可选注入
- 容量统计逻辑从入口下沉至 report 包，便于测试与复用

### Fixed

- 企业微信消息按 UTF-8 字符边界截断，避免中文等多字节字符截断后乱码
- 修复重试循环中 `defer` 延迟释放响应体导致的连接占用
- 企业微信超长消息报错单位由「字符」修正为「字节」
- 修复流式解析正则误用字面量 `\\n`/`\\s`（反斜杠文本）导致清理逻辑失效：多余空行压缩与 Markdown 引用行删除现按真实换行生效，且不再误删「优化建议：」内容
- 建议段提取的尾部清理同时去除空白与标点，句末标点后跟空白即可构成完整段落边界
- 校准流式解析测试用例，修复既有失败测试

### Security

- 清理文档与部署清单中的内网地址、内部命名空间等敏感信息
