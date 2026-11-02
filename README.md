# Middle Platform · 研发中台

基于 **Go + MySQL 8.0+ + Vue 3** 的游戏研发配置中台。代码仓库包含三个独立 Go 服务、一个 Vue 管理端以及发布与 CI 脚本；不依赖 Java、Flyway 或 Docker。

## 工程组成

| 目录 | 职责 |
| --- | --- |
| `config-center/` | 权威配置中心：三级配置节点、渠道应用、订阅者、同步日志与数据库迁移 |
| `config-center-access/` | 可选接入端：独立 MySQL 节点副本、Webhook 验签、快照与增量同步 |
| `sdk-core/` | SDK 核心服务：目前提供按环境与 AppID 查询配置，登录/充值能力尚未在此文档宣称实现 |
| `config-center-admin/` | Vue 3 配置管理后台 |
| `shared/logging/` | 三个 Go 服务共用的结构化日志 |
| `scripts/` | 统一 Linux 打包脚本及 CI 测试脚本 |

配置层级为 `game → region → server_group`，渠道应用 `channel_app` 独立维护。每个渠道应用的 AppID 全局唯一，最多分别绑定一个 `prod`（正式）、`test`（测试）、`review`（提审）区服组；渠道应用的 `config` 不按环境拆分。

## 快速开始（Windows PowerShell）

依赖：**Go 1.27.3、MySQL 8.0+**；启动 Vue 管理端额外需要 Node.js 22 / npm。

1. 创建空的 `config_center` 数据库。
2. 在 `config-center/` 下复制 `configs/config.yaml` 为 `configs/config-dev.yaml`，填写 `database.dsn`，按需启用 `admin.writesEnabled` / `webhook.enabled`。
3. **先独立迁移，后启动服务**（下面两条命令都在 `config-center/` 目录执行）：

```powershell
Copy-Item configs/config.yaml configs/config-dev.yaml
# 修改 configs/config-dev.yaml 后执行
go run ./cmd/migrate -config configs/config-dev.yaml
go run ./cmd/config-center -config configs/config-dev.yaml
```

4. 接入端和 SDK Core 分别在**各自目录、各自终端**复制 YAML 后启动。接入端使用独立的 `config_access` 数据库；SDK Core 使用权威库的只读账号。

```powershell
# 在 config-center-access 目录；需先创建空的 config_access 数据库
Copy-Item configs/config.yaml configs/config-dev.yaml
go run ./cmd/config-center-access -config configs/config-dev.yaml

# 在 sdk-core 目录；另开终端
Copy-Item configs/config.yaml configs/config-dev.yaml
go run ./cmd/sdk-core -config configs/config-dev.yaml
```

5. 在 `config-center-admin/` 下运行 `npm ci`、`npm run dev`。

默认监听端口：配置中心 `8080`、接入端 `8081`、SDK Core `8082`。完整建库、配置项及联调步骤见 [本地开发指南](docs/development.md)。

## 生产发布

开发机在仓库根目录执行：

```powershell
.\scripts\package.ps1 -Arch amd64 -Version v1.0.0
```

发布脚本仅在 `dist/` 下生成一个 `middle-platform-v1.0.0-linux-amd64.tar.gz`；归档内只有 `bin/` 和可选 `admin/`，不包含生产密钥或服务管理配置。参见 [打包说明](docs/package.md) 与 [生产部署与运维](docs/production.md)。

## 文档导航

- [架构与配置同步](docs/architecture.md)
- [HTTP API 契约](docs/api.md)
- [数据库结构与迁移](docs/database.md)
- [本地开发与联调](docs/development.md)
- [Linux 打包](docs/package.md)
- [生产部署、滚动更新、日志与优雅停机](docs/production.md)
- [CI 与测试](docs/ci.md)
- 模块说明：[配置中心](config-center/README.md) · [接入端](config-center-access/README.md) · [SDK Core](sdk-core/README.md)

**安全边界：** 当前管理 API 没有账号权限系统。`/api/config/admin/**`、配置同步接口、内部 Webhook 均不得直接暴露到公网。数据库迁移不是应用启动时自动执行的操作；已有数据库不可删除迁移记录后强行初始化。
