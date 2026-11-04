# 研发中台架构（当前实现）

本文描述当前系统，不按历史 V1～V11 变更过程组织；具体表结构见 [数据库文档](database.md)，接口见 [API 文档](api.md)。

## 服务与数据流

```text
Vue 3 管理端 ──HTTP──> config-center ────────> 权威 MySQL（config_center）
                             │                         ↑
                    节点变更写入 revision              │ 只读 JOIN
                             │                         │
                             └──签名 Webhook──> config-center-access
                                                   │          │
                                            HTTP 拉取节点     本地 MySQL
                                            快照/增量日志     （config_access）

游戏/客户端 ──配置、订单、渠道事件──> sdk-core ───> 权威 MySQL
平台 ──支付回调──────────────────────> sdk-core ───> 游戏发货通知
平台 ──用户核验──────────────────────> sdk-service ──适配器──> 游戏核验 API
                                               └──────────────> 权威 MySQL（只读）
```

- `config-center`：管理节点、渠道应用与订阅者，并提供同步版本、变更记录、快照查询。
- `config-center-access`：可选独立接入服务，缓存三级节点，不存储渠道应用，也不向权威库写入。
- `sdk-core`：直接查询权威库，根据环境绑定和接口映射生成配置；管理订单、支付回调、异步发货与渠道事件转发。
- `sdk-service`：平台主动调用游戏，目前实现用户核验；通过默认或定制适配器请求大区配置的 `userCheckUrl`，不保存用户核验数据。
- `config-center-admin`：Vue 3 管理界面，后端管理 API 当前没有内建用户认证。

## 配置数据模型

`config_node` 只有三种节点：

1. `game`：根节点，`game_id` 与 `region_id` 均为空。
2. `region`：归属于游戏，`env_type` 仅支持 `test`、`prod`、`review`（旧数据可能为空）。
3. `server_group`：归属于游戏与大区。

表不再使用 `parent_id`；接口的 `parentId` 由 `game_id` / `region_id` 推导。`config_channel_app` 是独立表，`code` 全局唯一，每个 AppID 的 `prod_server_group_id`、`test_server_group_id`、`review_server_group_id` 各指向至多一个区服组。三个环境共享同一份应用 `config` JSON，不存在独立绑定配置表，也不自动向上继承 JSON。

## 节点变更与同步一致性

1. 配置中心写入三级节点时，在同一 MySQL 事务中更新节点、全局 `revision` 和 `config_node_change_log`，确保版本提交次序一致；无变化写入不增加 revision。
2. Webhook 仅携带 `epoch`、`revision`，使用 HMAC-SHA256 与时间窗口验证，不在通知内传输完整配置。接入端收到有效通知后异步拉取。
3. 接入端按 `revision` 获取变更节点 ID，再批量查询最新节点；节点内容与游标在本地同一事务提交。初始化、epoch 变化或变更日志断档时使用一致性快照。
4. 接入端定时校对版本（默认 5 分钟）补偿丢失的 Webhook；HTTP 202 表示通知已受理，**不等于节点已完成同步**。
5. `config_channel_app` 的增删改及三环境绑定不推进三级节点的 `revision`，SDK Core 直接从权威库读取，不依赖接入副本同步。

配置中心多实例可共享权威 MySQL；投递进度由数据库租约与重试协调。单个 MySQL 仍是单点，生产需要独立的备份与故障恢复机制。

## 边界与演进原则

- 当前数据库由管理员手动执行 SQL 建表或升级；各服务启动只校验表结构，不执行 DDL、也不维护历史迁移版本。
- 服务内部业务路由不包含 `/sdk` 或 `/v1`；Nginx 可分别转发 `/api/config/`、`/api/core/`、`/api/service/`，公网若使用 `/sdk/` 前缀由网关负责转换。
- 接口级字段映射编译进 `sdk-core`，新增映射字段不必修改 SQL，但需要重新构建并滚动部署。
- 管理接口、同步接口、回调地址只允许可信内网或受保护的网关访问。生产环境不能将管理 API 与公开 SDK API 无差别暴露。

部署方案见 [生产运维](production.md)。

## SDK 业务与密钥边界

- `sdk_service` 管理已实现功能的全局开关，`sdk_service_channel` 按服务、渠道包、环境唯一绑定；当前功能为 `event_report` 和 `user_check`。
- 游戏侧用 `game.config.gameSecret` 验签，平台侧用 `channel_app.config.appSecret` 验签；两套签名密钥不能混用。
- 订单登记与支付回调具有持久化处理和异步发货补偿，游戏必须幂等；渠道事件目前同步转发、不入库、不自动重试；用户核验不缓存、不落库、不自动重试。
- 对外协议及签名细节以 [游戏 HTML](game.html) 和 [平台 HTML](platform.html) 为准，避免维护多份相同字段表。
