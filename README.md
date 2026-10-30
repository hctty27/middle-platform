# Middle Platform · 研发中台

研发中台配置中心采用 **Go + MySQL**，管理前端使用 Vue 3，接入示例同样使用 Go。原 Java 实现作为兼容与数据库迁移工程保留。

## 工程组成

| 工程 | 技术栈 | 说明 |
| --- | --- | --- |
| `config-center` | Go | **主配置中心**，管理四级配置树、变更日志、订阅者和 Webhook |
| `config-center-java` | Java 25 / Spring Boot | 旧版配置中心，保留 Flyway V1–V4 数据库迁移和 Java/Go 兼容性测试 |
| `config-center-access` | Go | 独立接入示例，Webhook 验签、增量同步、定期补偿和 MySQL 本地副本 |
| `config-center-admin` | Vue 3 | 无登录的配置管理页面，仅允许可信网络访问 |

## 同步协议

配置变更与单调递增的 `revision`、`config_change_log` 在同一事务提交。Webhook 仅发送 `epoch` 和最新 `revision`；接入端获取变更节点 ID，再查询节点最新状态，并在一个事务内更新本地副本和游标。删除、重试、断线补偿和快照重建均受支持，详见 [架构说明](docs/architecture.md)。

## 本地运行

1. 准备 MySQL 8.0+，首次部署权威数据库仍须运行 `config-center-java` 中的 Flyway 迁移（不能直接跳过 Java 数据库初始化）。
2. 进入 `config-center`，设置 `CONFIG_DB_DSN`、`CONFIG_GO_ADDR=:8080`，执行 `go run ./cmd/config-center`。需要管理写入、回调投递时显式配置 `CONFIG_GO_ENABLE_WRITES=true` 和 `CONFIG_GO_ENABLE_WEBHOOK=true`。
3. 管理页：`cd config-center-admin && npm ci && npm run dev`。
4. 接入端：进入 `config-center-access`，设置 `ACCESS_DB_DSN`、`CONFIG_CENTER_URL`、`CONFIG_WEBHOOK_SECRET`，执行 `go run ./cmd/config-center-access`。

完整步骤参见 [部署文档](docs/deployment.md)、[Go 配置中心](config-center/README.md) 和 [Go 接入端](config-center-access/README.md)。

**安全边界：** 项目没有用户登录和角色权限，管理 API 必须限制到可信内网或受控网关。内部 Webhook 必须进行 HMAC 验签，不应暴露为公共管理接口。

## 四级配置树

业务层级为 `game → region → server_group → channel_app`（游戏 → 大区 → 区服组 → 渠道应用）。旧 `channel` 和 `sdk_app` 类型已经取消。权威数据库通过 Java Flyway V4 升级；历史接入端使用 Flyway V3 升级本地副本。Go 接入端会为全新数据库创建最新副本表，但不会自动执行旧库 Flyway。旧渠道及应用数据归档在 `config_node_v4_legacy_archive`。生产部署前需备份并验证迁移。
