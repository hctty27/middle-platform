# 研发中台正式环境压力测试

本目录提供 Windows + k6 压测脚本。目标是正式 Nginx 入口，但业务请求默认只使用 test 环境渠道包；所有写入或可能调用外部平台的场景均需要显式二次确认。

## 1. 当前项目路由与测试边界

Nginx 将以下路径转发到对应服务：

| 服务 | 路由前缀 | 默认场景 |
| --- | --- | --- |
| config-center | /api/config/ | 签名读取同步版本 |
| sdk-core | /api/core/ | 读取 test 渠道包配置 |
| sdk-service | /api/service/ | 需要独立 test 渠道及明确确认 |

所有实例的 /health/live 和 /health/ready 是进程本地路由；当前 Nginx locations 不把这些路径按服务区分。因此本套入口压测使用实际业务 API，不能把 /health/ready 假定为公网 Nginx 路由。

脚本不依赖实例数量。执行前应核对服务器当前 Nginx upstream runtime 文件、Supervisor 实例和部署版本。不要从历史文档推断当前实例数量。

## 2. 安全前置条件

正式环境压测必须安排低峰维护窗口，并由业务负责人确认。执行前完成：

1. 使用 HTTPS 正式 Nginx 域名；Windows 压测机与业务服务器分离。
2. 使用 test 环境 AppID、游戏密钥和独立订阅者身份；不要把 prod AppID 或生产密钥放进本地配置。
3. 确认 test 渠道包已绑定有效配置，且所有测试请求不会触发真实充值、发货、奖励或用户资产变更。
4. 确认 Nginx access log 能通过压测源 IP 和专用 User-Agent 识别流量；如果当前日志格式记录了 x-app-id，再同时核对测试 AppID。
5. 先确认 MySQL 备份、连接数余量、磁盘空间、日志轮转和告警可用。
6. 先执行 smoke；确认正式业务流量无异常后，才逐档提高 RPS。
7. 任何正式业务异常、持续 5xx、P95 明显恶化、CPU/内存/数据库连接接近告警阈值时，立即停止压测。

不要在正式环境运行 DROP/CREATE 类集成测试，也不要对管理写接口、配置发布、支付回调、真实订单或发货接口施压。

### 写操作与外部调用保护

- sdk-core-orders 会向生产数据库新增订单，并由异步 Worker 处理后续通知。只有确认 test 渠道的 payNotifyUrl 指向隔离的测试接收端、不会触发真实发货后，才允许使用 -AllowOrderWrites -ConfirmOrderNotifySink。脚本限制最高 5 RPS，但仍会持续产生订单记录，测试结束后按业务审核流程清理，不直接执行未经审核的 SQL。
- sdk-service-user-check 会经过 SDK Service 的游戏 HMAC、配置读取和用户核验流程。只有确认 test 渠道全局及渠道环境开关使其跳过外部平台核验，并且不会访问真实平台时，才允许使用 -AllowServiceUserCheck -ConfirmSkipUserCheck。否则不要执行该场景。
- mixed/ramp/soak 默认使用合成比例：40% config-center revision 读取、60% sdk-core test 配置读取。只有显式使用 -IncludeServiceInMix 并完成上面的确认，才改为 40% config-center、50% sdk-core、10% sdk-service 用户核验。该比例是压测初始模型，不代表真实线上流量比例，正式容量评估前应按访问日志调整。
- 不测试支付回调、订单发货通知、角色封禁/解禁、配置管理写入或事件/订阅推送；这些操作可能影响真实玩家或第三方系统。

## 3. Windows 安装与本地配置

安装 k6：

~~~powershell
winget install k6 --source winget
k6 version
~~~

复制本地配置模板：

~~~powershell
Copy-Item .\tests\load\config.example.ps1 .\tests\load\config.local.ps1
notepad .\tests\load\config.local.ps1
~~~

只填写 test 环境凭据。config.local.ps1 和 reports 目录已加入 Git 忽略规则。

配置项：

