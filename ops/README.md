# 生产运维配置

生产目录统一为 `/data/middle-platform`。本目录提供 Supervisor、Monit 和 Nginx 配置模板及运维脚本。整个 `ops/` 不进入发布包，管理员首次部署时手动复制所需文件，后续独立维护；应用发布不会上传或覆盖这些文件。

本阶段不包含 MySQL 备份、Binlog 归档或数据库恢复脚本。

## 进程与实例拓扑

| 服务组 | Supervisor program | 实例 | 本机监听端口 |
|---|---|---:|---|
| 配置中心 | `config-center:config-center-1..2` | 2 | 8201–8202 |
| SDK Core | `sdk-core:sdk-core-1..4` | 4 | 8211–8214 |
| SDK Service | `sdk-service:sdk-service-1..4` | 4 | 8221–8224 |

三个服务组均只监听 `127.0.0.1`，由本机 Nginx 接收外部流量。多个 Config Center 和 SDK Core 实例通过共享 MySQL 协调：Webhook 投递使用数据库租约，SDK Core 订单发货使用行锁、`SKIP LOCKED` 和租约。SDK Core 的 `orderNotify.workers` 是每进程配置，4 个实例会把总 Worker 数放大为单实例值的 4 倍，请按 MySQL 与游戏回调承载能力配置。

`config-center-access` 不属于当前 2/4/4 拓扑，保持可选且默认不启用。

### MySQL 连接池容量

当前代码的每实例连接池上限为 Config Center 20、SDK Core 20、SDK Service 10。按 2/4/4 计算，理论最大连接数为：

`2 × 20 + 4 × 20 + 4 × 10 = 160`

这是连接池上限之和，不代表启动后立即建立 160 条连接。MySQL `max_connections` 还要为管理连接、迁移工具、监控和其他应用预留余量。上线前请按实际 DSN、并发和 MySQL 规格核对；不要只按单实例配置估算。

SDK Core 的 `orderNotify.workers: 4` 是每进程配置，4 个实例最多同时运行 16 个发货 Worker；Config Center 每实例 Webhook 并发为 4，2 个实例合计最多 8 个投递并发。

## Supervisor

模板：`supervisor/middle-platform.conf`。

本部署按要求让 Go 实例以 `root` 运行：Supervisor 的 systemd unit 必须由 root 启动，模板中的每个 program 显式设置 `user=root`。SSH 发布账号仍建议使用独立的 `deploy` 用户，不要为了运行 Go 程序而开放 root SSH 登录。root 运行会扩大应用漏洞的影响范围，请确保二进制、YAML、日志目录和发布权限只允许可信管理员修改。

每个 Go 实例的 Supervisor 环境变量都设置 `GOMEMLIMIT=128MiB`。这是 Go Runtime 软限制，不是 RSS/cgroup 硬上限；Go Runtime 管理的内存会更积极触发 GC，但二进制映射、内核代管内存和部分外部内存不在该限制内。若要硬性隔离，后续仍需单独配置 cgroup v2。

Supervisor 使用三个异构分组：`config-center`、`sdk-core`、`sdk-service`。组内进程名在 Supervisor 中带组前缀，例如 `config-center:config-center-1`。可以使用 `supervisorctl status config-center:*`、`supervisorctl restart sdk-core:*` 等命令按组管理。

**从旧版 2/5/5 首次迁移时注意：** 旧配置的进程标识是 `sdk-core-1`，新分组后的标识是 `sdk-core:sdk-core-1`。替换 Supervisor 配置后执行 `reread/update` 会切换进程组标识，并移除 `sdk-core-5`、`sdk-service-5`；这次配置迁移可能重启全部 10 个实例，不属于逐实例滚动发布。请先备份 Supervisor 配置，在维护窗口完成迁移，确认 2/4/4 全部 RUNNING 且 Ready 后，再启用新的 Nginx upstream 和后续滚动发布。

如果服务器已有同名 program，先备份并合并配置，不要重复定义。SSH 发布用户也必须能够通过 Supervisor control socket 执行 `supervisorctl status/restart`；按实际发行版配置 socket 所属组和权限，不要把管理 socket 暴露给普通业务用户。完成配置后：

```bash
sudo supervisorctl reread
sudo supervisorctl update
sudo supervisorctl status
```

Supervisor 自身由 systemd 管理并设置开机启动。Debian/Ubuntu 常见 unit 名称为 `supervisor`，RHEL/CentOS 常见为 `supervisord`；先用 `systemctl list-unit-files | grep -E 'supervisor|supervisord'` 确认实际名称，再执行 `sudo systemctl enable --now <unit>`。

模板采用 `autorestart=unexpected`、`startsecs=30`、`startretries=20`、`stopsignal=TERM`、`stopwaitsecs=60` 和 `stopasgroup=true`。正常 SIGTERM 退出不会被当成异常退出重新拉起。

## Monit

Monit 由 systemd 独立管理，不交给 Supervisor 管理。它只负责检测已经进入 Supervisor RUNNING 状态但 HTTP 不再响应的进程，并通过 `monit/restart-supervisor-program.sh` 将逻辑实例名映射为 `group:program` 后请求 Supervisor 重启。进程退出、启动失败仍由 Supervisor 自己处理。 Monit 检查和重启脚本共享 `/data/middle-platform/.publish.lock`（需要系统提供 `flock`，通常由 util-linux 提供）；部署持有锁时 Monit 会跳过本轮操作，Monit 正在重启时发布会在改文件前退出，避免并发重启。

