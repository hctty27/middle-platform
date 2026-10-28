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

## API 契约

- `GET /api/v1/admin/nodes` 列出全部节点
- `POST /api/v1/admin/nodes` 新增节点
- `PUT /api/v1/admin/nodes/{id}` 修改名称、状态、JSON、备注
- `DELETE /api/v1/admin/nodes/{id}` 无子节点才允许删除
- `GET /api/v1/config/nodes/{id}` 获取节点
- `POST /api/v1/config/nodes/batch-get` 获取节点集合
- `GET /api/v1/sync/revision` 当前数据集标识和版本
- `GET /api/v1/sync/changes?afterRevision=&limit=` 获取连续变更
- `GET /api/v1/sync/snapshot` 初始化专用（需事务一致性快照）
- `POST /api/v1/admin/subscribers` 配置接入方 Webhook
- `POST /internal/config/callback` 接入方接收通知

## 正确性关键

单纯的实时分页不是一致性快照：首次全量同步必须使用一致性快照，或导出物化快照并附带对应版本。
若变更日志已被清理而出现游标缺口，客户端应重新初始化，而非跳过版本。
版本号每次配置写操作递增，普通读取、订阅者编辑不改变版本。
单 MySQL 是单点故障；必须备份、监控、测试恢复，前端无登录不等于 API 可公开访问。

## V2 数据库升级

- Flyway V1 已被使用，不修改；V2 在配置中心和 Demo 副本分别新增四个祖先列、按原父子关系回填、校验路径完整性并删除持久化 `parent_id`。
- V2 重建同级唯一索引和四个查询索引，并更新权威端 `epoch`，使新 Starter 对旧数据执行全量快照恢复（revision 不重置）。
- **这是非向后兼容的数据库升级**：先备份数据库，停旧版读写服务，部署迁移与新版服务及 Starter，最后恢复流量；避免旧版实例继续查询或写入已经删除的列。
- `parentId` 保留在管理 API 和写入请求中，前端无需直接编辑祖先路径。部署结束应验收同级 `code` 唯一、历史数据回填、搜索路径、Webhook 同步与删除。

## 同步可靠性及清理

- 客户端的快照恢复共用 `resetFromSnapshot()`，避免首次接入与断档恢复使用两套写入逻辑。
- 相同节点内容（含仅 JSON 空白差异）的重复 PUT 不再递增 `revision`；真实配置变更仍与日志在同一写事务中提交。
- V3 为每个订阅者引入 `acknowledged_epoch`。投递条件以 `(epoch, revision)` 作为游标，即使数据集标识单独变化也必须再次通知；变更世代时接入方主动重建本地快照。
- V1/V2 已发布迁移保持不变。V1→V2→V3 的历史配置回填需在独立数据库执行 CI 验证。
- 当前不删除 `config_change_log.idx_change_node` 和历史迁移文件；索引删除需先确认实际线上查询没有依赖。
