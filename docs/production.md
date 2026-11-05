# 生产部署与运维（Supervisor 优先）

部署模型：**Linux 二进制 + 独立 MySQL 8.0+ + Nginx + Supervisor**。`systemd` 可选，但本文以当前采用的 Supervisor 为主要方案。服务管理配置不放入仓库，也不包含在发布包中；生产机无需安装 Go、Node.js。

> 当前配置中心管理 API 未内建认证。管理、同步、接入回调需通过可信内网或具备认证、白名单和 TLS 的专用网关暴露。对外开放的业务 API 包括必要的 `/api/core/**` 与 `/api/service/**`，其他管理与同步路径仍需保护。

## 1. 部署目录与端口

```text
/data/middle-platform/
├── bin/             # 四个 Linux 可执行程序
├── config/          # 生产 YAML（禁止提交 Git 或打入归档）
├── admin/           # Vue 静态文件
└── logs/            # 由 Supervisor 或系统负责轮转
```

建议：`config-center:8201`、`sdk-core-1:8211`、`sdk-core-2:8212`、`sdk-service:8221`、`config-center-access:8291`（可选），管理端由内部 Nginx 提供。具体端口按现有主机资源调整；不同实例不得占用同一监听地址。

- `config-center` 多实例共享 `config_center` 权威库。
- `sdk-core` 使用单一 `database.dsn` 和单连接池，配置表只读、`config_center.game_order` 限定读写；订单接口及补偿 Worker 随服务启动。多实例可共用一份 YAML，并通过 `SDK_CORE_ADDR` 覆盖监听端口。平台 v1 通过签名、订单与金额一致性校验后确认支付；不查单，但签名未覆盖金额的残余风险必须明确接受。
- `config-center-access` 只有需要本地三级节点副本时才部署，使用独立 `config_access` 库。

## 2. 打包上传与首次安装

在开发机执行：

```powershell
.\scripts\package.ps1 -Arch amd64 -Version v1.0.0
```

将 `dist/middle-platform-v1.0.0-linux-amd64.tar.gz` 上传到服务器 `/tmp/`。**仅首次部署且确认程序尚未运行时**，可用以下命令安装：

```bash
sudo mkdir -p /data/middle-platform/{bin,config,admin,logs}
sudo mkdir -p /tmp/middle-platform-stage
sudo tar --no-same-owner -xzpf /tmp/middle-platform-v1.0.0-linux-amd64.tar.gz -C /tmp/middle-platform-stage
file /tmp/middle-platform-stage/bin/*
ls -l /tmp/middle-platform-stage/bin/
sudo cp -a /tmp/middle-platform-stage/bin/. /data/middle-platform/bin/
sudo cp -a /tmp/middle-platform-stage/admin/. /data/middle-platform/admin/
```

使用 `-SkipFrontend` 构建的包没有 `admin/`，不要执行对应 `cp`。创建低权限服务账号 `middle-platform`，按实际用户/组授予 `bin/`、`config/` 和 `logs/` 最小权限；真实 `config-center.yaml`、`config-center-access.yaml`、`sdk-core.yaml`、`sdk-service.yaml` 单独放在 `/data/middle-platform/config/`。

### 打包参数及产物

`scripts/package.ps1` 是唯一手动打包入口，支持 Windows PowerShell 与 GitHub Actions 统一生成 Linux ELF + tar.gz。依赖 Go 1.27.3；包含前端时还需要 Node.js 22 / npm。

```powershell
.\scripts\package.ps1 -Arch amd64 -Version v1.0.0
.\scripts\package.ps1 -Arch arm64 -Version v1.0.0
.\scripts\package.ps1 -SkipFrontend -Version v1.0.0
```

省略 `-Version` 时使用提交号或本地时间作为版本标识；Go 交叉编译加 `-buildvcs=false`。正常情况下 `dist/` 只保留一个版本归档，没有中间目录。

归档结构以打包脚本为准：

```text
bin/
  config-center
  config-center-access
  sdk-core
  sdk-service
sql/
  config-center/
    initialize.sql
    add_game_order.sql
    migrate_sdk_node_config.sql
    cleanup_legacy_sdk_fields.sql
    add_sdk_services.sql
    add_sdk_user_check.sql
    add_sdk_role_query.sql
    add_sdk_server_query.sql
    add_sdk_role_control.sql
    add_sdk_subscribe_message.sql
  config-center-access/
    initialize.sql
admin/                     # -SkipFrontend 时不存在
```

