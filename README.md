# Middle Platform · 研发中台

基于 **Go + MySQL 8.0+ + Vue 3** 的游戏研发中台。提供配置管理、Webhook 配置同步、SDK 配置查询、充值订单与发货通知、事件上报、平台主动用户核验。

## 工程组成

| 目录 | 职责 |
| --- | --- |
| `config-center/` | 权威配置中心：游戏、大区、区服组、渠道应用、订阅者、服务开关 |
| `config-center-access/` | 可选接入端：三级节点副本、Webhook 回调与快照/增量同步 |
| `sdk-core/` | 配置查询、订单登记、支付回调、异步发货、渠道事件上报 |
| `sdk-service/` | 平台主动调用游戏：目前实现用户核验，默认 `standard` 适配器 |
| `config-center-admin/` | Vue 3 管理端 |
| `shared/` | 结构化日志和签名公共库 |
| `scripts/` | 打包及 CI 测试辅助脚本 |

配置结构为 `game → region → server_group`，`channel_app` 独立维护；渠道包 AppID 全局唯一，每个 `prod/test/review` 环境最多绑定一个区服组。**SDK Core、SDK Service 与配置中心使用同一个 `config_center` 权威库**；只有可选的接入端使用独立 `config_access` 副本库。

## 本地运行

依赖 **Go 1.27.3、MySQL 8.0+**，管理端另需 Node.js 22/npm。

1. 创建空 `config_center`，手工执行 `config-center/db/sql/initialize.sql`；已有库禁止重新执行初始化 SQL。
2. 在各服务目录复制 `configs/config.yaml` 为 `configs/config-dev.yaml`，填写数据库 DSN 等必要配置，切勿提交真实密码。
3. 在各模块目录执行 `go run ./cmd/<模块名> -config configs/config-dev.yaml`。
4. 在 `config-center-admin/` 执行 `npm ci`、`npm run dev`。

本地默认监听：配置中心 `8080`、可选接入端 `8081`、SDK Core `8082`、SDK Service `8221`。完整步骤见 [本地开发与测试](docs/development.md)。

## 文档导航

**对外接口**：[统一接口文档](docs/index.html)（游戏服务端 / 平台服务端 / 第三方配置同步）

**内部开发**：[HTTP API 索引](docs/api.md) · [架构](docs/architecture.md) · [数据库与手动 SQL](docs/database.md) · [开发与 CI](docs/development.md) · [打包与生产运维](docs/production.md)

**模块**：[配置中心](config-center/README.md) · [配置接入端](config-center-access/README.md) · [SDK Core](sdk-core/README.md) · [SDK Service](sdk-service/README.md)

平台原始集成协议保留于 [references/服务端集成文档.md](references/服务端集成文档.md)，仅作为外部原始资料，不代表所有接口均已实现。

## 安全与部署边界

- 数据库 DDL 由管理员审核后手动执行，服务启动不建表、不自动迁移。
- 管理写接口始终注册，`/api/config/admin/**` 和可能包含密钥的原始配置/同步接口必须通过内网或受保护的网关访问。
- Go 业务路由不包含 `/sdk` 前缀，公网如使用 `/sdk/` 应由 Nginx 负责前缀转换。
- 订单与发货通知必须幂等；事件上报当前不落库、不自动重试。对外请求与签名规范统一在 `docs/index.html` 中维护。
