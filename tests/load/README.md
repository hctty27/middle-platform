# Runner 本地压测

全部业务程序、MySQL、Nginx、Supervisor、模拟发行平台/游戏和 k6 均运行在同一 GitHub 托管 Ubuntu Runner，不使用生产地址、密钥、数据，也不参与正式发布。脚本拒绝在非 GitHub 托管 Runner 执行。业务 API 和数据库结构不修改。

## 运行
在 Actions → Runner Load Test → Run workflow 选择参数。压测相关 PR 自动运行两个拓扑的 smoke/stress 和独立 soak（见下文）；其他业务提交不自动触发此工作流。

- profile: smoke (5 次迭代/s × 10 秒)、baseline (25/50/100 × 30 秒)、stress (100/250/500/1000 × 30 秒)。以及 soak (50 次迭代/s × 30 分钟，all 默认 mixed)。每个场景独立阶梯，失败即停止该场景继续加压。
- topology: compact (2/1/1) 或 cluster (2/4/4)，both 对比二者，每个 Go 实例 GOMEMLIMIT=128MiB（软目标）。
- scenario: all（逐项运行）或 config-sync、sdk-config、user-check、order、duplicate、payment、mixed。所有场景固定访问 127.0.0.1，不能指定外部地址。

配置同步读 revision；SDK 配置读热点渠道；用户核验真实经过 DB、验签和本地模拟发行平台；订单写入临时 DB；重复登记走真实幂等接口；payment 先登记再发送平台 V1 回调，最后检验持久化发货状态；mixed 轮流执行以上业务。支付与重复订单每次迭代最多两次 HTTP 请求，因此迭代速率不是 HTTP QPS。

## 结果
Actions Summary 显示每档实际 HTTP QPS、P50/P95/P99、业务失败率、丢弃迭代和退出码。摘要同时打印到 Actions 日志。Artifacts 保留 1 天，尽力上传；额度不足仅警告。每档采集 MySQL 状态与 Performance Schema SQL 摘要差值、vmstat 和 iostat。初始门槛为 P95 < 500ms、P99 < 1500ms、HTTP 和业务失败率 < 1%、非持续档位无丢弃迭代；不是线上 SLA。

局限：标准私有仓库 Runner CPU 有限、压测与被测程序同机竞争资源；本地 HTTP 不含公网/TLS；单一游戏、渠道与 mock 不代表真实分布；当前只覆盖上述场景，不覆盖所有 SDK 转发路由、配置写入、故障恢复和滚动更新。实际线上容量需独立压测机和隔离环境验证。对比版本时固定 topology/profile/scenario 并重复运行。

高档位是探索目标，不是已验证容量；丢弃迭代可能来自负载发生器资源不足或被测服务慢。PR 的订单 stress 复用 smoke 后的数据库，与独立运行不可直接比较。

## 扩展压测
压测相关 PR 启动三个独立 Runner：compact 和 cluster 各执行全部 smoke 与探索性 stress（stress 失败单独记录），另一个 compact 执行 30 分钟 soak（默认 mixed，50 次迭代/s）。手动支持 both 对比拓扑以及 soak；每个测试使用独立临时数据库。SDK 配置 stress 增加 2000/3000/5000 目标，其他场景最高 1000；每档失败即停止该场景加压。支付/混合每档等待发货清空最多 120 秒，超时保留结果并停止该场景的更高档位。每阶段按进程采样 CPU（单逻辑核百分比，可超过 100%）与 RSS，输出各服务合计的首段/峰值/末段内存和平均 CPU；不能仅凭 RSS 变化判定 Go 内存泄漏。原始 SQL 语句类型计数与预处理执行计数也会收集，不能保证还原每条 prepared SQL。高档位是探索目标，并非承载能力；PR 自动使用三个 Runner，消耗 Actions 分钟。
## 稳定性测试阈值
soak 30 分钟和手动 soak2h 2 小时均为 50 次业务迭代/s，默认 mixed；预分配 50 VU，最高 100 VU。按计划迭代数允许最多 0.1% 未启动迭代（30 分钟 90 次、2 小时 360 次），其余 HTTP/业务失败率和延迟阈值不变；smoke、baseline、stress 仍要求零丢弃。报告额外显示实际丢弃比例和失败 HTTP 状态分类，不将丢弃当作成功请求。2 小时档位只手动运行，预期消耗不少于 120 Runner 分钟/拓扑；不是自动 PR 的一部分。

## 容量探索 capacity（向 10 万 QPS 目标推进）
PR 增加独立 compact Runner；手动选择 capacity 也可运行。固定 SDK 热点接口对比 Nginx 与单实例直连，1000/3000/5000/7500 次/s 各 20 秒，10000 次/s 冲击 15 秒并回落至 1000 次/s。高档位失败记录但不阻塞后续；1000 档、恢复及独立 profiling 阶段必须通过。capacity 固定执行这套测试，忽略 scenario 参数；cluster 的直连只请求首个 SDK Core，不能与完整集群视为等价。仅 capacity 使用 `-tags loadtest`，独立回环监听 pprof 和 DB.Stats；正常构建为 no-op，不增加业务路由。CPU、Heap、Mutex、Block 在单独的 profiling 流量阶段采集，文本 Top 及连接池和 SQL 事件差值写入日志/摘要，原始文件尽力上传。Mutex/Block 采样为 capacity 进程全程启用，有额外开销；样本量不足可能出现空 profile。Runner 内 k6 和被测服务竞争 CPU，本轮目标不是实际 10000 QPS 保证，更不能作为 100000 并发的证明。

## 第六轮对照实验
单 Runner 先比较快速成功 SDK 配置访问日志全量/10:1 采样（各 Nginx、直连 3000 迭代/s），再运行原 1000–10000 阶梯。Nginx SDK Core upstream 空闲 keepalive 64，其他 16，访问日志 64 KiB 缓冲、1 秒刷新；连接上限和业务重试规则不变。订单登记 500 迭代/s 对比 20/10 与 32/16 数据库最大/空闲连接池，独立测试用配置并重启，不修改线上池默认值。对照档位为探索，原必测基线和恢复阈值不放宽。采样及连接池差异以同 Runner 测量为准，仍可能有时间顺序与噪声影响。

## 第七轮：真实高频流量与抗干扰（traffic）
新增 event-report 单接口（`POST /api/service/events/test`），使用真实游戏 HMAC、平台 Header 签名及本地 Mock `/report/event`。原 mixed 保持历史 6 场景配比，方便跨轮对比；新增 traffic 使用三个独立 constant-arrival-rate 场景，配置查询:用户核验:事件上报=1:1:2，从 100/100/200 到 5000/5000/10000（单 Runner 探索，失败停止升档）。独立配置 P95/P99 20/50ms、核验 100/250ms、上报 500/1500ms；错误率 <1%、丢弃预算 0.1%、完成数至少计划 99.9%。Mock 注入仅事件 1/10/50/100ms 和 HTTP 503，验证核心接口独立阈值及恢复，并校验实际平台签名到达计数，避免跳过上游造成虚假成功。低档基线、事件冒烟、1/10ms、故障及恢复为必测；高档和 50/100ms 为探索。生产代码、缓存、签名与部署不变。100ms 延迟和更高吞吐可能受同机器 k6/VU 容量限制。
