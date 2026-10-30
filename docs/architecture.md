# 配置中心架构决策

- 当前四级层级固定：game -> region -> server_group -> channel_app。
- 所有业务配置仅存于 `config_node`；其余表是同步/订阅的基础设施表。
- 当前 `config_node` 仅存储 `game_id`、`region_id`、`server_group_id` 三个祖先 ID，不存储 `parent_id`；仅保存祖先，不保存自身。
- 游戏三个祖先列均为空；大区只保存游戏 ID；区服组保存游戏和大区 ID；渠道应用保存游戏、大区和区服组 ID。
- API 中的 `parentId` 是按 `nodeType` 推导的兼容属性；写服务通过创建请求的 `parentId` 校验父类型并自动生成祖先路径。节点层级、类型、编码创建后不可修改。
- 同级 `code` 的唯一性通过按 `nodeType` 和最深非空祖先 ID（根为 0）建立的 MySQL 函数唯一索引保证；`channel_app` 的直属父节点是 `server_group`。`config` 只保存本节点 JSON，不向上继承。
- 配置中心的 Go 主服务与 Java 兼容服务均可使用同一个 MySQL，应用无独立配置状态。
- 全局版本行必须在写事务内先锁定，确保提交顺序与 revision 一致；同时提交 config_node 与 change log。
- Webhook 通知仅包含 epoch 和 revision，订阅者按 revision 获取 ID 列表并查询节点最新状态。
- 重试允许重复通知，客户端需要实现幂等且严格连续推进本地游标。
- Webhook 必须通过 HMAC SHA-256 验证；管理接口只在可信网络提供。
- 不实现账号、登录、角色与权限模块。

## API 契约与路由规范

统一 HTTP 前缀为 `/api/{service}/...`，配置中心使用 `/api/config`，SDK 认证服使用 `/api/auth`，SDK 功能服使用 `/api/service`。Nginx 只按服务前缀分流，不按版本号分流；服务端 URL、Java 包名和接口类名均不带版本号。

- 管理端：`GET/POST /api/config/admin/nodes`，`GET/PUT/DELETE /api/config/admin/nodes/{id}`
- 配置树：`GET /api/config/admin/nodes/tree?parentId=...`
- 直属子项分页：`GET /api/config/admin/nodes/{id}/children?page=...&size=...&keyword=...`；根级仍通过 `/admin/nodes/children?parentId=0` 查询
- 全局搜索与路径：`GET /api/config/admin/nodes/search?keyword=...`、`GET /api/config/admin/nodes/{id}/path`
- 订阅者管理：`GET/POST /api/config/admin/subscribers`
- 客户端节点查询：`GET /api/config/nodes/{id}`、`POST /api/config/nodes/batch-get`
- 增量与快照：`GET /api/config/sync/revision`、`GET /api/config/sync/changes?afterRevision=...&limit=...`、`GET /api/config/sync/snapshot`
- 接入实例内部回调：`POST /internal/config/callback`，属于内部 Webhook，不公开在统一 API 网关
- 接入工程本地查询：`GET /api/access/nodes`、`GET /api/access/revision`；仅供接入端自身或可信网络调用

现存 Java 配置中心的 HTTP 入口统一放在各业务模块的 `api` 包，类名统一为 `*Api`。Go 接入工程统一使用 `internal/api`。不按版本拆 Java 包或增加 URL 版本段；后续若需要不兼容升级，先评估兼容字段、独立资源或明确迁移窗口，并按实际需求决定是否引入版本协商。

**接口收敛**：配置中心只暴露 `/api/config/**`。旧版版本化接口全部下线，不再保留兼容映射。必须在同一上线窗口升级管理端、接入服务与所有调用方，并调整网关路由；否则旧调用方将收到 404。

## 正确性关键

单纯的实时分页不是一致性快照：首次全量同步必须使用一致性快照，或导出物化快照并附带对应版本。
若变更日志已被清理而出现游标缺口，客户端应重新初始化，而非跳过版本。
版本号每次配置写操作递增，普通读取、订阅者编辑不改变版本。
单 MySQL 是单点故障；必须备份、监控、测试恢复，前端无登录不等于 API 可公开访问。

## V2 数据库升级

- Flyway V1 已被使用，不修改；V2 在配置中心和 接入服务 副本分别新增四个祖先列、按原父子关系回填、校验路径完整性并删除持久化 `parent_id`。
- V2 重建同级唯一索引和四个查询索引，并更新权威端 `epoch`，使新 接入服务 对旧数据执行全量快照恢复（revision 不重置）。
- **这是非向后兼容的数据库升级**：先备份数据库，停旧版读写服务，部署迁移与新版服务及 接入服务，最后恢复流量；避免旧版实例继续查询或写入已经删除的列。
- `parentId` 保留在管理 API 和写入请求中，前端无需直接编辑祖先路径。部署结束应验收同级 `code` 唯一、历史数据回填、搜索路径、Webhook 同步与删除。

## 同步可靠性及清理

