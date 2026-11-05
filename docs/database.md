# 数据库结构与手动 SQL

## 核心约定

**所有建表和升级都由管理员人工执行 SQL。** 四个 Go 服务不会在启动时执行 DDL；只校验实际表结构。SQL 随 Linux 发布包一起交付，也可从源码中获取：

| 场景 | 手工执行的 SQL |
| --- | --- |
| 新建空的 `config_center` | [config_center.sql](../db/full/config_center.sql) |
| 新建空的 `config_access` | [config_access.sql](../db/full/config_access.sql) |
| 已有库缺少订单表 | [V0001__add_game_order.sql](../db/incremental/V0001__add_game_order.sql) |
| 已有库缺少 SDK 服务表 | [V0002__add_sdk_services.sql](../db/incremental/V0002__add_sdk_services.sql) |
| 旧通用服务绑定表迁移（特定旧结构） | [V0003__migrate_sdk_service_channel.sql](../db/incremental/V0003__migrate_sdk_service_channel.sql) |
| 已有服务表缺少核验用户信息目录 | [V0004__add_sdk_user_check.sql](../db/incremental/V0004__add_sdk_user_check.sql) |
| 已有服务表缺少角色查询目录 | [V0005__add_sdk_role_query.sql](../db/incremental/V0005__add_sdk_role_query.sql) |
| 已有服务表缺少区服查询目录 | [V0006__add_sdk_server_query.sql](../db/incremental/V0006__add_sdk_server_query.sql) |
| 已有服务表缺少封禁与解禁目录 | [V0007__add_sdk_role_control.sql](../db/incremental/V0007__add_sdk_role_control.sql) |
| 已有服务表缺少订阅消息目录 | [V0008__add_sdk_subscribe_message.sql](../db/incremental/V0008__add_sdk_subscribe_message.sql) |
| 旧版本 SDK 专属配置迁移（需停服备份） | [V0009__migrate_sdk_node_config.sql](../db/incremental/V0009__migrate_sdk_node_config.sql) |
| SDK 迁移验证完成后的旧字段清理 | [V0010__cleanup_legacy_sdk_fields.sql](../db/incremental/V0010__cleanup_legacy_sdk_fields.sql) |
| 已有游戏节点缺少运营地区字段 | [V0011__add_game_market_type.sql](../db/incremental/V0011__add_game_market_type.sql) |

不得对已有配置数据的库再次执行 `db/full/` 中的全量 SQL。执行前备份，确认 MySQL 8.0+、目标库名称、当前表结构及 SQL 内容。初始化 SQL 不包含 `DROP TABLE`。已有库应先核对当前表定义、备份及 SQL 内容，切勿在结构不明的库上盲目执行初始化或增量 SQL。

## SQL 目录与增量编号

- 根目录 `db/full/`：`config_center.sql` 与 `config_access.sql` 是**当前完整结构快照**，仅用于新建对应空库，不包含生产业务数据。
- 根目录 `db/incremental/`：文件命名为 `V0001__description.sql`，序号全局连续递增，历史存量脚本为 **V0001～V0011**；下一份增量是 **V0012**。跨数据库时在文件名中注明 `config_center` 或 `config_access`。
- **新增数据库变更时，创建新编号的增量 SQL，并同步更新对应的全量 SQL**，以及本文和 CI、发布相关检查。已经发布的增量 SQL 不允许修改、删除或重新编号。
- 新增增量 SQL 头部注明目标数据库、适用旧结构与前置条件；所有 SQL 仍由管理员手工检查并执行。
- **注意：V0001～V0011 是不同历史版本、不同场景的 SQL 归档编号，并非可全部连续执行的升级链。** 例如 `V0003` 只适用于仍有旧 `sdk_service_binding` / `scope_type` 的库，`V0009` 仅适用于旧渠道表结构。已升级的库不得重跑。
- 当前不引入 Flyway、自动执行或迁移版本记录表。**编号本身不能证明数据库已经应用过该变更**，执行前需检查真实表结构、数据和历史操作记录。

源码目录 `db/full/`、`db/incremental/` 在发布包中分别对应 `sql/full/`、`sql/incremental/`。原有 `config-center/internal/sqlc/` SQL 仅用于代码生成，不属于手动数据库部署脚本。

## 本地 MySQL 示例

```sql
CREATE DATABASE IF NOT EXISTS config_center
  CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE DATABASE IF NOT EXISTS config_access
  CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
```

**只对新建空库执行初始化：**

```bat
mysql -u root -p config_center < db/full/config_center.sql
mysql -u root -p config_access < db/full/config_access.sql
```

上面两行采用 shell 输入重定向，适用于 CMD/bash；PowerShell 可直接在 Navicat、DataGrip 等数据库工具中执行 SQL，或从 CMD 调用。

