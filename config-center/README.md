# Go 配置中心

`config-center` 是主配置中心工程，使用 Go 1.27.3、Chi、MySQL 8.0+，对外提供 `/api/config/**`、四级配置树和 `epoch + revision` 同步协议。

## 代码目录

- `cmd/config-center`：HTTP 服务启动入口。
- `cmd/migrate`：独立的 MySQL 迁移 CLI。
- `internal/migration/sql`：内置版本 V1–V4 的权威数据库迁移 SQL。
- `internal/app`：数据库连接、迁移初始化、HTTP 服务、优雅退出。
- `internal/settings`：环境变量。
- `internal/api`：HTTP 接口。
- `internal/model`：业务模型。
- `internal/service`：节点规则。
- `internal/store`：MySQL 查询、写事务和变更日志。
- `internal/webhook`：多实例租约、签名、投递和 SSRF 防护。
- `internal/sqlc`、`internal/sqlcgen`：SQL 源文件和生成代码。

## 新建数据库

```sql
CREATE DATABASE config_center CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
```

```bash
cd config-center
export CONFIG_DB_DSN='root:password@tcp(127.0.0.1:3306)/config_center?parseTime=true&loc=UTC'
export CONFIG_GO_ADDR=':8080'
export CONFIG_GO_ENABLE_WRITES=true
export CONFIG_GO_ENABLE_WEBHOOK=true
go run ./cmd/config-center
```

启动时自动检查并更新数据库结构，首次空库执行 V1→V4。也可在服务启动前单独运行：

```bash
go run ./cmd/migrate
```

## 旧数据库安全升级

- 如果数据库已有 V4 版本的历史迁移记录，会安全识别旧状态并建立 Go 迁移游标，不重复创建节点表。
- 对历史 V1–V3，程序默认拒绝自动升级，因为 V4 会删除旧渠道层级；先停写、备份并核对重复 Code。
- 确认后使用 `go run ./cmd/migrate -allow-legacy-upgrade`（或启动服务时设置 `CONFIG_ALLOW_LEGACY_UPGRADE=true`）。
- MySQL DDL 不是完整事务，迁移失败会标记 `config_schema_migrations.dirty=1`；必须先核查和恢复数据库，不允许自动重跑半完成的 DDL。
- 新状态记录在 `config_schema_migrations`，不会修改历史迁移记录。对于已有 V4 的数据库，会验证四级节点列、订阅 epoch 字段和归档表。
- 升级所有配置中心实例前，请先备份并停止旧写入实例。

## 配置

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `CONFIG_DB_DSN` | 必填 | Go MySQL DSN |
| `CONFIG_GO_ADDR` | `:8081` | 服务监听地址（推荐改为 `:8080`） |
| `CONFIG_GO_ENABLE_WRITES` | `false` | 开启管理写入 |
| `CONFIG_GO_ENABLE_WEBHOOK` | `false` | 开启变更回调，必须同时开启写入 |
| `WEBHOOK_ALLOWED_HOSTS` | `localhost,127.0.0.1` | 回调目标主机白名单 |
| `CONFIG_ALLOW_LEGACY_UPGRADE` | `false` | 允许升级历史 V1–V3 数据库 |

## 检查

```bash
go test -race ./...
go vet ./...
go build ./cmd/config-center ./cmd/migrate
```

CI 会用 MySQL 8.4 检验全新建库、V1→V4 迁移、历史配置保留、双实例同步和 Go 接入端 Webhook。
