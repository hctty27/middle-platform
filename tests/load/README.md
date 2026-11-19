# Runner 本地压测

全部业务程序、MySQL、Nginx、Supervisor、模拟发行平台/游戏和 k6 均运行在同一 GitHub 托管 Ubuntu Runner，不使用生产地址、密钥、数据，也不参与正式发布。脚本拒绝在非 GitHub 托管 Runner 执行。业务 API 和数据库结构不修改。

## 逐接口固定 QPS 阶梯（Endpoint QPS Ladder）

在原有 **Runner Load Test** 工作流中选择 profile=endpoint-qps，只用 GitHub 托管 Ubuntu Runner，启动临时 MySQL、Nginx、Supervisor、配置中心、SDK Core、SDK Service、配置接入端和本地签名 Mock；不访问任何外部业务域名。标题包含 QPS 的 PR 会额外跑全部接口基础档，其他 PR 保留原有快速回归；手动工作流提供这些输入：

- group：quick（单接口快速验证）/ read（32 个只读或查询类场景）/ write（20 个写入、上报、回调场景）/ all（52 类路由）。
- tiers：basic 读 50/200/500 QPS、写 10/30/60 QPS；high 读 100/500/1000/3000/5000/10000/20000 QPS、写 20/50/100/250 QPS。每个接口前一档失败就停止升级、保留失败指标；quick 固定 50/200/500 QPS。高档仅为探索目标。
- topology：compact（2/1/1）或 cluster（2/4/6），每实例 GOMEMLIMIT=128MiB。
- seconds：每档 3–15 秒。endpoint：可填 tests/load/endpoint-qps-catalog.json 中某个接口 ID 单独测试。

执行前先跑一次 api-coverage.js，要求 52/52 合约成功；之后单接口逐档固定 arrival-rate，**每次迭代只发一次被测 API 请求**。记录目标 QPS、实际 HTTP/s、P95、P99、业务失败率、未启动迭代、MySQL 行锁等待增量和进程 CPU/RSS；导出 endpoint-qps-summary.csv/json 与各档原始日志。

合格门槛：错误率小于 1%，P95 小于 500ms，P99 小于 1500ms，完成请求达到计划的 99%，丢弃率不超过 0.1%。最低档失败使任务失败；高档失败记录容量边界并停止加压。最高通过档仅代表此 Runner、此测试时长下通过的**已测试目标**，不是准确容量上限。

写接口有明确数据边界：创建操作每请求生成唯一对象；删除操作每档预置独立临时行；更新使用可重复的测试数据；支付回调只测**已支付订单重复回调**；订单重试只测**不可重试订单返回 409**。后两项不代表首次支付或成功补偿的吞吐。Mock 链路和单机 Runner 的数据也不能当作生产环境最大性能。

## 全接口覆盖（api-coverage）

Actions → Runner Load Test → Run workflow：选择 profile=api-coverage、topology=compact、scenario=all。该任务只在 GitHub 托管 Ubuntu Runner 上运行，所有 HTTP 目标固定为 127.0.0.1。它会启动配置中心、SDK Core、SDK Service、Nginx、MySQL、模拟游戏/平台，以及带独立临时 config_access 数据库的 config-center-access。

- 通过 k6 api-coverage.js 验证当前注册的 **52 种 HTTP 方法 + 路由组合**（包含 4 个服务的健康检查）；每个路由必须有可判定的 HTTP/业务结果，不把 401/404 当成成功压测。
- 覆盖完整配置同步、基础配置增删改查、订阅者管理、SDK 节点配置与渠道包、服务开关、订单管理、支付回调、角色/区服查询、封解禁、事件、订阅消息、接入端签名查询与 Webhook。
- 使用一次性合成游戏/大区/区服组/渠道/订单，CRUD 在同次运行中有序创建、读取和清理；对本地模拟游戏/发行平台执行真实签名和协议校验；接入端需要在回调后追平版本。
- tests/load/test_api_coverage.py 对照 Go 实际注册路由与 k6 覆盖调用；新增/删除 HTTP 路由若未同步维护压测场景，CI 直接失败。
- 此 profile 是**全接口正确性和覆盖率检查**，不是把所有危险的增删改操作同时放大到高 QPS。原有 stress/traffic/capacity/soak 才提供高频性能数据；后续可针对具体读写链路做独立、可重复的负载档位。
- 对于订单补偿 API，目前覆盖已完成订单不可重试的 409 条件；符合条件的成功补偿和 Worker 时序需通过现有订单单测和专项集成测试确认，不应把 409 冒充成功业务补偿。