归档不包含生产密钥、YAML、Nginx/Supervisor 配置。Linux 可执行文件保存 0755 权限，可在上传后执行 `tar -tzf`、`file bin/*` 校验。GitHub Actions `.github/workflows/package.yml` 使用同一脚本并检查 ELF 架构与权限。打包不会自动部署、执行 SQL 或管理 Supervisor。

## 3. 手动 SQL 先于启动

备份并核对目标数据库，**只执行对应场景的 SQL**：

- SDK 拆分升级：停所有旧实例并备份后执行 `sql/config-center/migrate_sdk_node_config.sql`，验证新版后单独执行 `cleanup_legacy_sdk_fields.sql`。详见 [数据库文档](database.md)。
- 全新、空的 `config_center`：`sql/config-center/initialize.sql`。
- 已有 `config_center` 仅缺少订单表：`sql/config-center/add_game_order.sql`。
- 已有权威库但没有 SDK 服务表：`sql/config-center/add_sdk_services.sql`（仅表确实不存在时执行）。
- 已有功能表但缺少 `user_check` 服务目录：`sql/config-center/add_sdk_user_check.sql`。
- 已有服务表但缺少角色查询、区服查询或封解禁目录：分别手工执行 `sql/config-center/add_sdk_role_query.sql`、`add_sdk_server_query.sql`、`add_sdk_role_control.sql`，在部署新版 sdk-service 前完成。
- 已有服务表但缺少订阅消息目录：手动执行 `sql/config-center/add_sdk_subscribe_message.sql`，在部署新版 sdk-service 前完成。
- 全新、空的 `config_access`：`sql/config-center-access/initialize.sql`。

例如在 Linux 发布包解压目录（已有库补建订单表）：

```bash
mysql -u <管理账号> -p config_center < sql/config-center/add_game_order.sql
```

如果目标表已经存在，不要反复执行初始化 SQL；应检查实际表定义与索引。
应用启动只校验结构，不创建或修改数据库表。历史 `config_schema_migrations` 仅作遗留记录，新代码不再检查或更新。
参见 [数据库说明](database.md)。

## 4. Supervisor 管理服务

Supervisor 配置由服务器独立管理，例如 `/etc/supervisor/conf.d/middle-platform.conf`。以下是配置中心的**参考样例**，使用前要确认账号存在、权限正确、YAML 已填写、服务已在 `8201` 监听：

```ini
[program:config-center]
command=/data/middle-platform/bin/config-center -config /data/middle-platform/config/config-center.yaml
directory=/data/middle-platform
user=middle-platform
autostart=true
autorestart=unexpected
stopsignal=TERM
stopwaitsecs=40
redirect_stderr=true
stdout_logfile=/data/middle-platform/logs/config-center.log
stdout_logfile_maxbytes=50MB
stdout_logfile_backups=5
environment=CONFIG_GO_ADDR=":8201"
```

对于 `sdk-core-1` / `sdk-core-2`，可复用 `bin/sdk-core` 与 `config/sdk-core.yaml`，但分别配置 `SDK_CORE_ADDR=":8211"` / `":8212"`、独立 `[program:...]` 名称和日志文件。`sdk-service` 使用独立的 `bin/sdk-service`、`config/sdk-service.yaml`，默认监听 `:8221`；接入端同理使用独立进程、DSN 与端口。配置变更后：

```bash
sudo supervisorctl reread
sudo supervisorctl update
sudo supervisorctl status
sudo supervisorctl restart config-center
sudo supervisorctl tail -f config-center
```

生产机原有 Supervisor 配置应以实际路径、命名和用户为准，**不要因为示例而覆盖已有服务定义**。如改用 `systemd`，请配置 `Restart=on-failure`、`KillSignal=SIGTERM`、`TimeoutStopSec=40s`，日志由 `journald` 收集；两种管理器不要同时控制同一个进程。

## 5. Nginx 网关与访问边界

以下示例按后端内部路由 `/api/...` 转发。若公网使用 `/sdk/api/...`，需在 Nginx 明确移除 `/sdk` 前缀后再交给后端，避免出现双重前缀。

- 对外业务域名：`/api/core/` 转发到 SDK Core 多实例；`/api/service/` 转发到 SDK Service（平台核验/查询/封解禁和游戏事件/订阅消息）。
- 管理域名或受保护内网：`/api/config/` 转发到配置中心，后台静态页来自 `admin/`；对管理和同步路径设置白名单/认证。
- `/internal/config/callback` 只允许配置中心从可信网络访问接入端；验证 HMAC 和时间窗口并不能替代网络边界。

