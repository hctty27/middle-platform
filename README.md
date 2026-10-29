# Middle Platform · 研发中台

基于 **JDK 25 + Spring Boot 4.1.2 + Gradle 9.8.2 + MySQL** 的研发中台配置中心。前端使用 Vue 3。

## 组成

- `config-center`：多实例无状态服务，共用一个权威 MySQL，管理四级配置树与变更日志。
- `config-center-admin`：无需登录的管理页（仅限可信网络部署）。
- `config-center-access`：独立接入工程，内置 Webhook 验签、增量同步、定期补偿和 MySQL 本地副本，不对外发布 Starter。

## 同步协议

配置变更与单调递增 `revision`、`config_change_log` 同事务提交。Webhook 仅发送 `epoch` 和最新 `revision`；接入方查询变更 ID 并批量读取最新节点，事务更新本地副本和游标。删除使用 `DELETE` 事件。持久化投递、失败重试、断线补偿、全量初始化。详见 `docs/architecture.md`。

## 本地启动

1. JDK 25，MySQL 8.0+。
2. 通过 Gradle Wrapper 启动：Linux/macOS 执行 `./gradlew :config-center:bootRun`，Windows 执行 `gradlew.bat :config-center:bootRun`。首次运行会自动下载 Gradle 9.8.2。配置 `CONFIG_DB_URL`、`CONFIG_DB_USER` 和 `CONFIG_DB_PASSWORD`。
3. 管理页：`cd config-center-admin && npm ci && npm run dev`。
4. 接入工程使用独立数据库，通过 `CONFIG_CENTER_URL` 访问配置中心；使用 `ACCESS_DB_URL`、`ACCESS_DB_USER` 和 `ACCESS_DB_PASSWORD` 配置副本数据库，执行 `./gradlew :config-center-access:bootRun` 启动。

**重要：不实现用户登录和角色权限。所有配置管理接口相当于管理员级权限，必须限制到受信任的内网/网关。Webhook 必须由接入方验证签名，禁止任意内网回调 URL。**

本项目使用 Flyway 管理数据库演进。已执行的 V1/V2 版本迁移不可直接改写；新增数据库结构变更需继续使用更高版本迁移。

## 四级配置树

当前业务层级为 `game → region → server_group → channel_app`（游戏 → 大区 → 区服组 → 渠道应用）。旧 `channel` 层级和 `sdk_app` 类型已经取消；配置中心使用 Flyway V4 升级，接入端本地副本使用 Flyway V3 升级。原有 SDK 应用 ID、Code、JSON 配置不变，旧渠道及其应用关联保存在配置中心的 `config_node_v4_legacy_archive` 归档表中。部署前务必完成备份、重名预检和停写切换，详见 `docs/deployment.md`。
