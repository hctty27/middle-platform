# sdk-service · SDK 业务转发服务

直接只读访问共享权威库 `config_center`。承担平台 → 游戏的用户核验、角色查询、区服查询、封禁/解禁，以及游戏 → 平台的事件上报与订阅消息推送。业务请求不落库、不自动重试。

## 业务入口

| 方法 | 路径 | 方向 |
| --- | --- | --- |
| POST | `/api/service/user/check/{env}` | 平台 → 游戏 |
| POST | `/api/service/role/query/{env}` | 平台 → 游戏 |
| POST | `/api/service/server/query/{env}` | 平台 → 游戏 |
| POST | `/api/service/role/control/{env}` | 平台 → 游戏 |
| POST | `/api/service/events/{env}` | 游戏 → 平台 |
| POST | `/api/service/subscribe/{env}` | 游戏 → 平台 |

AppID 来自请求头 `x-app-id`。游戏节点的 `gameSecret`、`serviceAdapters` 和大区的业务回调 URL 位于 `sdk_node_config`，由 SDK Core 管理，不在配置中心基础节点 JSON 中配置；渠道包的平台密钥来自 `sdk_channel_app.config.appSecret`。

用户核验、角色查询、区服查询和封解禁均默认使用内置 `standard` 适配器，无需额外配置。只有特殊游戏需要在对应游戏 SDK 节点配置中显式选择已注册适配器。功能生效还依赖全局及渠道环境开关。

协议参数、签名算法、请求示例及响应结构不在此 README 重复，统一见 [游戏接口](../docs/index.html#g-start)、[平台接口](../docs/index.html#p-start)、[签名规范](../docs/index.html#sig-overview)。

## 本地启动

```powershell
Copy-Item configs/config.yaml configs/config-dev.yaml
# 配置 database.dsn 指向 config_center（只读账号）
go run ./cmd/sdk-service -config configs/config-dev.yaml
```

默认端口 `:8221`。首次部署或升级前按 [数据库文档](../docs/guide.html#database) 核对功能服务表与增量 SQL；平台事件/订阅目标 URL 在 `platform.eventReportURL` 和 `platform.subscribeMessageURL` 配置。旧的 `/api/core/events/{env}` 不再存在。

```bash
go test -race ./...
go vet ./...
go build ./cmd/sdk-service
```

[架构与内部路由](../docs/guide.html#architecture) · [本地开发](../docs/guide.html#development) · [生产部署与迁移](../docs/guide.html#production)
