# 数据库结构与手动 SQL

## 核心约定

**所有建表和升级都由管理员人工执行 SQL。** 四个 Go 服务不会在启动时执行 DDL；只校验实际表结构。SQL 随 Linux 发布包一起交付，也可从源码中获取：

| 场景 | 手工执行的 SQL |
| --- | --- |
| 新建空的 `config_center` | [config-center/db/sql/initialize.sql](../config-center/db/sql/initialize.sql) |
| 已有权威库，只缺 `game_order` | [config-center/db/sql/add_game_order.sql](../config-center/db/sql/add_game_order.sql) |
| 已有权威库但尚无 SDK 服务表 | [config-center/db/sql/add_sdk_services.sql](../config-center/db/sql/add_sdk_services.sql) |
| 已有服务表但缺少用户核验功能目录 | [config-center/db/sql/add_sdk_user_check.sql](../config-center/db/sql/add_sdk_user_check.sql) |
| 新建空的 `config_access` | [config-center-access/db/sql/initialize.sql](../config-center-access/db/sql/initialize.sql) |

不得对已有配置数据的库再次执行 `initialize.sql`。执行前备份，确认 MySQL 8.0+、目标库名称、当前表结构及 SQL 内容。初始化 SQL 不包含 `DROP TABLE`。已有库应先核对当前表定义、备份及 SQL 内容，切勿在结构不明的库上盲目执行初始化或增量 SQL。

## 本地 MySQL 示例

```sql
CREATE DATABASE IF NOT EXISTS config_center
  CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE DATABASE IF NOT EXISTS config_access
  CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
```

**只对新建空库执行初始化：**

```bat
mysql -u root -p config_center < config-center/db/sql/initialize.sql
mysql -u root -p config_access < config-center-access/db/sql/initialize.sql
```

上面两行采用 shell 输入重定向，适用于 CMD/bash；PowerShell 可直接在 Navicat、DataGrip 等数据库工具中执行 SQL，或从 CMD 调用。

**已有 config_center 库且缺少订单表时，只执行：**

```sql
-- 先核对订单表是否已存在
SHOW TABLES FROM config_center LIKE 'game_order';
```

检查后在数据库工具中运行 `config-center/db/sql/add_game_order.sql`。该 SQL 只创建 `game_order`，不会改动配置表及历史业务数据。若表已经存在，不要重复运行，应先对比字段、索引和数据。

执行后核对：

```sql
SHOW TABLES FROM config_center LIKE 'game_order';
SHOW CREATE TABLE config_center.game_order;
SELECT COUNT(*) FROM config_center.config_node;
```

## 权威库的业务表

- `config_node`：游戏、大区、区服组三级节点；`region.env_type` 为 `prod/test/review`
- `config_channel_app`：全局唯一 AppID、共享 JSON 配置、三环境绑定
- `config_revision`、`config_node_change_log`：同步游标和变更日志
- `config_subscriber`、`config_subscriber_delivery_state`：Webhook 订阅者和投递状态
- `game_order`：SDK Core 充值订单、支付状态、通知租约和补偿
- `sdk_service`：SDK 功能目录与全局开关，新库初始化预置已实现的 `event_report`、`user_check`，不包含 `scope_type`、`sort_order`
- `sdk_service_channel`：按 `service_id + channel_app_id + env` 唯一的渠道包服务绑定

`config_center` 的配置表与订单表使用同一个数据库。SDK Core 只使用一个 `database.dsn` 连接池；建议配置表只授予 SELECT、订单表授予 SELECT/INSERT/UPDATE，运行账号不授予 DDL。

## SDK 功能配置与数据安全

- `sdk_service_channel` 使用 `(service_id, channel_app_id, env)` 唯一约束，`env` 只能是 `prod/test/review`。
- `event_report` 与 `user_check` 均要求服务全局开关、渠道包环境开关同时启用；无渠道环境绑定即默认关闭。
- `sdk_service_channel.config` 为 JSON 对象；当前事件上报不使用事件级规则，不持久化和自动重试。
- 配置中心的管理接口为 `GET /api/config/admin/sdk-services`、`PUT /api/config/admin/sdk-services/{id}` 和渠道包 `/api/config/admin/channel-apps/{id}/services` 系列，详见 [内部 API](api.md)。
- 充值订单和发货通知不依赖功能开关。


- MySQL DDL 会隐式提交，失败时不能假定事务回滚。应检查实际执行到哪一条 SQL，再制定恢复步骤。
- 接入端副本 `config_access` 独立于权威库，只保存三级节点及同步游标。首次全量拉取由接入服务在表结构就绪后完成，不再自动建表。
- 生产环境继续控制 `/api/config/**` 的访问权限，保护 `gameSecret` 和其他原始配置密钥。
