# Go 接入工程

`config-center-access` 是独立 Go 接入示例，不作为 SDK/Starter 给其他项目依赖。与 `config-center` 的 `/api/config/**` 完全兼容。

## 主要功能

- `POST /internal/config/callback`：检查五分钟时间窗口、HMAC-SHA256 签名后立即返回 202，在后台同步。
- `GET /api/access/nodes`、`GET /api/access/revision`：读取本地 MySQL 副本。
- `GET /health/live`、`GET /health/ready`：探活及数据库就绪探针。
- 初次启动读取一致性快照，以后按 revision 连续增量拉取；缺日志、epoch 变更则全量恢复。
- 同步游标与副本在一个事务提交；行锁校验防止多个接入进程覆盖新版本；重复回调合并处理。
- 每 5 分钟自动补偿一次，启动约 3 秒后首次同步。

## 本地启动（PowerShell）

要求 Go 1.27.3、MySQL 8.0+。先创建空数据库：

```sql
CREATE DATABASE config_access CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
```

```powershell
cd config-center-access
$env:ACCESS_DB_DSN = 'root:your-password@tcp(127.0.0.1:3306)/config_access?charset=utf8mb4&parseTime=true&loc=UTC'
$env:CONFIG_CENTER_URL = 'http://127.0.0.1:8080'
$env:CONFIG_ACCESS_ADDR = ':8081'
$env:CONFIG_WEBHOOK_SECRET = 'replace-with-a-strong-secret'
go mod download
go run ./cmd/config-center-access
```

> 如果 Go 配置中心使用默认 `:8081`，需将其中一个进程改为不同端口，例如配置中心 `CONFIG_GO_ADDR=:8080`。

## Linux/macOS 启动

```bash
export ACCESS_DB_DSN='root:password@tcp(127.0.0.1:3306)/config_access?charset=utf8mb4&parseTime=true&loc=UTC'
export CONFIG_CENTER_URL='http://127.0.0.1:8080'
export CONFIG_WEBHOOK_SECRET='replace-with-a-strong-secret'
go run ./cmd/config-center-access
```

创建订阅者（配置中心允许 localhost 回调）：

```bash
curl -X POST http://127.0.0.1:8080/api/config/admin/subscribers \
  -H 'Content-Type: application/json' \
  -d '{"code":"go-access","callbackUrl":"http://127.0.0.1:8081/internal/config/callback","secret":"replace-with-a-strong-secret"}'
```

订阅者的 `secret` 必须与接入工程 `CONFIG_WEBHOOK_SECRET` 一致。若跨主机回调，Go 配置中心需配置允许的回调主机，域名应使用 HTTPS；内部 Webhook 不经公共 API 网关暴露。

## 环境变量

| 环境变量 | 默认值 | 用途 |
|---|---|---|
| `ACCESS_DB_DSN` | 必填 | **接入方本地数据库**的 Go MySQL DSN，非 JDBC URL |
| `CONFIG_CENTER_URL` | `http://127.0.0.1:8080` | 配置中心根地址，不带 `/api/config` |
| `CONFIG_ACCESS_ADDR` | `:8081` | 本地接入服务监听地址 |
| `CONFIG_WEBHOOK_SECRET` | 必填 | 与订阅者一致的 HMAC 密钥 |
| `CONFIG_CLIENT_CONNECT_TIMEOUT_MS` | `2000` | 连接超时 |
| `CONFIG_CLIENT_READ_TIMEOUT_MS` | `15000` | 单次远程调用整体超时 |
| `CONFIG_RECONCILE_INTERVAL` | `5m` | 断线补偿校对频率 |

## 数据库升级边界

新建库会自动创建最新四级 `config_node` 和 `config_sync_state` 表；已有 Flyway V3 数据库可以原样接入，不重置游标。

若副本库尚停留在历史 V1/V2 五级结构，**必须先用旧版 Flyway 升级至 V3 并备份**，再切换 Go 接入服务。历史 SQL 存放在 `db/migration`，保留用于核查升级，不会被 Go 服务自动执行；不要直接改写已有 Flyway 版本。Go 服务不会自动删除旧表或历史数据。

## 验证

```bash
go test -race ./...
go vet ./...
go build ./cmd/config-center-access
```
