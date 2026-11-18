# sdk-core · SDK 核心服务

直接访问共享权威库 `config_center`，负责 SDK 节点配置、渠道包与功能开关管理，以及配置查询、订单登记、平台支付回调和异步游戏发货。事件上报与订阅消息已归 `sdk-service` 处理。

## 主要入口

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/core/config/{env}/{appid}` | 获取 SDK 配置 |
| POST | `/api/core/orders/{env}/{appid}` | 登记游戏订单 |
| POST | `/api/core/pay/notify/{appid}` | 平台支付回调 |
| GET/PUT | `/api/core/admin/node-configs/{id}` | SDK 节点配置管理 |
| GET/POST | `/api/core/admin/channel-apps` | 渠道包查询与新增 |
| GET/PUT | `/api/core/admin/services[/{id}]` | 服务开关查询与更新 |
| GET | `/api/core/admin/orders` | 按游戏查询订单（游标分页） |
| GET | `/api/core/admin/orders/stats` | 今日订单统计及历史发货积压 |
| GET | `/api/core/admin/orders/{id}` | 订单详情 |
| POST | `/api/core/admin/orders/{id}/retry` | 提前安排失败订单补偿 |

游戏主动登记订单及中台发货通知均使用 HMAC-SHA-256，绑定方法、编码路径、AppID、时间戳、nonce 和原始请求体。代理不得改写签名路径；时间窗口不等于 nonce 防重放。

上述表格仅用于快速定位；完整字段、全部管理路由、签名、错误码和请求示例统一以 [接口文档](../admin/public/docs/index.html) 为准。

## 本地启动

```powershell
Copy-Item configs/config.yaml configs/config-dev.yaml
# 设置 database.dsn 指向 config_center
go run ./cmd/sdk-core -config configs/config-dev.yaml
```

发货使用 `orderNotify.workers`（默认 4，范围 1～32）设置单实例在途任务上限，`orderNotify.idleInterval`（默认 `500ms`，范围 `100ms～10s`）设置空队列、候选页扫描结束或数据库失败后的等待时间；在途任务完成会提前唤醒。调度先只读预览最多 128 个候选，取得目标额度后才按主键重新检查并领取，候选不持有租约。页内打散降低多实例争抢相同 ID，陈旧或锁定的候选跳过，保留分页进度与剩余候选；分页使用数据库时间固定扫描上界，持续新增任务不能阻止游标回到先前跳过的目标。修改配置后需重启 SDK Core。订单和调度共享现有 `database.maxOpenConns` 连接池，增加并发前应观察数据库连接等待和游戏通知耗时。

新库按 [数据库文档](../admin/public/docs/guide.html#database) 初始化，旧库手动检查增量 SQL；服务不会执行 DDL。订单与发货必须幂等，支付回调是否已成功完成需基于持久化状态判断。公开配置接口固定返回 `snKey`、`zoneKey` 和可选 `tsKey`，直接读取所需数据库字段，不再使用响应映射文件。需要增加公开字段时同步更新查询、DTO、测试和接口文档。

`/api/core/admin/**` 始终使用 HMAC-SHA256，密钥为 `adminAuth.secret`，至少 32 字节，并与配置中心使用相同管理密钥；生产仍需网关隔离并启用 HTTPS，不应公开暴露管理路由。

## 并发保护与回退

- `configCache` 同时配置公开配置缓存和独立的订单配置缓存：默认 `ttl: 1s`、各最多 `4096` 条、各最多 `8` 个不同键同时回源、回源预算 `5s`。订单缓存的登记配置、平台验签密钥和游戏发货密钥共享一组容量与回源额度，不与脱敏后的公开缓存混用，键和值不记录日志。返回的 JSON 深拷贝，避免请求间互相污染。同键回源合并，不同键回源槽满时等待空位，等待可取消或超时，错误不缓存，过期不返回旧值；`ttl: 0s` 同时恢复两条链路的实时读取。
- 停用、解绑、公开配置修改及密钥轮换的可见性可能延迟一个 TTL 加在途读取时间，已取得的请求快照可能存活更久；现有三级节点 revision 不覆盖全部 SDK 配置，不作为即时失效保证。必须即时读取配置时关闭缓存；短缓存不是双密钥轮换机制。HTTP 响应仍是 `Cache-Control: no-store`。
- 不设置接口在途额度，不因并发数量主动拒绝配置、订单或支付请求。`server.requestTimeout` 默认 `5s`，取消信号传播至数据库，不能代替 HTTP 读写超时或强制中断 handler。回源槽满时等待空位，同键仍合并；等待超过请求预算或数据库/下游故障仍可能返回错误，不能理解为请求一定成功。
- 发货默认复用 HTTP 连接：空闲总数 `32`、每目标空闲 `4`、每目标总连接 `4`、空闲超时 `30s`；`orderNotify.disableKeepAlives: true` 可回退。新建连接仍校验全部解析 IP、阻止 metadata 地址且禁止重定向；连接复用不意味着每次请求重新解析 DNS。
- `orderNotify.hostConcurrency` 默认 `1`，按规范化主机名与端口隔离。目标饱和的候选直接跳过，不领取、不增加尝试号、不写延期、不读密钥、不发网络请求；分页会继续查看其他目标。取得额度后领取失败或候选已被其他实例处理时立即释放，发送结束后在结果保存前释放。`0` 关闭，多 Worker 时非零值须小于在途任务上限。这是进程内保护，不是跨实例全局限流，也不保证严格 FIFO。
- `notify_attempts` 仍是单调领取序号和 fencing token，不是精确的 HTTP 请求次数；正常调度不会因本地限流虚增。历史延期已增加的序号不回退，真实失败仍按该序号指数退避。领取在一个行锁事务内读取完整快照并更新租约，只执行一条 SELECT 和一条 UPDATE；另有按页摊销的只读候选查询。60 秒租约、旧结果隔离、支付幂等和至少一次发货语义不变，ACK 丢失仍可能重发，游戏必须幂等。没有数据库结构变更。

全部新参数见 [示例配置](configs/config.yaml)，仅通过这份 YAML 调整，现有 DSN/监听/日志环境变量仍保留原覆盖行为。缺省字段采用上述默认值；修改后重启生效，不自动更改私有配置或线上部署。灰度时结合成功吞吐、P99、DB 连接等待、缓存回源等待、通知积压年龄和下游超时逐步调整资源，不能视为百万级容量承诺。

数据库集成测试可能 DROP/CREATE 表，仅能在确认可销毁的隔离库运行，不能使用开发或生产库的测试 DSN。普通回归应跳过此类测试；`-race` 需要可用的 CGO/C 编译器。

```bash
go test -race ./...
go vet ./...
go build ./cmd/sdk-core
```

[架构](../admin/public/docs/guide.html#architecture) · [本地开发](../admin/public/docs/guide.html#development) · [生产运维](../admin/public/docs/guide.html#production)

## 订单管理

订单管理复用 `game_order`、管理 HMAC 和现有 Worker。列表不指定日期时查询全部历史订单，每页 20 条（最多 100）；日期区间按北京时间展示和选择，不限制跨度，显式传入日期须成对且结束晚于开始；所有查询必填 `gameId`，环境和 AppID 可筛选。统计仅按游戏、环境、AppID，今日支付基于 `paid_at`，积压含历史订单，进程内缓存 60 秒（最多 256 个键）。四个接口共用独立的每实例 2 个在途额度和 3 秒请求预算，不改变公开接口的并发策略。429 表示订单管理繁忙，503 表示数据库或查询不可用。

手动补偿只能提前调度已支付、通知失败且尚未到期的订单；最近一次领取后 60 秒内禁止提前调度。条件更新与 Worker 领取互斥，不直接发货、不改变金额、支付状态或尝试序号。成功通知是终态；游戏仍须幂等处理。日志记录请求来源及调度结果，共享管理密钥不提供独立人员身份。已有库低峰手工审核执行 `db/incremental/V0021__config_center_order_admin_indexes.sql`，新库使用全量 SQL。

## SDK 配置热点路径
`log.sdkConfigSampleEvery` / `LOG_SDK_CONFIG_SAMPLE_EVERY` 控制快速成功的公开配置 GET 访问日志；模板为 10（每 10 条保留 1 条），缺省或 0/1 保留全量。错误、慢请求及订单/支付/管理请求不采样；Request ID 仍照常返回。修改已有私有配置后重启才生效。成功公开配置的 JSON 响应与 DTO 使用同一个缓存条目和 TTL，保留 `no-store`、原有字段及换行，不增加配置失效延迟。