```nginx
upstream sdk_core {
    server 127.0.0.1:8211;
    server 127.0.0.1:8212;
}
# 以下 location 仅放在对外 SDK API 对应的 server 中
# 必须放在通用 /api/core/ 路由之前：SDK 管理接口不能通过公开 SDK 网关访问。
location ^~ /api/core/admin/ {
    return 403;
}
location ^~ /api/core/ {
    proxy_pass http://sdk_core;
}
location ^~ /api/service/ {
    proxy_pass http://127.0.0.1:8221;
}
```

Nginx 不会因 `/health/ready` 自动从静态 upstream 中摘除后端；需要明确的流量摘除流程或具备主动健康检查的网关。

### 配置管理前端：运行时配置

前端构建采用 Vite `base: './'`，可以直接放在 Nginx 的 `/admin/` 或其他子路径下。静态资源使用相对路径，不需要修改打包脚本或重新构建。

前端所有配置中心 API 请求从 `admin/config.js` 中读取：

```js
window.__MIDDLE_PLATFORM_CONFIG__ = {
  apiBaseUrl: '/api/config',
  sdkApiBaseUrl: '/api/core'
}
```

注意：此文件是浏览器能读取的公开配置，不要加入密码、访问令牌或 Webhook 密钥。`apiBaseUrl` 指向配置中心，`sdkApiBaseUrl` 指向 SDK Core。管理页面只可通过内网/受认证的独立网关访问，不得通过公开 SDK 网关访问 `/api/core/admin/`。

为了确保更新 `admin/` 静态资源时不覆盖生产配置，建议在正式服务器将配置长期保存于 `/data/middle-platform/config/admin.js`：

```bash
cp -n /data/middle-platform/admin/config.js /data/middle-platform/config/admin.js
vi /data/middle-platform/config/admin.js
```

在现有的 `zk.skybluek.com` HTTPS `server {}` 内，优先配置精确匹配：

```nginx
location = /admin/config.js {
    alias /data/middle-platform/config/admin.js;
    add_header Cache-Control "no-store" always;
}
location = /admin {
    return 301 /admin/;
}
location ^~ /admin/ {
    alias /data/middle-platform/admin/;
    index index.html;
    try_files $uri $uri/ /admin/index.html;
}
location ^~ /api/config/ {
    proxy_pass http://127.0.0.1:8201;
}
```

该规则只演示配置中心相关路径。SDK Core 的 `/api/core/` 仍应独立配置负载均衡代理，不能转发到配置中心。此处 `proxy_pass` 不附加 URI，直接透传原始 `/api/config/` 请求路径。

```bash
nginx -t && systemctl reload nginx
curl -i https://zk.skybluek.com/admin/config.js
```

改动 `admin.js` 后强制刷新浏览器即可生效，不需要重启 Supervisor。原项目使用 hash 路由，子路径刷新不会要求额外的前端路由配置。

**安全提醒：** 现在配置中心管理 API 尚无完整认证，`/admin/` 和 `/api/config/admin/` 必须同时通过 IP 白名单或网关认证保护。只隐藏前端页面不能保护管理接口。

## 6. 直接发布与失败排查

本项目使用固定的 `bin/`、`admin/`、`sql/` 目录，不建立 `releases/` 版本目录，也不提供自动或手动回滚脚本。发布工具见 [本地发布说明](../publish/README.md)。

```powershell
# 在开发机上执行：默认打包、上传、更新文件、重启 Supervisor 并验活
.\publish\publish.ps1 -Version v1.0.0

# 仅部署 SDK Core（按 publish.config.json 的 services 配置依次重启）
.\publish\publish.ps1 -Version v1.0.1 -Service sdk-core

# 仅更新文件，不重启
.\publish\publish.ps1 -Version v1.0.2 -SkipRestart
```

1. 发布前确认服务与数据库结构兼容。DDL 仍在单独变更窗口由管理员审核并执行，不随脚本自动迁移。
2. 上传后校验 SHA-256、架构、归档内容、所选服务；校验成功再把文件逐个写入同目录临时文件并重命名替换，保留 `config/`、`logs/`、`admin/config.js`。
3. 默认读取本地 `publish/publish.config.json` 的 `services`（Supervisor 进程名 → 健康检查端口），只重启选中模块对应的实例并逐个验证本机 `/health/ready`。实例支持 `config-center-1/2`、`sdk-core-1/2/3` 等名称；新增实例只需改 JSON。`-SkipRestart` 可跳过这一阶段。
4. **单次发布不是跨多个文件的事务**；更新、重启或验活失败不会自动回滚，可能需要人工修复。Nginx 静态 upstream 不自动根据健康端点摘流量，两个 Core 顺序重启不保证零中断。
5. 发布后还需人工抽查充值订单、配置同步和事件上报等关键业务，确认未发生接口或配置不兼容。

## 7. 日志、停服与排障

