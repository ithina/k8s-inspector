# 贡献指南

感谢你参与 k8s-inspector 的建设！本文档说明本地开发、测试与提交规范。

## 环境要求

- Go 1.23+
- 可访问的 Kubernetes 集群（可选，仅端到端验证时需要）

## 本地开发

```bash
# 依赖下载
go mod download

# 构建
go build ./...

# 运行全部测试（CI 中带 -race）
go test -race ./...

# 代码格式与静态检查
gofmt -l .
go vet ./...
```

## 提交规范

- 提交信息建议使用 Conventional Commits 风格：`feat:`、`fix:`、`docs:`、`refactor:`、`test:`、`ci:`、`chore:`
- 每个 PR 尽量只做一件事，保持变更集可审阅
- 新增或修改行为时请同步补充/更新单元测试
- 涉及配置项、CLI 标志、部署清单的变更，请同步更新 README 与 deploy 目录

## Pull Request 流程

1. Fork 仓库并创建分支（如 `feat/node-disk-alert`）
2. 完成开发与自测，确保 `gofmt -l .` 无输出、`go vet ./...` 与 `go test ./...` 通过
3. 提交 PR，填写 PR 模板中的自检清单
4. 等待 CI 通过与维护者 Review

## 安全注意

- 不要在 Issue / PR / 测试数据中提交真实的集群凭证、企业微信 Webhook、Dify API Key 或内网地址
- 安全问题请按 [SECURITY.md](SECURITY.md) 私下报告
