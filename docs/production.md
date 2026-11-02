# 生产部署与运维（Supervisor 优先）

部署模型：**Linux 二进制 + 独立 MySQL 8.0+ + Nginx + Supervisor**。`systemd` 可选，但本文以当前采用的 Supervisor 为主要方案。服务管理配置不放入仓库，也不包含在发布包中；生产机无需安装 Go、Node.js。

> 当前配置中心管理 API 未内建认证。管理、同步、接入回调需通过可信内网或具备认证、白名单和 TLS 的专用网关暴露。`/api/core/**` 才是面向 SDK 客户端的业务 API。

## 1. 部署目录与端口

```text
/data/middle-platform/
├── bin/             # 四个 Linux 可执行程序
├── config/          # 生产 YAML（禁止提交 Git 或打入归档）
├── admin/           # Vue 静态文件
└── logs/            # 由 Supervisor 或系统负责轮转
```

建议：`config-center:8201`、`sdk-core-1:8211`、`sdk-core-2:8212`、`config-center-access:8291`（可选），管理端由内部 Nginx 提供。具体端口按现有主机资源调整；不同实例不得占用同一监听地址。

- `config-center` 多实例共享 `config_center` 权威库。
- `sdk-core` 多实例共享权威库的只读账号，可共用同一二进制，通过环境变量覆盖不同监听端口。
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

使用 `-SkipFrontend` 构建的包没有 `admin/`，不要执行对应 `cp`。创建低权限服务账号 `middle-platform`，按实际用户/组授予 `bin/`、`config/` 和 `logs/` 最小权限；真实 `config-center.yaml`、`config-center-access.yaml`、`sdk-core.yaml` 单独放在 `/data/middle-platform/config/`。

## 3. 迁移先于启动

只在数据库初始化或经过审查的升级发布时单独执行：

```bash
/data/middle-platform/bin/migrate -config /data/middle-platform/config/config-center.yaml
```

新空库通过当前迁移器建立 V11 基线；已有低于 V11 的权威库必须先用旧版本迁移工具升级。迁移前要备份并停写；多个 config-center 实例不得各自启动迁移。`dirty=1` 或迁移异常时检查实际表结构，禁止篡改迁移表。参见 [数据库说明](database.md)。

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

对于 `sdk-core-1` / `sdk-core-2`，可复用 `bin/sdk-core` 与 `config/sdk-core.yaml`，但分别配置 `SDK_CORE_ADDR=":8211"` / `":8212"`、独立 `[program:...]` 名称和日志文件。接入端同理使用自己的 DSN 与端口。配置变更后：

```bash
sudo supervisorctl reread
sudo supervisorctl update
sudo supervisorctl status
sudo supervisorctl restart config-center
sudo supervisorctl tail -f config-center
```

生产机原有 Supervisor 配置应以实际路径、命名和用户为准，**不要因为示例而覆盖已有服务定义**。如改用 `systemd`，请配置 `Restart=on-failure`、`KillSignal=SIGTERM`、`TimeoutStopSec=40s`，日志由 `journald` 收集；两种管理器不要同时控制同一个进程。

## 5. Nginx 网关与访问边界

- 对外业务域名：只转发 `/api/core/` 到 SDK Core 多实例。
- 管理域名或受保护内网：`/api/config/` 转发到配置中心，后台静态页来自 `admin/`；对管理和同步路径设置白名单/认证。
- `/internal/config/callback` 只允许配置中心从可信网络访问接入端；验证 HMAC 和时间窗口并不能替代网络边界。

```nginx
upstream sdk_core {
    server 127.0.0.1:8211;
    server 127.0.0.1:8212;
}
# 以下 location 仅放在对外 SDK API 对应的 server 中
location ^~ /api/core/ {
    proxy_pass http://sdk_core;
}
```

Nginx 不会因 `/health/ready` 自动从静态 upstream 中摘除后端；需要明确的流量摘除流程或具备主动健康检查的网关。

## 6. 安全发布与回滚

1. 备份权威库并确认新旧代码与数据库版本的兼容性。涉及 DDL 时先停写，在单独的迁移窗口执行 `migrate`。
2. 先把发布包解压到临时目录，校验目标 Linux 架构、权限和归档内容。**绝不直接向正在运行的 `bin/` 覆盖归档。**
3. 逐节点从 Nginx upstream 摘流量，确认其他节点可服务；停止目标进程，备份旧二进制，用同一文件系统的安全替换方式部署新版本，再启动。
4. 验证 `/health/live`、`/health/ready`、关键 API、日志和必要的配置同步后恢复流量，再更新下一节点。
5. 回滚二进制前检查数据库兼容性。**二进制回滚不会自动回滚 MySQL DDL**；不应盲目恢复旧程序写入新表结构。

## 7. 日志、停服与排障

三个 Go 进程共用 `shared/logging`（`log/slog`），YAML 示例：

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
