# Runner 本地压测

全部业务程序、MySQL、Nginx、Supervisor、模拟发行平台/游戏和 k6 均运行在同一 GitHub 托管 Ubuntu Runner，不使用生产地址、密钥、数据，也不参与正式发布。脚本拒绝在非 GitHub 托管 Runner 执行。业务 API 和数据库结构不修改。

## 运行
在 Actions → Runner Load Test → Run workflow 选择参数。压测相关 PR 自动运行两个拓扑的 smoke/stress 和独立 soak（见下文）；其他业务提交不自动触发此工作流。

- profile: smoke (5 次迭代/s × 10 秒)、baseline (25/50/100 × 30 秒)、stress (100/250/500/1000 × 30 秒)。以及 soak (50 次迭代/s × 30 分钟，all 默认 mixed)。每个场景独立阶梯，失败即停止该场景继续加压。
- topology: compact (2/1/1) 或 cluster (2/4/4)，both 对比二者，每个 Go 实例 GOMEMLIMIT=128MiB（软目标）。
- scenario: all（逐项运行）或 config-sync、sdk-config、user-check、order、duplicate、payment、mixed。所有场景固定访问 127.0.0.1，不能指定外部地址。

配置同步读 revision；SDK 配置读热点渠道；用户核验真实经过 DB、验签和本地模拟发行平台；订单写入临时 DB；重复登记走真实幂等接口；payment 先登记再发送平台 V1 回调，最后检验持久化发货状态；mixed 轮流执行以上业务。支付与重复订单每次迭代最多两次 HTTP 请求，因此迭代速率不是 HTTP QPS。

## 结果
Actions Summary 显示每档实际 HTTP QPS、P50/P95/P99、业务失败率、丢弃迭代和退出码。摘要同时打印到 Actions 日志。Artifacts 保留 1 天，尽力上传；额度不足仅警告。每档采集 MySQL 状态与 Performance Schema SQL 摘要差值、vmstat 和 iostat。初始门槛为 P95 < 500ms、P99 < 1500ms、HTTP 和业务失败率 < 1%、无丢弃迭代；不是线上 SLA。

局限：标准私有仓库 Runner CPU 有限、压测与被测程序同机竞争资源；本地 HTTP 不含公网/TLS；单一游戏、渠道与 mock 不代表真实分布；当前只覆盖上述场景，不覆盖所有 SDK 转发路由、配置写入、故障恢复和滚动更新。实际线上容量需独立压测机和隔离环境验证。对比版本时固定 topology/profile/scenario 并重复运行。

高档位是探索目标，不是已验证容量；丢弃迭代可能来自负载发生器资源不足或被测服务慢。PR 的订单 stress 复用 smoke 后的数据库，与独立运行不可直接比较。

## 扩展压测
压测相关 PR 启动三个独立 Runner：compact 和 cluster 各执行全部 smoke 与探索性 stress（stress 失败单独记录），另一个 compact 执行 30 分钟 soak（默认 mixed，50 次迭代/s）。手动支持 both 对比拓扑以及 soak；每个测试使用独立临时数据库。SDK 配置 stress 增加 2000/3000/5000 目标，其他场景最高 1000；每档失败即停止该场景加压。支付/混合每档等待发货清空最多 120 秒，超时保留结果并停止该场景的更高档位。每阶段按进程采样 CPU（单逻辑核百分比，可超过 100%）与 RSS，输出各服务合计的首段/峰值/末段内存和平均 CPU；不能仅凭 RSS 变化判定 Go 内存泄漏。原始 SQL 语句类型计数与预处理执行计数也会收集，不能保证还原每条 prepared SQL。高档位是探索目标，并非承载能力；PR 自动使用三个 Runner，消耗 Actions 分钟。