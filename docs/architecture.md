# 配置中心架构决策

- 五级层级固定：game -> region -> server_group -> channel -> sdk_app。
- 所有业务配置仅存于 `config_node`；其余表是同步/订阅的基础设施表。
- 游戏 parent_id=0，其他节点 parent_id>0；不使用父节点自引用 FK。写服务核对父子类型。
- parent_id、node_type、code 创建后不可修改；`config` 是本节点 JSON，不向上继承。
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