**已有 config_center 库且缺少订单表时，只执行：**

```sql
-- 先核对订单表是否已存在
SHOW TABLES FROM config_center LIKE 'game_order';
```

检查后在数据库工具中运行 `db/incremental/V0001__add_game_order.sql`。该 SQL 只创建 `game_order`，不会改动配置表及历史业务数据。若表已经存在，不要重复运行，应先对比字段、索引和数据。

执行后核对：

```sql
SHOW TABLES FROM config_center LIKE 'game_order';
SHOW CREATE TABLE config_center.game_order;
SELECT COUNT(*) FROM config_center.config_node;
```

## 权威库的业务表

- `config_node`：游戏、大区、区服组三级节点；`region.env_type` 为 `prod/test/review`
- `sdk_channel_app`：SDK Core 管理，全局唯一 AppID、共享 JSON 配置、三环境绑定
- `sdk_node_config`：SDK Core 管理，node_id 引用 `config_node.id`，游戏/大区/区服组的 SDK 专属 JSON
- `config_revision`、`config_node_change_log`：同步游标和变更日志
- `config_subscriber`、`config_subscriber_delivery_state`：Webhook 订阅者和投递状态
- `game_order`：SDK Core 充值订单、支付状态、通知租约和补偿
- `sdk_service`：SDK 功能目录与全局开关，新库初始化预置已实现的 `event_report`、`subscribe_message`、`user_check`、`role_query`、`server_query`、`role_control`，不包含 `scope_type`、`sort_order`
- `sdk_service_channel`：按 `service_id + channel_app_id + env` 唯一的渠道包服务绑定

`config_center` 的配置表与订单表使用同一个数据库。SDK Core 只使用一个 `database.dsn` 连接池；建议配置表只授予 SELECT、订单表授予 SELECT/INSERT/UPDATE，运行账号不授予 DDL。

## SDK 功能配置与数据安全

- `sdk_service_channel` 使用 `(service_id, channel_app_id, env)` 唯一约束，`env` 只能是 `prod/test/review`。
- `event_report`、`subscribe_message`、`user_check`、`role_query`、`server_query` 与 `role_control` 均要求服务全局开关、渠道包环境开关同时启用；无渠道环境绑定即默认关闭。
- `sdk_service_channel.config` 为 JSON 对象；当前事件上报不使用事件级规则，不持久化和自动重试。
- SDK Core 的管理接口为 `GET /api/core/admin/services`、`PUT /api/core/admin/services/{id}` 和渠道包 `/api/core/admin/channel-apps/{id}/services` 系列，详见 [管理接口](index.html#a-sdk-start)。
- 充值订单和发货通知不依赖功能开关。


- MySQL DDL 会隐式提交，失败时不能假定事务回滚。应检查实际执行到哪一条 SQL，再制定恢复步骤。
- 接入端副本 `config_access` 独立于权威库，只保存三级节点及同步游标。首次全量拉取由接入服务在表结构就绪后完成，不再自动建表。
- 生产环境继续控制 `/api/config/**` 的访问权限，保护 `gameSecret` 和其他原始配置密钥。


## SDK 配置拆分上线顺序

1. 在维护窗口停止旧版本 `config-center`、所有 `sdk-core-*`、`sdk-service` 与管理端写操作；备份 `config_center` 并核对真实字段。
2. **已有库**执行 `db/incremental/V0009__migrate_sdk_node_config.sql`。该脚本创建 `sdk_node_config`、复制已知的游戏/大区 SDK 字段，并将 `config_channel_app` 改名为 `sdk_channel_app`。保留旧 `config_node.config` JSON 供比较，禁止对已经迁移的库重复执行。
3. 对比游戏密钥、适配器、五类回调地址、渠道包数量/ID、环境绑定及功能开关，检查是否存在未归类的自定义 SDK 字段。确认没有遗漏后再升级二进制与前端。
4. 同时切换 `config-center`、全部 `sdk-core`、`sdk-service`，不能保留混合新旧版本；Nginx 新增对 `/api/core/admin/` 的保护。验证配置查询、订单登记/回调/发货、角色与区服查询、封解禁、事件与订阅消息。
5. 验证通过后，在单独维护窗口人工执行 `db/incremental/V0010__cleanup_legacy_sdk_fields.sql`。清理后校验配置快照和日志不再包含 SDK 密钥。不得通过自动启动逻辑执行 DDL。

**注意**：MySQL DDL 隐式提交，迁移脚本不是单事务；重命名表前应确认目标表不存在，失败时人工核对已执行步骤。仅对已知 SDK 字段自动搬迁，未知自定义键需人工识别及处理。密钥迁移不可输出明文到 CI 或审计日志。