主配置 `/etc/monit/monitrc` 需包含：

```
set daemon 10
```

将 `monit/middle-platform.conf` 安装到主配置实际 include 的目录（常见为 `/etc/monit/conf-enabled/` 或 `/etc/monit/conf.d/`），然后：

```bash
sudo chmod 0755 /data/middle-platform/ops/monit/check-http-live.sh
sudo chmod 0755 /data/middle-platform/ops/monit/restart-supervisor-program.sh
sudo monit -t
sudo systemctl enable --now monit
sudo monit reload
sudo monit status
```

Monit 每 10 秒检查一次，连续 3 个周期 Liveness 失败后才重启实例。它不使用 Readiness 作为重启条件：MySQL 暂时不可用时，`/health/ready` 返回 503，但 `/health/live` 保持 200，避免数据库故障触发应用重启风暴。

确保 Monit 运行用户可以调用 `supervisorctl status/restart`，并能打开共享的 `/data/middle-platform/.publish.lock`（默认 systemd Monit 以 root 运行）。Supervisor 与 Monit 的 unit 均应设置开机自启；检查状态可用 `systemctl status <unit>`、`monit status` 和 `supervisorctl status`。

## Nginx 与安全滚动发布

Nginx 站点配置统一为 [`nginx/middle-platform.conf`](nginx/middle-platform.conf)，同一个文件内包含三组 upstream、HTTP/HTTPS、SSL、API location、公共代理参数和 Vue 管理后台。三个 upstream 初始配置保存在 Git 仓库根目录 [`run/nginx/`](../run/nginx/)，仅供首次部署手动复制或参考，不随发布包交付；服务器实际运行状态保存在 `/data/middle-platform/run/nginx/`，由滚动发布控制脚本维护。首次创建方式见 [Nginx 运维说明](nginx/README.md)。

项目不提供 Nginx 或 Supervisor 的安装脚本，也不会自动覆盖系统配置。管理员手动将站点配置复制到 `/etc/nginx/conf.d/middle-platform.conf`，按 Nginx 运维说明创建三个动态 upstream 状态文件，并在 `/etc/nginx/nginx.conf` 的 main context 设置 `worker_shutdown_timeout 75s`。Supervisor 继续使用本目录的单文件模板 `supervisor/middle-platform.conf`，由管理员按服务器现有 Supervisor 配置方式加载。

当前采用 Nginx Open Source 被动健康检查（`max_fails=1 fail_timeout=10s`），观察真实请求的连接/超时失败；它不会主动轮询 `/health/ready`。故障实例暂时避开，超过失败窗口后通过后续真实请求重新尝试。滚动发布时则由发布脚本将单个实例标记为 `down`、reload Nginx、等待旧 worker 排空，再重启该实例并验证 3 次连续 Ready，最后重新加入 upstream。

首次部署须从仓库手动复制 `ops/nginx/upstream-control.sh` 到服务器 `/data/middle-platform/ops/nginx/upstream-control.sh` 并设置 0755 执行权限。使用 Monit 时也须手动复制 `ops/monit/check-http-live.sh`、`ops/monit/restart-supervisor-program.sh` 到服务器对应目录，并设置 0755 权限。日常发布不会更新这些脚本；如有变更，须由管理员在非发布期间手动同步。

SSH 发布用户必须拥有动态 upstream 状态目录和共享发布锁的写权限，并拥有受限的免密 sudo 权限，只允许执行实际路径下的 `nginx -t`、`nginx -T` 和 `systemctl reload nginx`。不要授予部署用户不受限制的 sudo。

发布前在本地 `publish/publish.config.json` 配置 SSH 连接信息，`remoteDirectory` 使用 `/data/middle-platform`。固定的 2/4/4 实例清单由发布脚本定义，无需重复配置；可选的 `config-center-access` 可通过 `services` 配置。默认发布会按实例逐个滚动，不执行 `supervisorctl restart all`。如果实例重启或 Ready 失败，发布停止并保持该实例从 Nginx 摘除；不会自动回滚二进制。

## 健康检查与数据库边界

四个 Go 服务都提供 `/health/live` 和 `/health/ready`。当前 2/4/4 对外流量由 Nginx 路由：

- `/api/config/` → Config Center
- `/api/core/` → SDK Core
- `/api/service/` → SDK Service

健康探针直接访问各实例本机端口，例如：

```bash
curl -i http://127.0.0.1:8211/health/live
curl -i http://127.0.0.1:8211/health/ready
```

数据库连接池会在后续操作中尝试重建失效连接，但单条失败 SQL 不保证自动重放。不要对订单登记、支付确认等有副作用的写操作做无条件重试；应依靠现有幂等键、事务和数据库租约。

在预发布环境演练进程崩溃、HTTP 假死、MySQL 短暂断连、SIGTERM 和 Worker 任务恢复。不要在生产环境用真实订单做故障注入。

本阶段是单机多进程滚动部署，不提供跨机器故障切换、MySQL 高可用、自动二进制回滚或严格零错误保证。
