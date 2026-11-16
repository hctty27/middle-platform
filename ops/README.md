# 生产运维配置

本目录提供 Supervisor 与 Monit 配置模板，随发布包部署到 `/data/middle-platform/ops/`，但发布程序不会自动安装或重载系统服务配置。

本阶段不包含 MySQL 备份、Binlog 归档或数据库恢复脚本。

## Supervisor

`supervisor/middle-platform.conf` 是单机示例，包含：

- `config-center`：8201
- `sdk-core-1`：8211
- `sdk-core-2`：8212
- `sdk-service`：8221
- `config-center-access`：8291（默认注释，可选）

使用前确认 Linux 用户、配置文件路径、数据库 DSN、端口和日志目录。示例将服务监听在 `127.0.0.1`，由本机 Nginx 代理。若服务需要跨机器访问，应按网络边界调整监听地址和防火墙规则。

如果生产服务器已经存在同名 Supervisor program，不要直接重复安装。将模板中的参数合并到现有 program，避免重复定义或覆盖服务器现有配置。

首次安装或完成配置合并后：

```bash
sudo supervisorctl reread
sudo supervisorctl update
sudo supervisorctl status
```

模板采用 `autorestart=unexpected`：非零退出会重启，正常 SIGTERM 停止不会被拉起。启动阶段最多重试 20 次；服务必须连续运行 30 秒才标记为 RUNNING。正常退出等待 60 秒，超过后 Supervisor 才会强制结束进程。

`publish/publish.config.json` 中的 `services` 键名必须与 Supervisor program 名称一致，值必须是对应实例的本机健康检查端口。

## Monit

Monit 只负责检测已经进入 Supervisor RUNNING 状态的进程是否还能响应 `/health/live`，连续 3 个检查周期失败后调用 Supervisor 重启对应实例。

- Monit 检查周期建议为 10 秒。
- 检查脚本跳过 Supervisor 非 RUNNING 状态，避免与启动重试互相干扰。
- 只检查 Liveness，不检查 Readiness。
- MySQL 暂时不可用时，`/health/ready` 应返回 503，但 `/health/live` 仍返回 200，因此不会因数据库故障触发应用重启风暴。
- 当前 Nginx 使用静态 upstream，不会根据 `/health/ready` 自动摘除实例；Ready 503 是健康信号，自动流量摘除需要后续支持主动健康检查的网关或负载均衡器。

确保主配置 `/etc/monit/monitrc` 已配置：

```
set daemon 10
```

安装模板：

```bash
sudo chmod 0755 /data/middle-platform/ops/monit/check-http-live.sh
sudo install -m 0644 /data/middle-platform/ops/monit/middle-platform.conf /etc/monit/conf-enabled/middle-platform
sudo monit -t
sudo monit reload
sudo monit status
```

不同发行版的 Monit include 目录可能是 `/etc/monit/conf.d/`，按主配置中的 include 路径安装。确保 Monit 运行用户有权限执行 `supervisorctl status/restart`。

可选的 config-center-access 实例只有在实际部署并启用对应 Supervisor program 后，才取消 Monit 配置中的注释。

## 数据库连接恢复边界

四个服务继续复用长期存活的 `sql.DB` 连接池。连接失效后，连接池会在后续数据库操作中尝试建立新连接；各服务的 `/health/ready` 使用有超时的 `PingContext` 检查当前可用性。

单条失败 SQL 不保证被自动重放。不要给订单登记、支付确认或其他有副作用的写操作增加无条件重试；应由现有幂等键、事务和持久化任务租约处理重试。

## 验证

```bash
bash -n /data/middle-platform/ops/monit/check-http-live.sh
bash /data/middle-platform/ops/monit/test-check-http-live.sh
curl -i http://127.0.0.1:8211/health/live
curl -i http://127.0.0.1:8211/health/ready
```

在预发布环境验证进程崩溃、HTTP 假死、MySQL 短暂断连、SIGTERM 和 Worker 任务恢复。不要在生产环境用真实订单做故障注入。

本阶段不提供多机器故障切换、MySQL 高可用、零停机滚动更新或自动版本回滚。