## 运行
在 Actions → Runner Load Test → Run workflow 选择参数。涉及服务、共享模块、数据库、Nginx 或压测脚本的 PR 自动执行 compact/cluster smoke 与 compact traffic-quick；完整压力测试按需手动触发。

- profile: api-coverage（52 类接口覆盖和业务断言，仅单次）、smoke（5 次迭代/s × 10 秒）、traffic-quick（四档 × 10 秒）、baseline（25/50/100 × 30 秒）、stress（100/250/500/1000 × 30 秒）、capacity、traffic、soak（50 次迭代/s × 30 分钟）、soak2h（50 次迭代/s × 2 小时）。capacity / traffic / traffic-quick 固定场景，忽略 scenario；soak 默认 mixed。
- topology: compact (2/1/1) 或 cluster (2/4/6)，both 对比二者，每个 Go 实例 GOMEMLIMIT=128MiB（软目标）。
- scenario: all（逐项运行）或 config-sync、sdk-config、user-check、order、duplicate、payment、mixed。所有场景固定访问 127.0.0.1，不能指定外部地址。

配置同步读 revision；SDK 配置读热点渠道；用户核验真实经过 DB、验签和本地模拟发行平台；订单写入临时 DB；重复登记走真实幂等接口；payment 先登记再发送平台 V1 回调，最后检验持久化发货状态；mixed 轮流执行以上业务。支付与重复订单每次迭代最多两次 HTTP 请求，因此迭代速率不是 HTTP QPS。

## 结果
Actions Summary 显示每档实际 HTTP QPS、P50/P95/P99、业务失败率、丢弃迭代和退出码。摘要同时打印到 Actions 日志。Artifacts 保留 1 天，尽力上传；额度不足仅警告。每档采集 MySQL 状态与 Performance Schema SQL 摘要差值、vmstat 和 iostat。初始门槛为 P95 < 500ms、P99 < 1500ms、HTTP 和业务失败率 < 1%、非持续档位无丢弃迭代；不是线上 SLA。

局限：标准私有仓库 Runner CPU 有限、压测与被测程序同机竞争资源；本地 HTTP 不含公网/TLS；单一游戏、渠道与 mock 不代表真实分布；常规压力 profiles 只覆盖各自列出的热点业务；api-coverage 单独覆盖当前全部 HTTP 路由，不对 CRUD 执行高并发，也不覆盖故障恢复及滚动更新。实际线上容量需独立压测机和隔离环境验证。对比版本时固定 topology/profile/scenario 并重复运行。

高档位是探索目标，不是已验证容量；丢弃迭代可能来自负载发生器资源不足或被测服务慢。历史 PR 的订单 stress 曾复用 smoke 后的数据库，与独立运行不可直接比较。

## 快速回归与按需压测
PR 默认两个拓扑的 smoke（8 个场景各 10 秒）和 compact traffic-quick（配置/核验/上报 100/100/200 QPS，正常、10ms 延迟、平台故障、恢复各 10 秒）。各任务独立 Runner、阈值与 Mock 签名检查不放宽，支付仍等待持久化发货清空。完整 stress、capacity、traffic A/B、30 分钟 soak、2 小时 soak2h 仅手动运行；建议重大性能调整、正式发布前按需执行。手动工作流与 PR 使用不同并发组，避免 PR 更新取消手动长测；同一 PR 的旧任务仍自动取消。没有定时任务。
SDK 配置 stress 增加 2000/3000/5000 档，其他场景最高 1000；每档失败停止该场景加压。每档采样 DB、CPU 和 RSS；不能仅凭 RSS 变化判定内存泄漏。完整测试与历史测试保持相同门槛。
## 稳定性测试阈值
soak 30 分钟和手动 soak2h 2 小时均为 50 次业务迭代/s，默认 mixed；预分配 50 VU，最高 100 VU。按计划迭代数允许最多 0.1% 未启动迭代（30 分钟 90 次、2 小时 360 次），其余 HTTP/业务失败率和延迟阈值不变；smoke、baseline、stress 仍要求零丢弃。报告额外显示实际丢弃比例和失败 HTTP 状态分类，不将丢弃当作成功请求。30 分钟和 2 小时档位均仅手动运行，且仍使用历史 mixed（不含事件上报）；并非自动 PR 的一部分。

