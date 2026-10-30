# Go 配置中心

`config-center` 是主配置中心工程，使用 Go 1.27.3、Chi、MySQL 8.0+，对外提供 `/api/config/**`、三级配置树 + 游戏级渠道应用绑定和 `epoch + revision` 同步协议。

## 代码目录

- `cmd/config-center`：HTTP 服务启动入口。
- `cmd/migrate`：独立的 MySQL 迁移 CLI。
- `internal/migration/sql`：内置版本 V1–V9 的权威数据库迁移 SQL。
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

启动时自动检查并更新数据库结构，首次空库执行 V1→V9。也可在服务启动前单独运行：

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

只给大区（`nodeType=region`）增加 `envType`：`test`（测试服）、`review`（提审服）、`prod`（正式服）。
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
- 渠道应用及绑定完全独立管理，CRUD 不修改节点同步版本、不记录节点变更日志、不触发 Webhook。

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
V8 起同步快照不再包含 `channelApps` 和 `bindings`，接入端应与配置中心协调升级。

## V7：移除冗余祖先列

V7 删除 `config_node.server_group_id` 及相关索引，重建同级唯一索引并更新 `epoch`；
保留 `config_channel_app.game_id` 和 `config_channel_app_binding.server_group_id`。
先确保 V6 完成、备份数据库并暂停旧版写入实例；已部署的接入端副本需先执行
`config-center-access/db/migration/V5__trim_node_group_id_replica.sql`，
然后升级两个 Go 服务并执行：

```powershell
go run ./cmd/migrate -config configs/config-dev.yaml
```

迁移失败时检查 `config_schema_migrations.dirty`，不要直接重跑部分成功的 DDL。

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

## V8 仅同步三级节点

- 只有 `config_node` 的 `game → region → server_group` 变更才推进 `revision` 并触发 Webhook。
- `/api/config/sync/snapshot` 只包含 `epoch`、`revision`、`nodes`。
- 渠道应用与绑定保留原来的 CRUD 和生效 JSON 合并逻辑，不参加节点同步。
- V8 迁移只切换一次 `epoch`，不删除业务表；新版接入端自动重建纯节点快照。
- 上线前备份并协调两个 Go 服务升级。旧接入库的渠道副本表不会自动删除，确认无依赖后手动清理。

## V9：同步表更名与历史归档清理

- `config_change_log` → `config_node_change_log`。
- `config_delivery_state` → `config_subscriber_delivery_state`。
- V9 为这两张表及其他六张正式表添加中文表说明；不会改变任何字段、数据、同步协议或 `epoch/revision`。
- 不修改 V1–V8 的历史迁移文件；Go 查询、sqlc、Webhook 调度和集成测试全部使用新名称。
- V9 以后 V4/V6 归档只用于历史审计，不作为启动校验的必需表。**升级不会自动删除任何归档数据。**

升级前停止旧版配置中心写入并备份数据库，然后运行：

```powershell
go run ./cmd/migrate -config configs/config-dev.yaml
```

升级后核对：

```sql
SELECT version, dirty FROM config_schema_migrations WHERE id = 1;
SHOW TABLES LIKE 'config_node_change_log';
SHOW TABLES LIKE 'config_subscriber_delivery_state';
```

如需从当前业务库移除 `config_node_v6_channel_app_archive`，应先使用 `mysqldump` 单独备份并检查备份完整性，在 V9 成功且业务数据核对无误后才手工执行 `DROP TABLE`。旧版配置中心不得再连接升级后的数据库。具体操作参见 `docs/database-cleanup.md`。

## V10：正式服环境值统一为 prod

- 大区环境类型统一为 `test` / `review` / `prod`，管理端、查询接口和同步协议返回新值；`production` 不再是合法输入。
- 保留 V1–V9 历史迁移 SQL；新 V10 将已有 `config_node.env_type='production'` 转为 `prod`、收紧 CHECK 约束，并切换一次同步 `epoch`（不改变 `revision`）。
- **升级顺序：** 停止配置中心和接入端 → 备份权威库和所有接入副本库 → 在已存在的接入库中手动执行 `config-center-access/db/migration/V6__rename_region_env_prod_replica.sql` → 部署新版二进制 → 对配置中心执行 `go run ./cmd/migrate -config configs/config-dev.yaml` → 重启各服务。
- 调用方将原 `envType=production` 改为 `envType=prod`。这次仅修改环境枚举，未新增 `/api/config/{env}/{appId}` 路由。

迁移后检查：

```sql
SELECT version,dirty FROM config_schema_migrations WHERE id=1;
SELECT env_type,COUNT(*) FROM config_node WHERE node_type='region' GROUP BY env_type;
```

MySQL DDL 不能整体事务回滚；V10 中断时不得直接清除 dirty 状态，应检查实际表结构并通过备份人工恢复。
