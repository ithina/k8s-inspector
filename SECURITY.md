# 安全策略

## 支持的版本

| 版本 | 支持状态 |
|------|----------|
| 最新 release / master 分支 | ✅ 支持 |
| 更早版本 | ❌ 不再维护 |

## 报告安全漏洞

请勿通过公开 Issue 披露安全漏洞。推荐通过 GitHub 私密报告渠道提交：

- 打开 [Security Advisories](https://github.com/ithina/k8s-inspector/security/advisories/new) 创建私密报告

报告中请尽量包含：

- 漏洞类型与影响范围
- 复现步骤或 PoC
- 受影响的版本 / 提交
- 可能的缓解措施

我们会在收到报告后尽快确认并回复处理计划。

## 部署安全建议

- 使用最小权限运行：CronJob 清单仅授予 `nodes` / `pods` / `namespaces` 的 `get`、`list` 权限
- 敏感配置（Webhook、API Key）通过 Kubernetes Secret 注入，不要写入镜像或代码
- 容器默认以非 root 用户（uid 700）运行，请勿随意放开该限制