- 客户端的快照恢复共用 `resetFromSnapshot()`，避免首次接入与断档恢复使用两套写入逻辑。
- 相同节点内容（含仅 JSON 空白差异）的重复 PUT 不再递增 `revision`；真实配置变更仍与日志在同一写事务中提交。
- V3 为每个订阅者引入 `acknowledged_epoch`。投递条件以 `(epoch, revision)` 作为游标，即使数据集标识单独变化也必须再次通知；变更世代时接入方主动重建本地快照。
- V1/V2 已发布迁移保持不变。V1→V2→V3 的历史配置回填需在独立数据库执行 CI 验证。
- 当前不删除 `config_change_log.idx_change_node` 和历史迁移文件；索引删除需先确认实际线上查询没有依赖。

## Webhook 异步触发与补偿

- 接入端 `POST /internal/config/callback` 先验证时间戳和 HMAC SHA-256 签名；合法通知成功入队时返回 **202 Accepted**，不再等待实际配置同步完成；签名错误返回 401，执行器拒绝任务返回 503。
- `202` 仅表示通知已在接入实例的**内存中接受**，不代表同步完成或持久化投递成功。配置中心仍以 2xx 作为投递成功判据；接入实例在成功应答后宕机会丢失该内存任务。
- 每个接入实例使用一个受限后台线程，同一时刻最多运行一个同步任务；短时间重复通知合并，执行期间收到新通知最多追加一次后续同步。
- 异步同步失败不会影响已经返回的 202。启动后 3 秒首次执行、默认每 5 分钟执行的定期校对仍然作为**最终一致性补偿机制**；进程重启后也会重新校对。
- 后台任务依旧依赖客户端本地 MySQL 的 `epoch + revision` 游标以及事务内校验，保证增量和快照恢复时不会被其他实例的旧结果覆盖。
- 需要强持久通知、严格投递完成确认或更短恢复时间时，应进一步引入持久化任务表/消息队列或加强校对频率和告警，本阶段不引入新基础设施。


## Java 兼容配置中心包结构（`config-center-java`）

`config-center-java` 保留 `com.hitall.platform.config.ConfigCenterApplication` 作为根启动类，按以下业务域组织：

```text
config/
├── ConfigCenterApplication.java
├── node/
│   ├── api/
│   ├── service/
│   ├── repository/
│   ├── model/
│   └── dto/
├── sync/
│   ├── api/
│   ├── repository/
│   └── dto/
├── subscriber/
│   ├── api/
│   ├── service/
│   ├── repository/
│   └── dto/
└── webhook/
    ├── service/
    └── repository/
```

`config-center-access` 现为独立 Go 应用，入口 `cmd/config-center-access`，内部按 `api`、`settings`、`syncer`、`remote`、`store` 分层。Webhook 回调签名协议和本地查询路径不变；新库由 Go 自动建四级表，旧库必须先完成历史 Flyway V3。接入端不是其他项目的 Starter 依赖。

CI 会运行 `scripts/check_java_packages.py`，阻止 Java 文件路径和 package 声明不一致，以及重要模块再次退化为扁平包。

**接口层命名约定：** 所有使用 `@RestController` 的 Java 类统一位于所属业务模块的 `api` 包，并以 `Api` 结尾，例如 `node.api.NodeApi`、`sync.api.SyncApi`。Go 接入工程的回调与本地查询都位于 `internal/api`。旧 `/demo/**` 本地查询路径已替换为 `/api/access/**`，但 Webhook 路径不变。

### API 命名规则

配置中心 API 类为 `node.api.AdminNodeApi`、`node.api.NodeApi`、`node.api.NodeNavigationApi`、`sync.api.SyncApi`、`subscriber.api.SubscriberApi`。不提供旧接口别名；Flyway 数据库迁移版本独立于 HTTP 路由。

**补充约定**：Java 的 `api` 包、`*Api` 类名及正式 HTTP 地址都不包含版本号。CI 将检查包路径及命名。

## V4 四级配置树升级

- 当前唯一有效节点类型：`game`、`region`、`server_group`、`channel_app`。禁止再创建 `channel` 和 `sdk_app`。
- 服务端新增 Flyway `V4__channel_app_hierarchy.sql`，删除 `channel_id`，调整约束和同级唯一索引；保留 SDK 应用原主键、编码和 JSON，并迁至区服组下。
- V4 在执行不可逆变更前，通过临时唯一键检查同一区服组下的重复应用 Code。若有冲突，迁移会失败，需要先解决并重新部署。
- V4 将原 `channel` 及 `sdk_app` 记录完整归档到 `config_node_v4_legacy_archive`，保留旧渠道配置、应用到渠道的关系。归档表不参与新系统的配置查询和同步；旧渠道 JSON 不会自动合并到应用 JSON。
- 迁移完成后配置中心切换 `epoch`，不重置 `revision`。历史 Java 接入工程使用独立 Flyway `V3__channel_app_replica.sql` 清理本地旧副本并重置同步游标，切换 Go 前需先执行，首次访问新服务后按四级快照全量恢复。
- V1/V2/V3 是历史迁移，不改写其内容；旧 V2 章节中的五级路径仅用于说明历史数据如何升级。生产上线必须先备份、协调停写、同时替换配置中心和接入端二进制，然后恢复 Webhook 投递。
- 管理界面仍只把前三层作为懒加载目录，`channel_app` 在区服组右侧分页列表中维护。HTTP 路由与 Nginx 分流规则不变。
