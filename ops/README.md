# 生产运维配置

生产目录统一为 `/data/middle-platform`。本目录提供 Supervisor、Monit 和 Nginx 模板；发布包会交付模板，但不会自动改写系统配置或重载系统服务。

本阶段不包含 MySQL 备份、Binlog 归档或数据库恢复脚本。

## 进程与实例拓扑

| 服务组 | Supervisor program | 实例 | 本机监听端口 |
|---|---|---:|---|
| 配置中心 | `config-center-1..2` | 2 | 8201–8202 |
| SDK Core | `sdk-core-1..5` | 5 | 8211–8215 |
| SDK Service | `sdk-service-1..5` | 5 | 8221–8225 |

三个服务组均只监听 `127.0.0.1`，由本机 Nginx 接收外部流量。多个 Config Center 和 SDK Core 实例通过共享 MySQL 协调：Webhook 投递使用数据库租约，SDK Core 订单发货使用行锁、`SKIP LOCKED` 和租约。SDK Core 的 `orderNotify.workers` 是每进程配置，5 个实例会把总 Worker 数放大为单实例值的 5 倍，请按 MySQL 与游戏回调承载能力配置。

`config-center-access` 不属于当前 2/5/5 拓扑，保持可选且默认不启用。

### MySQL 连接池容量

当前代码的每实例连接池上限为 Config Center 20、SDK Core 20、SDK Service 10。按 2/5/5 计算，理论最大连接数为：

`2 × 20 + 5 × 20 + 5 × 10 = 190`

这是连接池上限之和，不代表启动后立即建立 190 条连接。MySQL `max_connections` 还要为管理连接、迁移工具、监控和其他应用预留余量。上线前请按实际 DSN、并发和 MySQL 规格核对；不要只按单实例配置估算。

SDK Core 的 `orderNotify.workers: 4` 是每进程配置，5 个实例最多同时运行 20 个发货 Worker；Config Center 每实例 Webhook 并发为 4，2 个实例合计最多 8 个投递并发。

## Supervisor

模板：`supervisor/middle-platform.conf`。

使用前确认服务器用户 `middle-platform` 已创建，且对 `/data/middle-platform/bin`、`config`、`logs` 有所需权限。配置文件路径和数据库 DSN 必须由运维人员按实际环境准备；模板不会生成或覆盖生产密钥。

如果服务器已有同名 program，先备份并合并配置，不要重复定义。SSH 发布用户也必须能够通过 Supervisor control socket 执行 `supervisorctl status/restart`；按实际发行版配置 socket 所属组和权限，不要把管理 socket 暴露给普通业务用户。完成配置后：

```bash
sudo supervisorctl reread
sudo supervisorctl update
sudo supervisorctl status
```

Supervisor 自身由 systemd 管理并设置开机启动。Debian/Ubuntu 常见 unit 名称为 `supervisor`，RHEL/CentOS 常见为 `supervisord`；先用 `systemctl list-unit-files | grep -E 'supervisor|supervisord'` 确认实际名称，再执行 `sudo systemctl enable --now <unit>`。

模板采用 `autorestart=unexpected`、`startsecs=30`、`startretries=20`、`stopsignal=TERM`、`stopwaitsecs=60` 和 `stopasgroup=true`。正常 SIGTERM 退出不会被当成异常退出重新拉起。

## Monit

Monit 由 systemd 独立管理，不交给 Supervisor 管理。它只负责检测已经进入 Supervisor RUNNING 状态但 HTTP 不再响应的进程，并通过 `monit/restart-supervisor-program.sh` 请求 Supervisor 重启。进程退出、启动失败仍由 Supervisor 自己处理。 Monit 检查和重启脚本共享 `/data/middle-platform/.publish.lock`（需要系统提供 `flock`，通常由 util-linux 提供）；部署持有锁时 Monit 会跳过本轮操作，Monit 正在重启时发布会在改文件前退出，避免并发重启。

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

Nginx upstream、location、运行时实例列表、部署排空脚本及首次安装步骤见 [Nginx 运维说明](nginx/README.md)。

当前采用 Nginx Open Source 被动健康检查（`max_fails=1 fail_timeout=10s`），观察真实请求的连接/超时失败；它不会主动轮询 `/health/ready`。故障实例暂时避开，超过失败窗口后通过后续真实请求重新尝试。滚动发布时则由发布脚本将单个实例标记为 `down`、reload Nginx、等待旧 worker 排空，再重启该实例并验证 3 次连续 Ready，最后重新加入 upstream。

首次部署 Nginx 集成后，SSH 发布用户必须拥有受限的免密 sudo 权限，只允许执行实际路径下的 `nginx -t`、`nginx -T` 和 `systemctl reload nginx`。不要授予部署用户不受限制的 sudo。

发布前在本地 `publish/publish.config.json` 配置完整的 2/5/5 实例清单，`remoteDirectory` 使用 `/data/middle-platform`。默认发布会按实例逐个滚动，不执行 `supervisorctl restart all`。如果实例重启或 Ready 失败，发布停止并保持该实例从 Nginx 摘除；不会自动回滚二进制。

## 健康检查与数据库边界

四个 Go 服务都提供 `/health/live` 和 `/health/ready`。当前 2/5/5 对外流量由 Nginx 路由：

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
