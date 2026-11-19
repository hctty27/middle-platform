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

事件上报仅同步转发：中台先校验渠道配置和游戏签名，再等待平台返回 HTTP 200 且业务 `code=0`、`data=true`，随后向游戏返回 `200`（`code: 0, message: "success", data: true`）。平台失败或超时按原有错误映射响应，不入库、不入队、不自动重试。`X-Report-Mode` 已不再提供异步模式；为兼容已有调用，仍接受 `X-Report-Mode: sync`，传 `async`、其他值或重复 Header 返回 `400`。订阅消息行为不变。

其余协议参数、签名算法、请求示例及响应结构统一见 [游戏接口](../admin/public/docs/index.html#g-start)、[平台接口](../admin/public/docs/index.html#p-start)、[签名规范](../admin/public/docs/index.html#sig-overview)。

## 本地启动

```powershell
Copy-Item configs/config.yaml configs/config-dev.yaml
# 配置 database.dsn 指向 config_center（只读账号）
go run ./cmd/sdk-service -config configs/config-dev.yaml
```

默认端口 `:8221`。首次部署或升级前按 [数据库文档](../admin/public/docs/guide.html#database) 核对功能服务表与增量 SQL；平台事件/订阅目标 URL 在 `platform.eventReportURL`、`platform.subscribeMessageURL` 和 `platform.userCheckURL` 配置；核验平台路径为 `/cp/user/check`。旧的 `/api/core/events/{env}` 不再存在。

## 并发保护与缓存一致性

- 不设置六类业务接口的在途额度，不因并发数量主动拒绝请求。HTTP 预算固定按 `max(game.timeout, 平台固定 5s) + 3s` 自动计算（默认 `8s`），不再接受 `server.requestTimeout` 显式覆盖；下游单次超时保留原值。context 取消不是强制结束 handler，数据库或下游超时仍可能返回错误。
- `configCache.ttl` 默认 `5s`，即默认缓存路由配置；路由含上下游密钥、启停和用户核验开关，**旧密钥撤销、服务停用和核验开关变更存在延迟生效窗口**。如要求实时校验应显式设为 `0s`。TTL 上限 `5s`，实际可见延迟还包含在途读取时间，不保证即时撤销。
- 开启后最多缓存 `4096` 条，各服务命名空间合计；最多 `8` 个不同键回源、单次预算 `5s`，同键合并，回源槽满时等待空位，等待受请求取消与超时约束。仅缓存成功值，过期回源失败不使用旧值兜底；TTL 不滑动延长。键隔离 AppID、环境、服务类型，用户核验保留原缺省开关语义。
- 三级节点 revision 不覆盖渠道、SDK 配置和服务开关的全部修改，不能依赖它保证缓存即时失效。`ttl: 0s` 可回退；不会改变同步接口成功含义，不增加消息队列、落库或自动重试。

可调整参数见 [示例配置](configs/config.yaml)：数据库最大/空闲连接数、游戏超时、缓存 TTL/容量/回源并发、平台 URL、`platform.maxIdlePerHost` 与 `platform.eventMaxConns`、日志级别和访问日志开关。连接池隔离模式固定开启，HTTP 总超时自动计算，缓存回源预算固定 5s；日志环境变量仅支持 `LOG_LEVEL` 和 `LOG_ACCESS_ENABLED`，修改后需重启。部署新版本前须按 [配置迁移说明](../ops/CONFIGURATION-MIGRATION.md) 清理线上旧字段。应按 DB 连接等待、缓存回源等待、下游延迟和 P99 测量调节资源；移除主动拒绝不代表无限处理能力，高峰时可能排队或超时，不能外推为百万级容量。具体灰度与回退边界见 [并发保护运维说明](../admin/public/docs/guide.html#production-capacity)。

```bash
go test -race ./...
go vet ./...
go build ./cmd/sdk-service
```

[架构与内部路由](../admin/public/docs/guide.html#architecture) · [本地开发](../admin/public/docs/guide.html#development) · [生产部署与迁移](../admin/public/docs/guide.html#production)

## 平台连接池与事件日志

用户核验使用独立 HTTP transport，事件/订阅共用另一 transport。默认每目标最大空闲连接数 32（`platform.maxIdlePerHost` 可调 1～256），事件每目标连接数上限 0（`platform.eventMaxConns` 可调 0～4096，0 表示不额外限制）；不主动拒绝、不新增重试。两池仍共享进程 CPU、数据库和网络，不是完整隔离。

`log.accessEnabled: false` 默认关闭正常的 HTTP 入站成功访问日志，以及平台出站事件上报、用户核验和订阅消息的快速成功日志；HTTP 异常、平台失败和超过 `slowThreshold` 的慢调用照常记录。`accessEnabled: true` 则开启正常请求日志全量输出。订单、支付、发货、配置同步等独立业务日志不受此开关控制。原有全部 `*SampleEvery` 参数已删除，生产私有 YAML 必须移除旧字段，并清理旧的 `LOG_*_SAMPLE_EVERY`、`LOG_FORMAT`、`LOG_OUTPUT`、`LOG_SLOW_THRESHOLD` 环境变量（残留时会在配置校验阶段报错）。DEBUG 请求/响应内容维持现有行为，配置缓存默认 5s；更新线上密钥或开关时要注意缓存可见性延迟。
