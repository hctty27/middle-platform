# sdk-service · SDK 业务转发服务

直接只读访问共享权威库 `config_center`。承担平台 → 游戏的角色查询、区服查询、封禁/解禁，以及游戏 → 平台的用户核验、事件上报与订阅消息推送。业务请求不落库、不自动重试。

## 业务入口

| 方法 | 路径 | 方向 |
| --- | --- | --- |
| POST | `/api/service/user/check/{env}` | 游戏 → 平台 |
| POST | `/api/service/role/query/{env}` | 平台 → 游戏 |
| POST | `/api/service/server/query/{env}` | 平台 → 游戏 |
| POST | `/api/service/role/control/{env}` | 平台 → 游戏 |
| POST | `/api/service/events/{env}` | 游戏 → 平台 |
| POST | `/api/service/subscribe/{env}` | 游戏 → 平台 |

AppID 来自请求头 `x-app-id`。游戏节点的 `gameSecret`、`serviceAdapters` 和大区的业务回调 URL 位于 `sdk_node_config`，由 SDK Core 管理，不在配置中心基础节点 JSON 中配置；渠道包的平台密钥来自 `sdk_channel_app.config.appSecret`。

角色查询、区服查询和封解禁均默认使用内置 `standard` 适配器，无需额外配置。只有特殊游戏需要在对应游戏 SDK 节点配置中显式选择已注册适配器。这些服务及事件、订阅功能依赖全局及渠道环境开关。用户核验的全局与渠道环境开关默认开启；关闭任一开关将跳过平台核验并直接返回成功，但仍校验游戏请求签名。关闭意味着不再验证平台用户真实性，应谨慎使用。

游戏与中台之间的用户核验、事件、订阅及游戏业务回调使用 HMAC-SHA-256，绑定方法、编码路径、AppID、时间戳、nonce 和原始请求体；平台方向仍使用其现有协议。游戏回调 URL 不允许查询串。代理不得改写签名路径；时间窗口不等于 nonce 防重放。

协议参数、签名算法、请求示例及响应结构不在此 README 重复，统一见 [游戏接口](../admin/public/docs/index.html#g-start)、[平台接口](../admin/public/docs/index.html#p-start)、[签名规范](../admin/public/docs/index.html#sig-overview)。

## 本地启动

```powershell
Copy-Item configs/config.yaml configs/config-dev.yaml
# 配置 database.dsn 指向 config_center（只读账号）
go run ./cmd/sdk-service -config configs/config-dev.yaml
```

默认端口 `:8221`。首次部署或升级前按 [数据库文档](../admin/public/docs/guide.html#database) 核对功能服务表与增量 SQL；平台事件/订阅目标 URL 在 `platform.eventReportURL`、`platform.subscribeMessageURL` 和 `platform.userCheckURL` 配置；核验平台路径为 `/cp/user/check`。旧的 `/api/core/events/{env}` 不再存在。

## 并发保护与缓存一致性

- 不设置六类业务接口的在途额度，不因并发数量主动拒绝请求。`server.requestTimeout: auto` 默认总预算为 `max(game.timeout, 平台固定 5s) + 3s`（默认 `8s`），也可显式设置不超过 `35s` 的正时长；下游单次超时保留原值。context 取消不是强制结束 handler，数据库或下游超时仍可能返回错误。
- `configCache.ttl` 默认 `0s`，保持实时查库；路由含上下游密钥、启停和用户核验开关，**只有接受旧密钥撤销、停用和核验开关变更的短暂延迟后才能开启**，例如 `1s`。TTL 上限 `5s`，实际可见延迟还包含在途读取时间，不保证即时撤销。
- 开启后最多缓存 `4096` 条，各服务命名空间合计；最多 `8` 个不同键回源、单次预算 `5s`，同键合并，回源槽满时等待空位，等待受请求取消与超时约束。仅缓存成功值，过期回源失败不使用旧值兜底；TTL 不滑动延长。键隔离 AppID、环境、服务类型，用户核验保留原缺省开关语义。
- 三级节点 revision 不覆盖渠道、SDK 配置和服务开关的全部修改，不能依赖它保证缓存即时失效。`ttl: 0s` 可回退；不会改变同步接口成功含义，不增加消息队列、落库或自动重试。

新参数见 [示例配置](configs/config.yaml)，通过单份 YAML 调整，现有 DSN/监听/日志环境变量覆盖行为不变，修改后需重启。应按 DB 连接等待、缓存回源等待、下游延迟和 P99 测量调节资源；移除主动拒绝不代表无限处理能力，高峰时可能排队或超时，不能外推为百万级容量。具体灰度与回退边界见 [并发保护运维说明](../admin/public/docs/guide.html#production-capacity)。

```bash
go test -race ./...
go vet ./...
go build ./cmd/sdk-service
```

[架构与内部路由](../admin/public/docs/guide.html#architecture) · [本地开发](../admin/public/docs/guide.html#development) · [生产部署与迁移](../admin/public/docs/guide.html#production)