四个 Go 进程共用 `shared/logging`（`log/slog`），YAML 示例：

```yaml
log:
  level: info
  format: json
  output: stdout
  accessEnabled: true
  slowThreshold: 500ms
```

支持 `LOG_LEVEL`、`LOG_FORMAT`、`LOG_OUTPUT`、`LOG_ACCESS_ENABLED`、`LOG_SLOW_THRESHOLD` 环境变量；`INSTANCE_ID` 可标记实例。HTTP 响应携带 `X-Request-ID`，访问日志含 `service`、`instance`、`request_id`、`status`、`duration_ms` 等字段，不记录密码、签名、请求体。Supervisor 收集 stdout 并轮转；若选择 systemd 则使用 `journalctl` / journald。不要把高基数的 `request_id` 用作日志系统标签。

服务接到 `SIGINT` 或 `SIGTERM` 时先将 `/health/ready` 置为不可用，`http.Server.Shutdown` 等待存量 HTTP 请求（最多 30 秒），再等待 Webhook 或同步 Worker 并关闭数据库连接；超时可能被强制关闭，依靠游标/租约重试恢复。Supervisor 应设置 `stopsignal=TERM` 和至少 40 秒停机宽限时间。不要使用强制 Kill 作为正常停机方式。

**提醒：** Go 的 Shutdown 不会自动排空 WebSocket/hijacked 连接；未来 SDK 新增此类连接需单独处理退出。

## 上线发布前检查

- 配置中心管理写接口**始终注册**：升级部署前必须由防火墙、私有网络或具备身份验证的网关限制整个 `/api/config/` 前缀，特别是 `/api/config/admin/`、节点原始 JSON 和同步快照，避免任意用户写入或读取服务端密钥。不要仅隐藏管理网页。
- SDK Core 订单接口和补偿 Worker 始终加载，数据库必须已有 `game_order` 表，账号需配置表 SELECT 与订单表 SELECT/INSERT/UPDATE；SDK Service 还需服务开关表与游戏、大区、渠道包的只读权限。
- 平台 v1 缺少实付金额字段的签名完整性认证；SDK Core 不做二次查单，按签名和登记金额一致性执行支付确认。上线前须核实平台服务端可信定价、支付成功回调保障和游戏幂等发货，并接受现有回调协议的残余风险。
- 部署前按各模块当前 YAML 模板更新私有配置，删除废弃开关及环境变量；代码合并不代表已经更新 Nginx ACL、线上配置或数据库。

## 8. 事件上报迁移发布顺序（重要）

1. 先在存量权威库执行 `add_sdk_subscribe_message.sql`，确认 `event_report`、`subscribe_message` 已注册且渠道环境功能开关配置正确。
2. 把原 `/data/middle-platform/config/sdk-core.yaml` 中的 `platform.eventReportURL` 移到 `sdk-service.yaml` 的同名配置；为订阅消息填写 `platform.subscribeMessageURL`。Core YAML 中必须删除旧 `platform`，否则严格 YAML 解码将拒绝启动。
3. 先验证 Nginx `/api/service/` 转发正确，再部署 SDK Service 和 SDK Core 新程序。旧 Core 的 `POST /api/core/events/{env}` 删除后返回 404。
4. 同步更新游戏端或调用 SDK 的事件上报 URL 到 `POST /api/service/events/{env}`，新增订阅消息使用 `POST /api/service/subscribe/{env}`。
5. 迁移切换不是无缝兼容：游戏方未更新旧地址时上报会失败。生产发布窗口应协调上游切换，必要时由受控网关短期转发旧路径到新 Service，完成切换后移除，避免双写。

发布不执行 SQL、不自动修改服务器 YAML，也不会自动变更 Nginx；必须按以上顺序人工部署。


## SDK 管理接口的 Nginx 隔离

SDK Core 进程中同时存在公开业务路由和 `/api/core/admin/` 管理路由。**管理路由没有内建用户认证**。生产环境必须使用两个受网络隔离的网关入口：

- 公共 SDK server：如上所示，用 `location ^~ /api/core/admin/ { return 403; }` 拒绝管理前缀，再代理 `/api/core/` 业务。
- 独立管理 server：仅可信内网、VPN 或带认证的网关可以访问，允许 `/api/core/admin/` 反向代理到 `sdk_core`；后台静态页面及 `/api/config/admin/` 也由该入口保护。

确保配置文件中带 `/sdk` 外部前缀时，先在对应外层 location 显式拒绝 `/sdk/api/core/admin/`，不能只拦截内部路径。请验证未认证的外部请求无法访问管理接口后才开始切流；单纯隐藏管理页面不足以保障安全。
