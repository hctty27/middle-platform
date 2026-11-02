# 数据库结构与迁移

**以实际 SQL 为准**：[权威库基线](../config-center/internal/migration/sql/schema.sql) · [接入副本基线](../config-center-access/internal/store/sql/schema.sql)。本文描述当前结构和安全操作，不作为历史版本逐条升级指南。

## 权威配置库：config_center

| 表 | 作用 |
| --- | --- |
| `config_node` | `game / region / server_group` 三级节点；`game_id`、`region_id` 表示祖先，`region.env_type` 区分环境 |
| `config_channel_app` | 独立渠道应用，`code` 全局唯一，包含 `game_id`、共享 `config` 以及三环境区服组绑定 |
| `config_revision` | 单行 `epoch` + `revision` 同步游标 |
| `config_node_change_log` | 按 revision 记录三级节点 UPSERT / DELETE |
| `config_subscriber` | Webhook 订阅者、回调地址、签名密钥 |
| `config_subscriber_delivery_state` | 回调确认进度、租约与失败重试 |
| `config_schema_migrations` | 迁移版本、`dirty` 状态，由迁移器维护 |

### 关键约束

- `config_node.node_type` 仅支持 `game` / `region` / `server_group`。大区 `env_type` 仅支持 `test` / `review` / `prod`，旧数据可为 null。
- 节点不保存 `parent_id`；根据节点类型使用 `game_id` 与 `region_id`。`config` 为 MySQL JSON。
- `config_channel_app.code` **在所有游戏之间全局唯一**。`prod_server_group_id`、`test_server_group_id`、`review_server_group_id` 各为可空外键，一个 AppID 每环境至多一个区服组；绑定的环境与游戏归属还必须通过服务端校验。
- 渠道应用仅有一份共享 `config`；已经移除历史 `config_channel_app_binding` 运行态表。
- 只有三级节点变更会推进同步 revision；渠道应用变更不会被接入端的节点增量同步传播。

## 权威库初始化（仅新建空库）

要求 MySQL 8.0+。执行独立命令 `cmd/migrate`，当前**基线版本是 V11**：

```sql
CREATE DATABASE IF NOT EXISTS config_center
  CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
```

```powershell
cd config-center
Copy-Item configs/config.yaml configs/config-dev.yaml
# 在 configs/config-dev.yaml 中填写 database.dsn
go run ./cmd/migrate -config configs/config-dev.yaml
go run ./cmd/config-center -config configs/config-dev.yaml
```

校验：

```sql
SELECT version, dirty
FROM config_schema_migrations
WHERE id = 1;
```

初始化成功应为 `version=11`、`dirty=0`。应用启动只读验证结构，**不执行 DDL**。集群部署期间迁移仅执行一次，不能让各实例在启动时并发迁移。

## 已有库与后续升级

- **已有 V11 库：** 保留迁移状态及业务数据，运行当前迁移 CLI 可做幂等校验。
- **已有 V1～V10 权威库：** 当前 CLI 不支持直接跨越基线升级。先备份，再使用基线重构前的旧版迁移工具升级至 V11，确认成功后更换新程序。
- **非空但没有迁移记录：** 新迁移器会拒绝“认领”该库。不得删除表、伪造版本、修改 dirty 值来绕过检查。
- **未来 V12+：** 在 `config-center/internal/migration/sql/` 增加增量 SQL，并更新 `migration.go` 中的版本定义和升级注册；升级前执行备份、停写与测试。

MySQL DDL 不保证事务回滚。遇到 `dirty=1` 或中途失败，先核对已执行的 DDL 与备份，恢复到一致状态，再由人工确认后重试。**不要**直接删除 `config_schema_migrations` 或手工执行新库 `schema.sql` 覆盖已有库。

## 接入端副本：config_access

该库和权威库**必须分开**，只有 `config_node` 与 `config_sync_state` 两张运行态表；副本基线为 **V6**，不存储渠道应用或订阅者。

- 首次启动连接**完全空的** `config_access` 库时，接入端可自动建立副本表并从权威库拉取全量快照。
- 已存在符合 V6 结构的副本只做结构检查，增量游标保留。
- 旧 V1～V5 副本必须先用历史迁移脚本升级；如果明确不需要旧缓存，可以在备份并确认影响后**另建空的副本库**重新同步，切勿直接对有数据的库运行基线 SQL。

## 历史归档与清理

历史 V4/V6 归档表可能仍在旧库中，不属于 V11 新库基线。缺失这些归档表不应影响现行服务，但其数据可能具有审计价值。是否删除需先单独备份、验证可恢复与行数，再由管理员人工清理；迁移器不得自动清除历史归档。

生产建议：权威库定期备份并演练恢复，SDK Core 使用最小只读权限账号；数据库结构迁移与可执行程序回滚必须分别设计。
