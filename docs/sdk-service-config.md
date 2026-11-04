# SDK 渠道包服务配置

## 数据模型

- `sdk_service`：SDK 功能目录和全局开关，`code` 唯一；不再使用 `scope_type` 或 `sort_order`。当前只初始化已实现的 `event_report`（事件上报），未实现功能不添加占位数据。
- `sdk_service_channel`：渠道包服务绑定，`service_id` 对应服务目录，`channel_app_id` 对应 `config_channel_app.id`，`env` 为 `prod/test/review`。
- 每个服务与渠道包、环境组合唯一：`(service_id,channel_app_id,env)`。无绑定记录时默认关闭，全局或渠道环境关闭时该功能不生效。
- 功能生效还要求渠道包、游戏、当前环境区服组、大区均有效。充值、订单回调和补偿不受事件上报开关控制。
- 服务扩展配置继续保留 JSON 对象结构，目前事件上报不使用扩展配置、不保存事件数据、不自动重试。

区服组不再使用 SDK 功能开关绑定；以后若实现区服级接口，单独评估区服功能配置。

## 手动 SQL

SQL 由管理员手动执行，服务不会自动创建或迁移表：

| 数据库现状 | 操作 |
| --- | --- |
| 新建空 `config_center` | 执行 `config-center/db/sql/initialize.sql` |
| 已有配置库，但无 SDK 服务表 | 执行 `config-center/db/sql/add_sdk_services.sql` |
| 已经使用 `sdk_service_binding` 通用表 | 执行 `config-center/db/sql/migrate_sdk_service_channel.sql` |
| 只有历史 `sdk_service_channel`（`sdk_service.sort_order`） | 先执行 `migrate_sdk_service_binding.sql`，再执行 `migrate_sdk_service_channel.sql` |

已有数据升级前必须备份并停止 config-center、sdk-core。请检查 `SHOW CREATE TABLE sdk_service`、`SHOW CREATE TABLE sdk_service_binding`、原有渠道表，以及旧服务绑定记录。

`migrate_sdk_service_channel.sql` 会：

1. 如果旧的 `sdk_service_channel` 仍存在，将其归档为 `sdk_service_channel_legacy`，移除归档表指向服务目录的外键，以便历史数据独立保留。
2. 清理非 `event_report` 功能的当前绑定和服务目录记录；事件上报的服务 ID、绑定 ID、环境、开关、配置保持不变。
3. 将 `sdk_service_binding` 改名为 `sdk_service_channel`，`target_id` 改名为 `channel_app_id`；删除 `scope_type` 与对应 CHECK 约束。
4. 校验目录最终只有事件上报，检查渠道包绑定是否存在孤儿记录。

如果 `sdk_service_channel_legacy` 已存在或此前迁移中断，不要重复执行；先人工核对冲突。MySQL DDL 会隐式提交，不能假定错误后自动回滚。

## 管理 API

管理接口必须由可信网关保护。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/config/admin/sdk-services` | 服务目录与全局开关 |
| PUT | `/api/config/admin/sdk-services/{id}` | 修改全局开关 |
| GET | `/api/config/admin/channel-apps/{id}/services?env=test` | 查看渠道包功能开关 |
| PUT | `/api/config/admin/channel-apps/{id}/services/{serviceId}?env=test` | 保存渠道包功能配置 |

保存请求保持 `{"enabled":true,"config":{}}`。原先的 `/api/config/admin/server-groups/{id}/services` 系列接口已经移除。

## 事件上报

继续使用 `POST /api/core/{env}/events`、`event_report` 功能编码与现有游戏验签、平台转发流程。事件上报必须同时开启全局开关、当前渠道包环境开关。接口不保存事件数据，也不自动重试。

## 上线检查

1. 全库备份，核对现有 SDK 服务表和绑定数据，停写后执行匹配的 SQL。
2. 核对目录恰好一条 `event_report`，以及绑定记录的 `service_id`、`channel_app_id`、`env`、`enabled`、`config`。
3. 发布配置中心和 SDK Core 后，再发布管理端，检查 SDK Core 表结构只读校验通过。
4. 对 `test/prod/review` 分别测试开启、关闭及无绑定时的事件上报行为；验证充值流程不受影响。