## 容量探索 capacity（向 10 万 QPS 目标推进）
手动选择 capacity 运行，默认 compact，不再每个 PR 自动执行。固定 SDK 热点接口对比 Nginx 与单实例直连，1000/3000/5000/7500 次/s 各 20 秒，10000 次/s 冲击 15 秒并回落至 1000 次/s。高档位失败记录但不阻塞后续；1000 档、恢复及独立 profiling 阶段必须通过。capacity 固定执行这套测试，忽略 scenario 参数；cluster 的直连只请求首个 SDK Core，不能与完整集群视为等价。仅 capacity 使用 `-tags loadtest`，独立回环监听 pprof 和 DB.Stats；正常构建为 no-op，不增加业务路由。CPU、Heap、Mutex、Block 在单独的 profiling 流量阶段采集，文本 Top 及连接池和 SQL 事件差值写入日志/摘要，原始文件尽力上传。Mutex/Block 采样为 capacity 进程全程启用，有额外开销；样本量不足可能出现空 profile。Runner 内 k6 和被测服务竞争 CPU，本轮目标不是实际 10000 QPS 保证，更不能作为 100000 并发的证明。

## 第六轮对照实验
单 Runner 先比较 SDK Core HTTP 成功访问日志开启/关闭（各 Nginx、直连 3000 迭代/s），再运行原 1000–10000 阶梯。Nginx SDK Core upstream 空闲 keepalive 64，其他 16，访问日志 64 KiB 缓冲、1 秒刷新；连接上限和业务重试规则不变。订单登记 500 迭代/s 对比 20/10 与 32/16 数据库最大/空闲连接池，独立测试用配置并重启，不修改线上池默认值。对照档位为探索，原必测基线和恢复阈值不放宽。访问日志开关及连接池差异以同 Runner 测量为准，仍可能有时间顺序与噪声影响。

## 第七轮：真实高频流量与抗干扰（traffic）
新增 event-report 单接口（`POST /api/service/events/test`），使用真实游戏 HMAC、平台 Header 签名及本地 Mock `/report/event`。原 mixed 保持历史 6 场景配比，方便跨轮对比；新增 traffic 使用三个独立 constant-arrival-rate 场景，配置查询:用户核验:事件上报=1:1:2，从 100/100/200 到 5000/5000/10000（单 Runner 探索，失败停止升档）。独立配置 P95/P99 20/50ms、核验 100/250ms、上报 500/1500ms；错误率 <1%、丢弃预算 0.1%、完成数至少计划 99.9%。Mock 注入仅事件 1/10/50/100ms 和 HTTP 503，验证核心接口独立阈值及恢复，并校验实际平台签名到达计数，避免跳过上游造成虚假成功。低档基线、事件冒烟、1/10ms、故障及恢复为必测；高档和 50/100ms 为探索。生产代码、缓存、签名与部署不变。100ms 延迟和更高吞吐可能受同机器 k6/VU 容量限制。

压测数据显式启用 `load-app/test` 的 `event_report` 与 `user_check`，启动前校验两个绑定；仅本地一次性数据库。容量脚本按 `database` / `log` 两个顶层配置段修改连接池和 `accessEnabled` 开关，并通过真实 YAML 模板回归测试，保证 `orderNotify.maxIdleConns` 不受影响。

## SDK Service A/B 对照

traffic 先比较共享连接池(每目标空闲2)、独立池(32)、统一平台出站成功日志采样1/10、路由 TTL 1s、可选事件每目标连接数64（50ms平台延迟冲击）。每次仅修改隔离 Runner 的 YAML 并重启 SDK Service，保留业务验签、真实 Mock 到达计数、各接口阈值；A/B 为探索，之后恢复默认 TTL=0s、平台成功采样=1、独立池32、无连接上限并执行原必测场景。不假定 CPU/DB 已隔离，不将 HTTP/2 连接上限视为请求并发限制。

## 第九轮：多游戏多渠道与容量对照
`traffic-multi` 手动档模拟 16 个游戏、256 个渠道包（各游戏独立 GameSecret，每渠道独立 AppSecret），按均匀轮转选择渠道，固定 1:1:2，比较 TTL=0s/1s，均使用平台出站成功日志采样10、独立连接池32、不限制事件连接；再探索总 2000/3000/5000 QPS，失败停止升档，低档和恢复必须通过。原单热点、订单和支付场景不变。日常 `traffic-quick` 仅额外执行 10 秒 16 渠道回归，不开启长测。所有游戏、密钥和 SQL 均为临时 Runner 合成数据，不使用生产。该档使用 loadtest 构建与独立回环诊断端口，报告每阶段 DB 连接等待差值、GC 次数/暂停、堆内存和 goroutine 数，并保留 CPU/RSS、MySQL 计数；pprof 采样有开销，不能视为正式配置容量。正式构建无诊断接口。

独立 Windows 压测见 [执行手册](INDEPENDENT.md)。与仅回环 Runner 脚本隔离，须手动指定授权测试 HTTPS 入口和仓库外测试渠道 JSON，逐档人工升压；CI 不运行该脚本。