| 环境变量 | 用途 |
| --- | --- |
| CONFIG_SUBSCRIBER_CODE | config-center 的 test 订阅者编码 |
| CONFIG_SUBSCRIBER_SECRET | 对应订阅者 signing_secret，至少 32 字节 |
| CORE_ENV | 固定 test |
| CORE_APP_ID | 已绑定 test 环境的渠道包 AppID |
| CORE_GAME_SECRET | sdk-core 订单测试使用的游戏密钥 |
| SDK_SERVICE_ENV | 固定 test |
| SDK_SERVICE_APP_ID | sdk-service 测试 AppID |
| SDK_SERVICE_GAME_SECRET | sdk-service 游戏 HMAC 密钥 |
| SDK_SERVICE_SKIP_USER_CHECK_CONFIRMED | 仅在核实 test 渠道跳过外部平台调用后设为 true |
| OPEN_ID_PREFIX | 用于生成唯一测试 open_id 的前缀 |
| ORDER_ID_PREFIX | 订单测试唯一 ID 前缀 |
| ORDER_AMOUNT | 订单测试金额，默认 1 |

不要把真实密钥写入命令行、测试报告或仓库。Windows 系统时间必须保持同步；游戏和订阅者签名均使用秒级时间戳，服务端验签时间窗口为 5 分钟。

## 4. 执行命令

以下命令均要求显式确认目标是正式入口。先从 1 次请求的 smoke 开始：

~~~powershell
.\tests\load\run.ps1 -BaseUrl "https://你的正式域名" -Scenario smoke -ConfirmProduction
~~~

单服务只读基准：

~~~powershell
.\tests\load\run.ps1 -BaseUrl "https://你的正式域名" -Scenario config-center -MaxRps 5 -ConfirmProduction
.\tests\load\run.ps1 -BaseUrl "https://你的正式域名" -Scenario sdk-core -MaxRps 5 -ConfirmProduction
~~~

混合只读压力，逐次提高上限，不要一次跳到高档：

~~~powershell
.\tests\load\run.ps1 -BaseUrl "https://你的正式域名" -Scenario ramp -MaxRps 5 -ConfirmProduction
.\tests\load\run.ps1 -BaseUrl "https://你的正式域名" -Scenario ramp -MaxRps 10 -ConfirmProduction
.\tests\load\run.ps1 -BaseUrl "https://你的正式域名" -Scenario ramp -MaxRps 20 -ConfirmProduction
~~~

每档确认无异常后，再考虑 50、100、150、200 RPS。runner 限制最高 200 RPS；达到上限不代表服务器容量上限，也不代表必须测到该值。

ramp 场景的升压阶段按 5、10、20 RPS 逐级展开，-Duration 控制最后一档保持时间；例如 5 RPS 时总时长约为 2 分钟升压 + 5 分钟保持 + 1 分钟降压。sdk-core-orders 与 sdk-service-user-check 使用恒定到达速率，-Duration 控制整个测试时长。长稳测试先以已验证稳定速率的 50%–70% 运行：

~~~powershell
.\tests\load\run.ps1 -BaseUrl "https://你的正式域名" -Scenario soak -MaxRps 5 -Duration 30m -ConfirmProduction
~~~

config-center 同步读取支持 revision、changes 和 snapshot。默认只测 revision。changes 使用 CONFIG_AFTER_REVISION 和 CONFIG_CHANGE_LIMIT；snapshot 可能返回较大数据，只能在低 RPS 单独测试：

~~~powershell
$env:CONFIG_SYNC_OPERATION = "snapshot"
.\tests\load\run.ps1 -BaseUrl "https://你的正式域名" -Scenario config-center -MaxRps 1 -ConfirmProduction
$env:CONFIG_SYNC_OPERATION = "revision"
~~~

### SDK Service 测试

先确认 test 渠道不会向真实平台发起用户核验，再执行：

~~~powershell
.\tests\load\run.ps1 -BaseUrl "https://你的正式域名" -Scenario sdk-service-user-check -MaxRps 1 -AllowServiceUserCheck -ConfirmSkipUserCheck -ConfirmProduction
~~~

### 订单登记写入测试

只有 test 渠道 payNotifyUrl 已明确指向隔离测试接收端，才可执行：

~~~powershell
.\tests\load\run.ps1 -BaseUrl "https://你的正式域名" -Scenario sdk-core-orders -MaxRps 1 -AllowOrderWrites -ConfirmOrderNotifySink -ConfirmProduction
~~~

订单写入默认禁用，最高 5 RPS。不要在未隔离发货通知的情况下执行。

## 5. 测试阶段与停止条件

建议执行顺序：

