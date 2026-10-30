# Go 配置中心

`config-center` 是主配置中心工程，使用 Go 1.27.3、Chi、MySQL 8.0+，对外提供 `/api/config/**`、三级配置树 + 游戏级渠道应用绑定和 `epoch + revision` 同步协议。

## 代码目录

- `cmd/config-center`：HTTP 服务启动入口。
- `cmd/migrate`：独立的 MySQL 迁移 CLI。
- `internal/migration/sql`：内置版本 V1–V6 的权威数据库迁移 SQL。
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

启动时自动检查并更新数据库结构，首次空库执行 V1→V6。也可在服务启动前单独运行：

```bash
go run ./cmd/migrate
```

## 旧数据库安全升级

- 如果数据库已有 V4 版本的历史迁移记录，会识别并追加非破坏性的 V5，不重复创建节点表。
- 对历史 V1–V3，程序默认拒绝自动升级，因为 V4 会删除旧渠道层级；先停写、备份并核对重复 Code。
- 确认后使用 `go run ./cmd/migrate -allow-legacy-upgrade`（或启动服务时设置 `CONFIG_ALLOW_LEGACY_UPGRADE=true`）。
- MySQL DDL 不是完整事务，迁移失败会标记 `config_schema_migrations.dirty=1`；必须先核查和恢复数据库，不允许自动重跑半完成的 DDL。
- 新状态记录在 `config_schema_migrations`，不会修改历史迁移记录。对于已有 V4 的数据库，会验证四级节点列、环境类型字段、订阅 epoch 字段和归档表。
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

CI 会用 MySQL 8.4 检验全新建库、V1→V5 迁移、历史配置保留、双实例同步和 Go 接入端 Webhook。

## 大区环境类型

只给大区（`nodeType=region`）增加 `envType`：`test`（测试服）、`review`（提审服）、`production`（正式服）。
新建大区必须选择类型，其他层级不能设置；已归类大区不能直接切换环境类型。
已有大区迁移时保持 `NULL`（未分类），需要逐个核对用途并在管理端首次归类。
迁移 V5 更新 `epoch`，通知接入端重新获取带环境类型的完整快照，保留现有 revision。

## YAML 本地启动

从 `configs/config.yaml` 复制为 `configs/config-dev.yaml`，填写 `database.dsn`，本地需要管理写入和回调时将 `admin.writesEnabled` 与 `webhook.enabled` 改为 `true`，然后执行：

```powershell
go run ./cmd/config-center -config configs/config-dev.yaml
```

数据库迁移可执行 `go run ./cmd/migrate -config configs/config-dev.yaml`。
配置优先级：程序默认值 → 同目录 `config.yaml` → 指定 YAML → 环境变量。不加 `-config` 时继续使用旧的环境变量方式。生产同样从 `configs/config.yaml` 复制成 `configs/config-prod.yaml`，生产数据库凭证推荐通过 `CONFIG_DB_DSN` 注入，不提交实际配置文件。

## 渠道应用与环境绑定（V6）

- 配置树固定三层：`game → region → server_group`。
- `config_channel_app`：按 `game_id + code` 唯一的渠道应用公共定义，支持状态与公共 JSON。
- `config_channel_app_binding`：按 `channel_app_id + server_group_id` 唯一；每个绑定独立控制状态并维护覆盖 JSON，禁止跨游戏绑定。
- 公共配置与绑定覆盖配置按 JSON 对象的同名键合并，覆盖层优先；`null` 用于删除公共字段。
- 应用或绑定变更时原子更新 `epoch + revision`，接入端收到通知后重建快照，副本保存公共定义及绑定生效配置。

### 管理 API

```text
GET    /api/config/admin/channel-apps?gameId=1
POST   /api/config/admin/channel-apps
PUT    /api/config/admin/channel-apps/{id}
DELETE /api/config/admin/channel-apps/{id}
GET    /api/config/admin/channel-apps/{id}/bindings
POST   /api/config/admin/channel-apps/{id}/bindings
PUT    /api/config/admin/channel-app-bindings/{id}
DELETE /api/config/admin/channel-app-bindings/{id}
```

### 升级注意

**V6 非向后兼容。** 上线前必须备份配置库、停写并协调接入端和配置中心升级。
迁移会合并同一游戏下相同 `channel_app.code`，并把旧区服组的完整 JSON 原样迁入绑定覆盖层，公共 JSON 初始为 `{}`，不会覆盖提审服的旧配置。
原始渠道节点保存在 `config_node_v6_channel_app_archive`，不得自动删除。
旧版接入端不认识快照中的 `channelApps` 和 `bindings`，必须一起升级。

## V6 GTID 1786 失败恢复

V6 早期版本曾使用 `CREATE TABLE ... SELECT`，在部分启用 GTID 一致性检查的 MySQL 上会报 `1786`，
并在 `config_schema_migrations` 留下 `version=5, dirty=1`。当前 SQL 已拆分为 `CREATE TABLE ... LIKE` 与 `INSERT ... SELECT`。

**不要直接把 dirty 设置为 0，也不要直接删除旧 `config_node` 或已归档数据。**
确保数据库已完整备份、关闭所有配置中心写入实例，在更新至包含本修复的代码后，先确认数据库连接的是预期环境：

```sql
SELECT DATABASE(), @@version, @@global.gtid_mode, @@global.enforce_gtid_consistency;
SELECT version,dirty FROM config_schema_migrations WHERE id=1;
```

若失败发生在创建归档表之前，执行：

```powershell
go run ./cmd/migrate -config configs/config-dev.yaml -repair-v6-gtid
```

此显式恢复参数在独占迁移锁下，只针对 `version=5,dirty=1` 且 `config_node` 仍是 V5、
归档表不存在、两张新增表均没有数据的情况，清理这两张空表与 dirty 标志，随后继续执行 V6。
任一条件不满足则停止，不清理数据，需要人工核查。该参数只用于此次确定的 1786 中断场景；
它不是通用的数据库修复工具，正常迁移不应使用。生产环境同理，但必须先完成备份并按生产配置执行。

## 已完成 V6，但旧 V4 归档缺失（1146）

如果 V6 完成执行后最终检查报错 `Table ... config_node_v4_legacy_archive doesn't exist`，
这说明旧历史归档表可能在历史 Flyway 基线或既有库中未创建，**不能据此认定 V6 失败**。
V4 归档仅供旧渠道数据审计，运行中的权威应用、绑定与同步并不读取该表。
校验程序现在将其视为可选项目：不存在时记录警告，仍严格校验 V6 归档表及所有业务必需表。

首先查询：

```sql
SELECT version,dirty FROM config_schema_migrations WHERE id=1;
SELECT table_name FROM information_schema.tables WHERE table_schema=DATABASE()
AND table_name IN ('config_node_v4_legacy_archive','config_node_v6_channel_app_archive',
                   'config_channel_app','config_channel_app_binding');
```

若是 `version=6,dirty=0`，更新代码后直接重新运行常规迁移命令以校验结构：

```powershell
go run ./cmd/migrate -config configs/config-dev.yaml
```

**不要重复使用 `-repair-v6-gtid`**，它只处理 `version=5,dirty=1` 且 V6 尚未执行的安全中断场景；
也不要为通过校验而创建空的 V4 归档表，否则会误导历史数据审计。
V4 历史归档缺失不会被自动恢复，若业务仍需旧渠道原始数据，请从备份或原始库恢复。
