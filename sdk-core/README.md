# sdk-core · SDK 核心服务

直接访问共享权威库 `config_center`，负责 SDK 配置查询、订单登记、平台支付回调和异步游戏发货。事件上报与订阅消息已归 `sdk-service` 处理。

## 主要入口

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/core/config/{env}/{appid}` | 获取 SDK 配置 |
| POST | `/api/core/orders/{env}/{appid}` | 登记游戏订单 |
| POST | `/api/core/pay/notify/{appid}` | 平台支付回调 |

游戏主动登记订单及中台发货通知均使用 HMAC-SHA-256，绑定方法、编码路径、AppID、时间戳、nonce 和原始请求体。代理不得改写签名路径；时间窗口不等于 nonce 防重放。

上述表格仅用于快速定位；完整字段、全部管理路由、签名、错误码和请求示例统一以 [接口文档](../admin/public/docs/index.html) 为准。

## 本地启动

```powershell
Copy-Item configs/config.yaml configs/config-dev.yaml
# 设置 database.dsn 指向 config_center
go run ./cmd/sdk-core -config configs/config-dev.yaml
```

发货使用 `orderNotify.workers`（默认 4，范围 1～32）设置单实例在途任务上限，空队列、候选页扫描结束或数据库失败后的等待时间固定为 `500ms`；在途任务完成会提前唤醒。调度先只读预览最多 128 个候选，取得目标额度后才按主键重新检查并领取，候选不持有租约。页内打散降低多实例争抢相同 ID，陈旧或锁定的候选跳过，保留分页进度与剩余候选；分页使用数据库时间固定扫描上界，持续新增任务不能阻止游标回到先前跳过的目标。修改配置后需重启 SDK Core。订单和调度共享现有 `database.maxOpenConns` 连接池，增加并发前应观察数据库连接等待和游戏通知耗时。

新库按 [数据库文档](../admin/public/docs/guide.html#database) 初始化，旧库手动检查增量 SQL；服务不会执行 DDL。订单与发货必须幂等，支付回调是否已成功完成需基于持久化状态判断。公开配置接口固定返回 `snKey`、`zoneKey` 和可选 `tsKey`，直接读取所需数据库字段，不再使用响应映射文件。需要增加公开字段时同步更新查询、DTO、测试和接口文档。

SDK 管理接口已迁移至独立的 `sdk-admin` 模块，路径为 `/api/sdk/admin/**`。SDK Core 不再加载管理密钥或注册管理路由。

## 并发保护与回退

- `configCache` 同时配置公开配置缓存和独立的订单配置缓存：默认 `ttl: 5s`、各最多 `4096` 条、各最多 `8` 个不同键同时回源、回源预算 `5s`。订单缓存的登记配置、平台验签密钥和游戏发货密钥共享一组容量与回源额度，不与脱敏后的公开缓存混用，键和值不记录日志。返回的 JSON 深拷贝，避免请求间互相污染。同键回源合并，不同键回源槽满时等待空位，等待可取消或超时，错误不缓存，过期不返回旧值；`ttl: 0s` 同时恢复两条链路的实时读取。
- 停用、解绑、公开配置修改及密钥轮换的可见性可能延迟一个 TTL 加在途读取时间，已取得的请求快照可能存活更久；现有三级节点 revision 不覆盖全部 SDK 配置，不作为即时失效保证。必须即时读取配置时关闭缓存；短缓存不是双密钥轮换机制。HTTP 响应仍是 `Cache-Control: no-store`。
- 不设置接口在途额度，不因并发数量主动拒绝配置、订单或支付请求。HTTP 处理预算固定为 `5s`，取消信号传播至数据库，不能代替 HTTP 读写超时或强制中断 handler。回源槽满时等待空位，同键仍合并；等待超过请求预算或数据库/下游故障仍可能返回错误，不能理解为请求一定成功。
- 发货默认复用 HTTP 连接：空闲总数 `32`、每目标空闲 `4`、每目标总连接 `4`、空闲超时 `30s`。新建连接仍校验全部解析 IP、阻止 metadata 地址且禁止重定向；连接复用不意味着每次请求重新解析 DNS。
- `orderNotify.hostConcurrency` 默认 `1`，按规范化主机名与端口隔离。目标饱和的候选直接跳过，不领取、不增加尝试号、不写延期、不读密钥、不发网络请求；分页会继续查看其他目标。取得额度后领取失败或候选已被其他实例处理时立即释放，发送结束后在结果保存前释放。`0` 关闭，多 Worker 时非零值须小于在途任务上限。这是进程内保护，不是跨实例全局限流，也不保证严格 FIFO。
- `notify_attempts` 仍是单调领取序号和 fencing token，不是精确的 HTTP 请求次数；正常调度不会因本地限流虚增。历史延期已增加的序号不回退，真实失败仍按该序号指数退避。领取在一个行锁事务内读取完整快照并更新租约，只执行一条 SELECT 和一条 UPDATE；另有按页摊销的只读候选查询。60 秒租约、旧结果隔离、支付幂等和至少一次发货语义不变，ACK 丢失仍可能重发，游戏必须幂等。没有数据库结构变更。

可调整参数见 [示例配置](configs/config.yaml)：`database.maxOpenConns`（默认 30）、`database.maxIdleConns`（默认 10）、`orderNotify.workers`、`orderNotify.hostConcurrency`、`configCache.ttl`（默认 5s）、`configCache.maxEntries`（默认 4096）、`configCache.maxInflight`（默认 8）和日志级别/访问日志开关。数据库连接生命周期、回源超时及慢请求阈值等保留固定代码默认值，不再接受对应 YAML 字段和 `LOG_FORMAT` / `LOG_OUTPUT` / `LOG_SLOW_THRESHOLD` 环境变量。修改后重启生效，不自动更改私有配置或线上部署。部署新版本前须按 [配置迁移说明](../ops/CONFIGURATION-MIGRATION.md) 清理线上旧字段。灰度时结合成功吞吐、P99、DB 连接等待、缓存回源等待、通知积压年龄和下游超时逐步调整资源，不能视为百万级容量承诺。

数据库集成测试可能 DROP/CREATE 表，仅能在确认可销毁的隔离库运行，不能使用开发或生产库的测试 DSN。普通回归应跳过此类测试；`-race` 需要可用的 CGO/C 编译器。

```bash
go test -race ./...
go vet ./...
go build ./cmd/sdk-core
```

[架构](../admin/public/docs/guide.html#architecture) · [本地开发](../admin/public/docs/guide.html#development) · [生产运维](../admin/public/docs/guide.html#production)

## 订单管理

订单管理页面与管理 API 已迁移至独立的 `sdk-admin` 模块。订单登记、支付确认、游戏发货及持久化补偿 Worker 继续由 SDK Core 承担。手动补偿由 SDK Admin 更新任务调度时间，SDK Core Worker 读取同一数据库后执行。


## SDK 配置热点路径
生产模板设置 `log.accessEnabled: false`：普通成功的 HTTP 访问日志默认不输出；请求错误、panic 和慢请求仍记录，`X-Request-ID` 照常返回。若显式开启 `accessEnabled: true`，所有 HTTP 访问均记录，不再配置按路由采样；请避免在高 QPS 正式环境长期开启。订单登记、支付确认和发货结果的业务日志由各自业务逻辑控制，不受访问日志开关影响。删除了旧的 `sdkConfigSampleEvery` / `LOG_SDK_CONFIG_SAMPLE_EVERY`，私有 YAML 和环境变量需同步清理，配置变更后重启生效。