| 阶段 | 测试 | 初始速率 | 时长 |
| --- | --- | ---: | --- |
| 1 | smoke | 1 次 | 单次 |
| 2 | config-center | 5 RPS | 约 8 分钟（2 分钟升压、5 分钟保持、1 分钟降压） |
| 3 | sdk-core | 5 RPS | 约 8 分钟（2 分钟升压、5 分钟保持、1 分钟降压） |
| 4 | mixed/ramp | 5、10、20 RPS | 每档约 8、11、14 分钟 |
| 5 | mixed/ramp | 50、100、150、200 RPS | 每档单独评估 |
| 6 | soak | 已验证稳定速率的 50%–70% | 先 30 分钟，再 2 小时 |
| 7 | availability-watch | 1 RPS | 人工故障窗口 |

初始停止条件（应以线上现有告警阈值和业务 SLA 中更严格者为准）：

- HTTP 5xx 持续 1 分钟超过 1%。
- P95 超过压测前基线 2 倍并持续 1 分钟。
- CPU 持续 2 分钟超过 85%。
- 任一进程 RSS 接近其 cgroup 内存上限。
- MySQL 连接数达到 max_connections 的 80%。
- 正式业务出现异常、订单/通知积压异常或人工告警。
- Windows 压测机 CPU 或网卡已饱和，无法维持目标请求速率。

发生任一停止条件：停止 k6、记录时间点、保存报告和服务器指标，不要立即继续下一档。

## 6. Nginx 故障摘除与恢复验证

故障注入必须单独安排窗口。k6 只负责持续观察请求，不会自动 SSH、kill 或重启生产实例。

先在 Windows 启动低速观察：

~~~powershell
.\tests\load\run.ps1 -BaseUrl "https://你的正式域名" -Scenario availability-watch -Duration 30m -ConfirmProduction
~~~

再在服务器 SSH 会话中，按照 ops/nginx/README.md 的当前实例清单，选择一个实例执行 drain；观察至少 1 分钟后 enable。不要同时 drain 同一 upstream 的多个实例，不要在业务高峰操作。依次记录故障、摘除、进程恢复、Ready 通过和重新接流量的时间。

Nginx Open Source 当前使用被动失败探测，不是主动轮询 /health/ready。测试应通过 Nginx access log 中的 upstream_addr/upstream_status 与请求成功率核实摘除和恢复；单看 Supervisor RUNNING 不算恢复完成。

## 7. 报告与数据判读

每次运行在 reports 目录生成 JSON 汇总报告，文件名包含场景和 Run ID。报告不包含密钥。

必须同时记录：

- k6：目标 RPS、实际 http_reqs rate、请求总量、失败率、P95/P99、dropped_iterations。
- Nginx：status、request_time、upstream_response_time、upstream_addr、upstream_status。
- Linux：每个 PID 的 CPU/RSS、load average、磁盘 IO、网络和 TCP。
- Go：HeapAlloc、HeapInuse、NumGoroutine、GC 指标、进程重启次数。
- MySQL：Threads_connected、Max_used_connections、慢查询、锁等待和连接池等待。

如果 dropped_iterations 大于 0，说明 k6 未能按目标速率启动全部 iteration；先排除 Windows 压测机、VUs 配置和网络瓶颈，再判断服务端容量。RPS 必须以实际 http_reqs rate 为准，不能只引用目标速率。

P95/P99 应按接口和服务分别比较。混合流量的总体延迟不能代替单接口结果。最终推荐运行容量以长稳测试、业务 SLA 和故障余量共同确定，不按一次峰值 QPS 定容。

## 8. HMAC 兼容性

签名实现直接对应仓库 shared/signature/game_hmac.go 和 shared/signature/admin.go：

- 游戏请求：HITALK-HMAC-SHA256、HTTP method、EscapedPath、AppID、秒级 timestamp、16 位小写 nonce、原始 Body。
- 配置订阅者请求：额外绑定 SUBSCRIBER 和 X-Subscriber-Code，并签署 method、EscapedPath、规范化 Query、timestamp、nonce 和原始 Body。
- 每次请求重新生成 timestamp 和密码学随机 nonce；不得复用签名。

sdk-core 配置查询是公开只读接口，不添加虚构签名。管理 API 不纳入本套压测，以免产生管理数据变更。

参考：
- Grafana k6 安装：https://grafana.com/docs/k6/latest/set-up/install-k6/
- Arrival-rate executor：https://grafana.com/docs/k6/latest/using-k6/scenarios/executors/ramping-arrival-rate/
- k6 报告输出：https://grafana.com/docs/k6/latest/results-output/
