# Middle Platform · 研发中台

本仓库已统一使用 **Go + MySQL** 实现配置中心和接入服务，管理界面使用 Vue 3。不再需要 JDK、Spring Boot、Gradle 或 Flyway。

## 工程结构

| 目录 | 技术栈 | 用途 |
| --- | --- | --- |
| `config-center` | Go | 权威配置中心、四级配置树、版本日志、Webhooks、数据库迁移 |
| `config-center-access` | Go | 独立接入示例、本地 MySQL 副本、增量同步与 HMAC 回调 |
| `sdk-core` | Go | SDK 登录充值核心服务（首期提供按环境、AppID 查询绑定区服组和公共配置） |
| `config-center-admin` | Vue 3 | 配置管理界面 |
| `deploy` | MySQL | 本地测试数据库 |
| `scripts` | Python | MySQL 迁移与 Go 双实例端到端测试 |

## 推荐：使用 YAML 本地启动

两个 Go 工程都支持指定 YAML 配置文件，公共默认配置在各自的 `configs/config.yaml`，实际的开发/生产配置不提交到 Git。

在对应工程目录下，将 `configs/config.yaml` 复制为 `configs/config-dev.yaml`，填写数据库连接等配置后启动（`config.yaml` 保留为空凭证模板）：

```powershell
go run ./cmd/config-center -config configs/config-dev.yaml
```

接入工程使用：

```powershell
go run ./cmd/config-center-access -config configs/config-dev.yaml
```

配置优先级：程序默认值 → `config.yaml` → 指定 YAML → 环境变量。省略 `-config` 则兼容旧版仅环境变量启动模式。详见两个工程各自的 README。

## 启动 Go 配置中心

新环境只需要 Go 1.27.3 和 MySQL 8.0+。先创建 `config_center` 数据库；程序启动时会自动执行内置 V1–V5 迁移。

```bash
cd config-center
export CONFIG_DB_DSN='root:password@tcp(127.0.0.1:3306)/config_center?parseTime=true&loc=UTC'
export CONFIG_GO_ADDR=':8080'
export CONFIG_GO_ENABLE_WRITES=true
export CONFIG_GO_ENABLE_WEBHOOK=true
go run ./cmd/config-center
```

也可以先独立执行数据库迁移：`go run ./cmd/migrate`。历史旧库如果尚未升级到 V4，必须先备份、停写并显式执行 `go run ./cmd/migrate -allow-legacy-upgrade`。

## 启动接入工程

```bash
cd config-center-access
export ACCESS_DB_DSN='root:password@tcp(127.0.0.1:3306)/config_access?parseTime=true&loc=UTC'
export CONFIG_CENTER_URL='http://127.0.0.1:8080'
export CONFIG_WEBHOOK_SECRET='replace-with-a-secure-secret'
go run ./cmd/config-center-access
```

接入服务默认监听 `:8081`，需要在配置中心为它注册订阅者并设置相同的 Webhook 密钥。详见 [接入端说明](config-center-access/README.md)。

## 管理页面

```bash
cd config-center-admin
npm ci
npm run dev
```

业务配置层级为 `game → region → server_group → channel_app`。同步协议采用 `epoch + revision` 和增量变更日志，Webhook 不直接传输配置，只触发接入端主动拉取。

**安全注意：** 管理接口没有账号权限系统，必须部署在可信内网或受保护网关。数据库迁移包含非事务性 MySQL DDL；升级旧库时必须先备份，失败后按照日志检查并恢复，不可盲目重试。

更多信息：[部署说明](docs/deployment.md) · [系统架构](docs/architecture.md) · [CI 回归](docs/ci.md)。

## 优雅停服与服务管理

三个 Go 服务统一响应 Ctrl+C 和 SIGTERM，接到停机信号后会切换就绪状态，等待已有 HTTP 请求完成，并等待配置同步 / Webhook 后台任务退出。Linux 生产环境推荐使用 systemd 管理服务，集群采用先摘流量再滚动更新的流程。详见 [优雅停机与启停管理](docs/graceful-shutdown.md)。
