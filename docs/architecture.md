# 配置中心架构决策

- 五级层级固定：game -> region -> server_group -> channel -> sdk_app。
- 所有业务配置仅存于 `config_node`；其余表是同步/订阅的基础设施表。
- `config_node` 仅存储 `game_id`、`region_id`、`server_group_id`、`channel_id` 四个祖先 ID，不存储 `parent_id`；仅保存祖先，不保存自身。
- 游戏四列都为空；大区只保存游戏 ID；区服组保存游戏和大区 ID；渠道再保存区服组 ID；SDK 应用保存全部四个 ID。
- API 中的 `parentId` 是按 `nodeType` 推导的兼容属性；写服务通过创建请求的 `parentId` 校验父类型并自动生成祖先路径。节点层级、类型、编码创建后不可修改。
- 同级 `code` 的唯一性通过按 `nodeType` 和最深非空祖先 ID（根为 0）建立的 MySQL 函数唯一索引保证。`config` 只保存本节点 JSON，不向上继承。
- 配置中心多个 Spring Boot 实例共享单个 MySQL，应用无独立配置状态。
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

Java HTTP 入口统一放在各业务模块的 `api` 包，类名统一为 `*Api`。不按版本拆 Java 包或增加 URL 版本段；后续若需要不兼容升级，先评估兼容字段、独立资源或明确迁移窗口，并按实际需求决定是否引入版本协商。

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


## Java 包结构（业务模块 + 职责子包）

`config-center-server` 保留 `com.hitall.platform.config.ConfigCenterApplication` 作为根启动类，按以下业务域组织：

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

`config-center-access` 是独立 Spring Boot 应用，Java 根包为 `com.hitall.platform.access`。内部按 `api`、`callback`、`config`、`sync`、`remote`、`repository` 组织；`ConfigAccessConfiguration` 改为普通应用配置，不再使用 Spring Boot 接入服务 自动装配元数据。`ConfigCallbackApi` 由组件扫描注册一次，避免重复路由。

原 接入服务 查询接口更名为 `access.api.AccessApi`，提供 `GET /api/access/nodes` 和 `GET /api/access/revision`；内部回调继续使用 `POST /internal/config/callback`。本次不改配置中心远程 API、数据库表、SQL、Webhook 验签和同步协议。接入工程可独立运行，不作为项目依赖或 接入服务 提供给其他工程。

CI 会运行 `scripts/check_java_packages.py`，阻止 Java 文件路径和 package 声明不一致，以及重要模块再次退化为扁平包。

**接口层命名约定：** 所有使用 `@RestController` 的 Java 类统一位于所属业务模块的 `api` 包，并以 `Api` 结尾，例如 `node.api.NodeApi`、`sync.api.SyncApi`。接入工程的回调入口位于 `access.callback.api.ConfigCallbackApi`，本地查询入口位于 `access.api.AccessApi`。旧 `/demo/**` 本地查询路径已替换为 `/api/access/**`，但 Webhook 路径不变。

### API 命名规则

配置中心 API 类为 `node.api.AdminNodeApi`、`node.api.NodeApi`、`node.api.NodeNavigationApi`、`sync.api.SyncApi`、`subscriber.api.SubscriberApi`。不提供旧接口别名；Flyway 数据库迁移版本独立于 HTTP 路由。

**补充约定**：Java 的 `api` 包、`*Api` 类名及正式 HTTP 地址都不包含版本号。CI 将检查包路径及命名。
