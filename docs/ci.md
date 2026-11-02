# CI 与回归测试

当前 GitHub Actions 包含三个独立工作流：

| 工作流 | 文件 | 主要验证 |
| --- | --- | --- |
| Build | `.github/workflows/build.yml` | 配置中心与接入端 Go 测试、race、vet、build；sqlc 生成文件；Vue 构建；MySQL 迁移、双实例与 Webhook E2E |
| sdk-core | `.github/workflows/sdk-core.yml` | SDK Core Go 测试、race、vet、build 与 MySQL 集成测试 |
| Package Linux | `.github/workflows/package.yml` | 统一 `package.ps1` 生成 Linux tar.gz，检查归档条目、ELF 架构与 Unix 0755 权限 |

Build 工作流的 `CI Gate` 要求 Go、接入端、Vue、迁移/集群回归均通过。`sdk-core` 与 `Package Linux` 为独立工作流；若配置分支保护，需检查实际状态检查名称和触发条件，不要误以为它们属于 `CI Gate`。

## 本地验证

分别在各 Go 模块目录执行：

```bash
go test -race ./...
go vet ./...
go build ./cmd/...
```

Vue 管理端：

```bash
cd config-center-admin
npm ci
npm run build
```

`shared/logging` 为独立 Go module，需单独测试。集成脚本：`scripts/ci_migration_smoke.py`（V11 数据库基线/历史库拒绝/GTID）和 `scripts/ci_smoke.py`（多实例、回调验签、节点同步）。这两个脚本用于 CI 测试环境，**不是生产数据库迁移工具，也不应在生产库上运行**。

## 文档与发布检查

文档改动至少确认相对链接有效；示例路径、API、环境变量和发布参数必须对照路由、配置模板与脚本。生产迁移还应检查空库初始化、历史库拒绝、迁移状态与业务数据安全；交付包遵循 [打包规范](package.md)。
