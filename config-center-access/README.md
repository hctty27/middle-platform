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

> 如果配置中心使用默认 `:8081`，需将其中一个进程改为不同端口，例如配置中心 `CONFIG_GO_ADDR=:8080`。

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

新建库会自动创建包含大区环境类型的最新三级 `config_node` 和 `config_sync_state` 表；已有 V3 数据库须先后手动执行 `db/migration/V4__region_env_type_replica.sql`、`db/migration/V5__trim_node_group_id_replica.sql`、`db/migration/V6__rename_region_env_prod_replica.sql`，不会重置已有游标。

若副本库尚停留在历史 V1/V2 五级结构，**必须先备份并手工执行缺失的 V2/V3 SQL 迁移**，再启动 Go 接入服务。原始 SQL 保留在 `db/migration`，不会由接入进程自动执行；已升级的 V3 库先后执行 V4 环境类型扩展及 V5 冗余列清理，不需要重建。Go 服务不会自动删除旧表或历史数据。

## 验证

```bash
go test -race ./...
go vet ./...
go build ./cmd/config-center-access
```

## 大区环境同步

`GET /api/access/nodes` 返回 `env_type`（`test` / `review` / `prod`）字段。
历史未分类大区返回 `null`；归类后通过权威端版本事件同步更新。
已使用 V3 接入副本数据库的部署，请备份并依次执行 V4、V5 SQL。

## YAML 本地启动

复制 `configs/config.yaml` 为 `configs/config-dev.yaml`，填写 `database.dsn` 和 `webhook.secret`，然后执行：

```powershell
go run ./cmd/config-center-access -config configs/config-dev.yaml
```

通用配置在 `configs/config.yaml`，开发/生产配置与通用配置合并；环境变量优先级最高。省略 `-config` 时保留旧的环境变量启动方式。
生产配置同样从 `configs/config.yaml` 复制为 `configs/config-prod.yaml`；建议通过 `ACCESS_DB_DSN` 和 `CONFIG_WEBHOOK_SECRET` 注入生产密钥。实际的 `config-dev.yaml`、`config-prod.yaml` 已加入 `.gitignore`。

## V8 三级节点同步

- 仅同步 `game`、`region`、`server_group` 三级节点与 `epoch + revision` 游标。
- 初次启动、epoch 变化、日志断档时读取 `snapshot.nodes`；增量同步继续按 revision 拉取节点变更 ID。
- 新副本库只建立 `config_node` 和 `config_sync_state` 两张表，不再建渠道应用和绑定副本表。
- 已存在的渠道副本表不会被自动删除，但不再写入或读取；请确认业务无依赖后备份并自行清理。
- 需要渠道应用配置的服务应使用配置中心独立的渠道应用查询接口，不可依赖遗留副本。
- V8 迁移切换一次 epoch，让新版接入工程重建三级节点副本。

## V5 副本结构（适配权威库 V7）

`config_node` 仅保留 `game_id` 与 `region_id` 两个祖先列，`config_channel_app_binding.server_group_id` 继续保留。
已有副本库先停接入服务、备份并手工执行 `db/migration/V5__trim_node_group_id_replica.sql`，再启动新版。
若旧库未升级，启动时会报错提示必须执行 V5，防止新旧 SQL 混用。

## V6：正式服环境从 production 调整为 prod

新版副本只接受 `test`、`review`、`prod`。历史副本的数据与 CHECK 约束都必须更新，不能只替换 Go 字符串。

停止接入服务并备份本地副本，在已完成 V5 的库上执行：

```powershell
mysql -u root -p config_access < db/migration/V6__rename_region_env_prod_replica.sql
```

该脚本不删除节点、不重置同步游标。新建空副本无需执行此 SQL。权威配置中心 V10 切换 `epoch` 后，接入端会自动重新拉取三级节点快照。若旧 CHECK 约束仍在，新接入服务会拒绝启动并提示先执行 V6。
