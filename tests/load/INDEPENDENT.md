# 独立压测机（Windows）执行手册

与 GitHub Runner 方案完全独立。只允许自有、授权、隔离的测试入口。脚本固定 test 环境，不发订单、支付、管理操作。虽然不会写入中台订单，事件仍会向平台发送：执行前必须确认测试渠道的核验和上报 URL 已指向隔离 Mock 或平台授权测试接口、测试用户均可核验、不会污染真实平台数据。仅填写确认变量不能证明目标已经隔离，请人工核实。不要修改生产渠道开关或密钥来获得压测通过。

## 准备

- Windows 安装 k6（建议与 CI 一致 1.0.0）、PowerShell，确认 k6 version。
- 单独部署与生产拓扑一致的隔离服务（2 config-center、4 sdk-core、4 sdk-service），独立测试数据库、专用 TLS 域名及测试平台，设置访问白名单。生产服务器上部署隔离环境需评估 CPU、内存与 DB 竞争；优先使用独立测试服务器。
- 在仓库之外保存 JSON 数组，如 C:\load-private\fixtures.json；只使用测试 GameSecret，禁止提交 Git 或 CI Artifact。每项必须有 appId、gameSecret、zoneKey（区服组 code），同一游戏可共享游戏密钥，渠道 AppSecret 由服务端读取。示例（均为占位值）：

    [
      {"appId":"test-channel-1","gameSecret":"REPLACE_WITH_TEST_GAME_SECRET","zoneKey":"test-group-1"}
    ]

## 逐档 Windows PowerShell 入口

已准备隔离 HTTPS 服务、Mock 和 256 个测试渠道之后，在 Windows 电脑仓库根目录执行：

    .\tests\load\run-independent.ps1 -BaseUrl 'https://YOUR-ISOLATED-TEST-HOST' -FixtureFile 'C:\load-private\fixtures.json' -Confirm 'AUTHORIZED-ISOLATED-TEST'

脚本依次执行总 400 / 1000 / 2000 / 3000 / 5000 QPS，每档 20 秒，每档都运行独立的三路 1:1:2 k6 场景。k6 阈值未通过、压测机无法发送足量请求、业务错误或摘要丢失时立即停止后续升压；报告写到 Windows 临时目录下 middle-platform-load 文件夹，不输出测试密钥。报告及 JSON 密钥必须保存在仓库外。只有显式添加 -Allow10000 才会追加 10000 QPS；建议完成独立压测机资源评估后再开启，不应直接压生产。此入口需要刚好 256 个有效测试渠道。

## 执行

在仓库根目录的 PowerShell 终端中手动输入：

    $env:LOAD_CONFIRM = 'AUTHORIZED-ISOLATED-TEST'
    $env:LOAD_BASE_URL = 'https://YOUR-ISOLATED-TEST-HOST'
    $env:LOAD_FIXTURES = 'C:\load-private\fixtures.json'
    $env:LOAD_TOTAL_QPS = '400'
    $env:LOAD_SECONDS = '20'
    k6 run --summary-export .\load-400.json .\tests\load\independent.js

确认 400 档达标且资源充足后，分别手工修改 LOAD_TOTAL_QPS 为 1000、2000、3000、5000；每档单独执行，修改 summary 文件名，不能自动升压。脚本最大 10000 总 QPS、单档 10～60 秒。三路独立 constant-arrival-rate，25% 配置、25% 核验、50% 上报。不自动重试；错误率达到阈值会停止测试，Ctrl+C 可随时停止。正常平台核验必须实际返回 true，不能靠关闭核验开关。TLS 证书必须可信。

## 观测与验收

- k6 机：任务管理器/资源监视器查看 CPU（建议低于 80%）、内存、网络；出现饱和、丢弃迭代过多、超时即停止升档。
- 服务端：使用 vmstat 1、iostat -dx 1、pidstat -u -r 1、ss -s（已安装相关工具时）；观察 Nginx 5xx/499、业务错误、Supervisor 重启次数；MySQL SHOW GLOBAL STATUS 记录 Com_stmt_execute、Threads_running、Innodb_row_lock_waits、Innodb_log_waits 前后差值。
- 配置 P95/P99 20/50ms；核验 100/250ms；上报 500/1500ms；各路 HTTP/业务错误率 <1%，丢弃不超过计划 0.1%，完成数不少于计划 99.9%。这些是测试门槛，不是正式 SLA。公网/TLS 延迟会影响结果。
- 先保持线上安全默认 TTL=0s 和全量日志；仅在隔离环境试 TTL=1s、事件日志采样10。相同流量、渠道分布、拓扑下比较多次，验证密钥撤销/启停的时效要求后再决定生产配置。报告记录实际平台接收次数，避免误把跳过核验算作成功。
- 独立脚本只能校验客户端结果，不读取远端 Mock 计数，不开启远程 pprof。GC/DB 细项先在 Runner 的 traffic-multi 档验证。没有现成生产指标端点；生产监控/告警需单独设计与部署。

测试完成清理本地测试数据与报告，移除环境变量。正式服务器未经维护窗口、隔离容量和真实平台配额确认，不直接发高压测试。
