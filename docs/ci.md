# CI：纯 Go + MySQL + Vue

所有 PR 和 main push 由 `.github/workflows/build.yml` 执行：

| Job | 校验 |
| --- | --- |
| `Go config center` | Go 单测、race、vet、build、sqlc 生成结果、MySQL 数据访问 |
| `Go access` | 接入端 Go 单测、race、vet、build |
| `Vue admin` | npm ci 与 Vue 构建 |
| `Go MySQL migration + two instances + Webhook E2E` | Go V1–V4 实际迁移、双实例、节点变更、Webhook 验签、同步补偿 |
| `CI Gate` | 要求上述四项全部成功 |

数据库回归通过 `scripts/ci_migration_smoke.py` 和 `scripts/ci_smoke.py` 运行，不需要 JDK、Gradle 或其它服务端语言。

生产分支保护建议只配置 `CI Gate` 为必需检查。迁移 SQL 更新必须验证空库初始化、旧节点归档、epoch 切换和重复迁移的幂等性。

本地验证：

```bash
cd config-center
go test -race ./...
go vet ./...
go build ./cmd/config-center ./cmd/migrate
cd ../config-center-access
go test -race ./...
go vet ./...
```
