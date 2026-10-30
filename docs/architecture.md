# 配置中心架构决策

## 项目组成

- `config-center`：Go 配置中心，提供统一配置管理、订阅者、版本与变更日志。
- `config-center-access`：独立 Go 接入服务，通过 Webhook 触发主动同步，并维护独立 MySQL 副本。
- `config-center-admin`：Vue 3 管理端。
- `config-center/internal/migration`：Go 版本化数据库迁移，保留 V1–V4 原始 SQL 升级历史。

部署只需要 Go、MySQL 和前端构建工具。

## 三级配置树与独立渠道应用

节点固定为 `game → region → server_group`，均保存在 `config_node`。渠道应用统一存储在 `config_channel_app`，三个环境的区服组 ID 分别保存在 `prod_server_group_id`、`test_server_group_id`、`review_server_group_id`，公共 JSON 只有一份。节点只保留 `game_id`、`region_id` 两个祖先字段，不存储 `parent_id`。API 的 `parentId` 由当前节点类型推导。

同层级 `code` 通过 MySQL 唯一索引保障；创建节点校验合法父类型。配置的 JSON 不进行向上继承。

## HTTP 接口

保持不带版本号的统一路径：

- `GET/POST /api/config/admin/nodes`，`GET/PUT/DELETE /api/config/admin/nodes/{id}`
- `GET /api/config/admin/nodes/tree?parentId=...`
- `GET /api/config/admin/nodes/{id}/children?page=...&size=...&keyword=...`
- `GET /api/config/admin/nodes/search?keyword=...`
- `GET /api/config/admin/nodes/{id}/path`
- `GET/POST /api/config/admin/subscribers`
- `GET /api/config/nodes/{id}`、`POST /api/config/nodes/batch-get`
- `GET /api/config/sync/revision`、`GET /api/config/sync/changes`、`GET /api/config/sync/snapshot`
- 接入内部回调：`POST /internal/config/callback`
- 接入本地查询：`GET /api/access/nodes`、`GET /api/access/revision`

生产 Nginx 对 `/api/config/**` 路由到 Go 配置中心。管理 API 必须限制到可信网络；当前无登录/角色功能。

## 数据一致性

权威配置中心多实例共享同一个 MySQL。写事务先锁全局版本行，然后更新节点与 `config_node_change_log`，保证提交顺序和递增 revision 一致；重复无变化更新不增加版本。

Webhook 仅发送 `epoch`、`revision`，由接入侧校验 HMAC-SHA256 和请求时间窗口，异步触发同步。接入端按 revision 连续拉取变更节点 ID，批量查询最新状态，并在短事务内一起更新本地节点副本和同步游标。删除事件会删除本地节点。渠道应用 CRUD 不影响节点版本、日志和回调。

接入端首次连接、epoch 变化、游标缺口时使用一致性快照重建；后台合并重复通知，每五分钟校对一次，补偿丢失 Webhook。回调返回 202 仅表示接受任务，不保证任务已经持久化或完成。接入本地事务通过版本比较和游标行锁协调多进程。

配置中心 Webhook 投递使用数据库租约、签名、重试、重定向限制与目标 IP 校验。单 MySQL 仍然是数据库级别的单点故障，需要备份、监控和恢复演练。

## 数据库迁移

Go 主程序在启动前校验 schema，通过 MySQL 命名锁避免多个配置中心同时执行 DDL。内置 V1–V4 SQL 保留历史迁移逻辑，包括旧 `channel`、`sdk_app` 到四级 `channel_app` 的迁移和归档。

- 新数据库执行 V1→V11，记录 `config_schema_migrations` 版本。
- 已迁到 V4 的旧数据库先验证结构，再建立 Go 迁移状态，不重复清理配置。
- 历史 V1–V3 数据库需要人工备份、停写，再显式开启旧版升级。
- MySQL DDL 隐式提交；失败后标记 dirty，禁止自动重跑半成功的迁移。
- `config-center-access` 新库只建立三级节点与游标副本表，历史旧库保留 SQL 供必要的手动结构升级。

## Go 包结构

```text
config-center/
├── cmd/
│   ├── config-center/
│   └── migrate/
├── internal/
│   ├── api/
│   ├── app/
│   ├── migration/sql/
│   ├── model/
│   ├── service/
│   ├── settings/
│   ├── sqlc/
│   ├── sqlcgen/
│   ├── store/
│   └── webhook/
└── go.mod
```

接入工程包含 `cmd/config-center-access` 和 `internal/api`、`settings`、`remote`、`store`、`syncer`。所有对外 HTTP 接口继续保持现有契约，无需修改 Vue 路径。

## V8 节点同步边界

- 同步快照仅返回 `epoch`、`revision`、`nodes`；只有三级节点的增删改会推进 revision。
- 渠道应用及其环境绑定不参与节点同步。
- V8 升级仅切换一次 epoch，不删除现有渠道业务数据；旧接入副本中的渠道表不会自动删除。

## V9 同步表命名规范

- 节点事件日志改为 `config_node_change_log`，按 `revision` 记录三级节点增量。
- Webhook 投递进度改为 `config_subscriber_delivery_state`，保持原有订阅者外键、租约和失败重试语义。
- 仅调整表名和中文表注释；`config_revision` 的 epoch/revision 不变，接入副本协议无需改动。
- V6/V4 历史归档不再是 V9 运行时依赖；迁移不自动删除归档，人工确认外部备份后方可清理。

## V11 三环境绑定收敛

- AppID 的正式服、测试服、提审服各绑定最多一个区服组，并共用一份 `config`。
- 删除 `config_channel_app_binding`；移除绑定专属 CRUD，区服组页只读显示关联应用。
- 在执行 DDL 前校验旧绑定的环境唯一性、游戏归属、状态和最终 JSON 一致性，有冲突则拒绝迁移。
- `sdk-core` 直接按环境选择一个区服组，必须与配置中心同时升级。
