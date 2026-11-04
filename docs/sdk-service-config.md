# SDK 服务定义与通用绑定

## 数据模型

- `sdk_service`：服务目录，`code` 全局唯一，`scope_type` 仅支持 `channel_app` / `server_group`，并提供全局 `enabled`。已删除 `sort_order`，按 ID 稳定展示。
- `sdk_service_binding`：唯一键 `(service_id,target_id,env)`；根据服务定义的 `scope_type` 解释 `target_id`，`env` 支持 `prod/test/review`。
- `target_id` 不建立跨类型外键，Go 写入时校验目标存在、区服组对应环境以及作用范围，并在删除渠道应用或区服组时事务清理对应绑定。
- 原有 6 个服务在迁移时统一标记为 `channel_app`，以保持原有渠道绑定有效。只有确认一个服务不存在旧渠道绑定后，才允许通过人工 SQL 将它重分类为 `server_group`；建议为语义不同的新业务另建独立服务编码。
- 未创建绑定记录默认关闭。功能生效要求全局服务、绑定开关、目标、上级配置节点和环境均有效。充值支付补偿不受服务开关控制。

## 手动 SQL（两种安装路径互斥）

- 新建空库：`config-center/db/sql/initialize.sql`。
- 旧库已经有 `sdk_service`、`sdk_service_channel`：先备份并停止旧服务写入，再执行 `config-center/db/sql/migrate_sdk_service_binding.sql`。脚本保留旧表作回退，不会删除现有绑定。
- 旧库没有任何服务表：执行 `config-center/db/sql/add_sdk_services.sql`，不要再执行迁移脚本。

SQL 不由服务自动执行。旧库迁移后，需要核对脚本末尾的记录计数和差异查询，确认两个结果分别为“计数相等”和“差异为 0”。**更新数据库结构与切换服务二进制必须协调停写窗口**：旧服务依赖 `sort_order` 和 `sdk_service_channel`；切换后不要再运行旧版写服务。

## 管理 API

以下接口都属于需网关鉴权的配置中心管理接口：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/config/admin/sdk-services` | 返回服务目录及 `scopeType` |
| PUT | `/api/config/admin/sdk-services/{id}` | 修改全局启用开关 |
| GET | `/api/config/admin/channel-apps/{id}/services?env=test` | 渠道包服务配置（兼容原路径） |
| PUT | `/api/config/admin/channel-apps/{id}/services/{serviceId}?env=test` | 保存渠道包绑定 |
| GET | `/api/config/admin/server-groups/{id}/services?env=prod` | 区服组服务配置 |
| PUT | `/api/config/admin/server-groups/{id}/services/{serviceId}?env=prod` | 保存区服组绑定 |

请求保存结构仍为 `{"enabled":true,"config":{}}`，只有 `enabled` 和 `config` 可更改；服务作用范围由初始化 SQL 管理而非 UI 修改。

## 前端

- 「服务管理」增加作用范围展示。
- 「渠道应用 → 服务配置」只展示 `channel_app` 作用范围。
- 「区服组 → 服务配置」只展示 `server_group`，环境来自所属大区。
- 两处复用 `ServiceBindingPanel.vue`。

## 事件上报

现有 `POST /api/core/{env}/events` 接口、SHA-1 签名、平台转发以及 `event_report` 功能码不变；`sdk-core` 的开关查询改为 `sdk_service_binding` 并明确要求 `scope_type='channel_app'`。

不增加事件级规则、不保存事件数据、不自动重试。区服上报及区服查询等业务接口仍未实现。

## 上线校验

1. 备份数据库，暂停旧服务的相关写操作，执行对应 SQL。
2. 验证服务目录、旧绑定数据、唯一约束、作用范围及目标有效性。
3. 先发布配置中心和 SDK Core，再发布管理端；确认服务启动时只读校验通过。
4. 验证原渠道事件开关、区服组配置读取/保存与原有订单功能。
5. 稳定后单独评审是否归档或删除旧 `sdk_service_channel`，本次不会自动删除。
