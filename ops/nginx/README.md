# Nginx 单文件站点配置与滚动发布

本配置适用于 `/data/middle-platform` 单机部署。站点配置统一维护在 `middle-platform.conf`，包含三组 upstream、HTTP/HTTPS、SSL、API 路由、代理公共参数和 Vue 管理后台。

## 监听队列与突发建连

站点配置的 80/443 监听使用 `backlog=8192`。Nginx 实际生效的 accept 队列取该值与内核 `net.core.somaxconn` 的较小者；内核默认 128 时，突发连接在新连接被 accept 之前就可能被丢弃，表现为入口 5xx 或连接失败而应用 CPU 并不高。

仓库提供内核参数文件 `ops/sysctl.d/99-listen-backlog.conf`（`net.core.somaxconn = 8192`）。首次部署或在其他机器部署时需要手动安装，并同步检查内核参数：

```bash
sudo install -D -m 0644 /data/middle-platform/ops/sysctl.d/99-listen-backlog.conf /etc/sysctl.d/99-listen-backlog.conf
sudo sysctl --system
sysctl -n net.core.somaxconn
```

该文件与 `10-timezone.conf` 一样手动安装，不随发布包交付，也不会覆盖服务器已有配置。如服务器已通过其他 sysctl 文件设置 `net.core.somaxconn`，请合并而不是并列写入，避免后加载的文件静默覆盖。

`listen` 的 backlog 只在 Nginx 进程启动时传给内核，`systemctl reload nginx` 不会重建监听套接字。修改 `middle-platform.conf` 的 `listen` 行或上述内核参数后，都必须在维护窗口 `systemctl restart nginx`，可能造成短暂入口中断；安排与 `10-timezone.conf` 的安装一起执行。`99-listen-backlog.conf` 的编号确保它晚于多数发行版默认参数加载。

## 实例拓扑

| Upstream | Supervisor programs | Local ports |
|---|---|---|
| `config_center` | `config-center-1..2` | 8201–8202 |
| `sdk_core` | `sdk-core-1..4` | 8211–8214 |
| `sdk_service` | `sdk-service-1..6` | 8221–8226 |

Supervisor 使用 `config-center`、`sdk-core`、`sdk-service` 三个异构分组；进程名带组前缀，例如 `sdk-core:sdk-core-1`。所有 Go 实例只监听 loopback，Nginx 是唯一公网入口。当前 2/4/6 拓扑不包含可选的 `config-center-access`。

## 配置文件结构

- `middle-platform.conf`：唯一需要维护的 Nginx 站点配置，包含三个 upstream 定义、80/443 server、三个 API location 和管理后台。
- [`run/nginx/`](../../run/nginx/)：Git 仓库根目录下的三个 upstream 初始配置，仅供首次部署时手动复制或参考，不进入发布包。
- `/data/middle-platform/run/nginx/*.conf`：服务器上的三个动态 upstream 实例列表，由 `upstream-control.sh` 在滚动发布时添加或移除 `down`。服务器运行状态独立于仓库初始配置。
- `upstream-control.sh`：负责校验、摘除、恢复 upstream 实例。仓库整个 `ops/` 不进入发布包；首次部署须手动复制该脚本到服务器 `/data/middle-platform/ops/nginx/upstream-control.sh`，设置 0755 执行权限。日常发布使用服务器现有脚本，不覆盖它。

站点配置中的三个 upstream 块直接写在 `middle-platform.conf`。块内只通过 `include` 引用服务器运行时实例列表，以便滚动发布可以安全地摘除单个实例，而不改写整份站点配置。

## 手动配置

Nginx 和 Supervisor 由管理员按服务器现状安装、管理；项目不提供 Nginx/Supervisor 安装脚本，也不会自动覆盖系统配置。

### 从旧版配置迁移

首次切换到单文件配置前，先备份旧站点配置，并检查 Nginx 当前实际加载的 upstream 定义：

```bash
sudo nginx -T 2>&1 | grep -nE 'upstream (config_center|sdk_core|sdk_service)'
```

如果旧版单独使用了 `/etc/nginx/conf.d/middle-platform-upstreams.conf`，必须先备份并停用它，避免新旧配置同时定义相同 upstream：

```bash
timestamp=$(date +%Y%m%d%H%M%S)
backup_dir=/etc/nginx/backup/middle-platform
sudo install -d -m 0700 "$backup_dir"

for file in middle-platform.conf middle-platform-upstreams.conf middle-platform-locations.conf proxy-common.conf; do
    source="/etc/nginx/conf.d/$file"
    if sudo test -f "$source"; then
        sudo cp -a "$source" "$backup_dir/$file.$timestamp"
    fi
done

legacy_upstream=/etc/nginx/conf.d/middle-platform-upstreams.conf
if sudo test -f "$legacy_upstream"; then
    sudo mv "$legacy_upstream" "$legacy_upstream.disabled.$timestamp"
fi
```

如果 `nginx -T` 显示其他文件仍定义了这三个 upstream，也必须先备份并停用对应旧定义。不要删除备份；确认新配置通过 `nginx -t` 且业务正常后，再按运维保留策略清理。

1. 确认 `/etc/nginx/nginx.conf` 的 `http {}` 中包含：

```nginx
include /etc/nginx/conf.d/*.conf;
```

2. 从仓库手动上传所需 Nginx 配置与控制脚本到服务器对应位置（不随应用发布包交付），设置控制脚本执行权限，并将唯一站点配置复制到 Nginx 站点目录：

```bash
sudo chmod 0755 /data/middle-platform/ops/nginx/upstream-control.sh
sudo cp /data/middle-platform/ops/nginx/middle-platform.conf /etc/nginx/conf.d/middle-platform.conf
```

从 Windows 上传时确保 `.sh` 使用 LF 换行符（仓库 `.gitattributes` 已约束）。已有 CRLF 脚本可在服务器执行 `sed -i 's/\r$//' /data/middle-platform/ops/nginx/upstream-control.sh` 后再设置执行权限。发布会在替换应用文件前检查控制脚本的 CRLF 与 Bash 语法。

3. 首次手动创建三个动态 upstream 实例列表。可以手动复制仓库根目录的 `run/nginx/*.conf` 至服务器对应目录（仅复制缺失文件），也可以按以下命令创建。将 `deploy` 替换为实际 SSH 发布用户。以下命令只创建缺失文件，不覆盖已有运行状态：

```bash
deploy_user=deploy
deploy_group=$(id -gn "$deploy_user")
runtime_dir=/data/middle-platform/run/nginx

sudo install -d -o "$deploy_user" -g "$deploy_group" -m 0750 "$runtime_dir"

if [[ ! -e "$runtime_dir/config-center-upstream.conf" && ! -L "$runtime_dir/config-center-upstream.conf" ]]; then
    sudo tee "$runtime_dir/config-center-upstream.conf" >/dev/null <<'EOF'
server 127.0.0.1:8201 max_fails=1 fail_timeout=10s;
server 127.0.0.1:8202 max_fails=1 fail_timeout=10s;
EOF
fi

if [[ ! -e "$runtime_dir/sdk-core-upstream.conf" && ! -L "$runtime_dir/sdk-core-upstream.conf" ]]; then
    sudo tee "$runtime_dir/sdk-core-upstream.conf" >/dev/null <<'EOF'
server 127.0.0.1:8211 max_fails=1 fail_timeout=10s;
server 127.0.0.1:8212 max_fails=1 fail_timeout=10s;
server 127.0.0.1:8213 max_fails=1 fail_timeout=10s;
server 127.0.0.1:8214 max_fails=1 fail_timeout=10s;
EOF
fi

if [[ ! -e "$runtime_dir/sdk-service-upstream.conf" && ! -L "$runtime_dir/sdk-service-upstream.conf" ]]; then
    sudo tee "$runtime_dir/sdk-service-upstream.conf" >/dev/null <<'EOF'
server 127.0.0.1:8221 max_fails=1 fail_timeout=10s;
server 127.0.0.1:8222 max_fails=1 fail_timeout=10s;
server 127.0.0.1:8223 max_fails=1 fail_timeout=10s;
server 127.0.0.1:8224 max_fails=1 fail_timeout=10s;
server 127.0.0.1:8225 max_fails=1 fail_timeout=10s;
server 127.0.0.1:8226 max_fails=1 fail_timeout=10s;
EOF
fi

for file in "$runtime_dir/config-center-upstream.conf" "$runtime_dir/sdk-core-upstream.conf" "$runtime_dir/sdk-service-upstream.conf"; do
    if [[ -L "$file" || ! -f "$file" ]]; then
        echo "Unsafe or missing upstream file: $file" >&2
        exit 1
    fi
done

sudo chown "$deploy_user:$deploy_group" "$runtime_dir"/*.conf
sudo chmod 0640 "$runtime_dir"/*.conf

sudo touch /data/middle-platform/.publish.lock
sudo chown "$deploy_user:$deploy_group" /data/middle-platform/.publish.lock
sudo chmod 0660 /data/middle-platform/.publish.lock
```

控制脚本校验兼容 LF 和 CRLF 的实例列表；实际摘除或恢复实例时会将对应文件写为 LF，保留其他实例的 `down` 状态。

这三个文件只在首次部署时手动创建或复制。仓库初始配置不打包、不自动上传、不自动初始化或覆盖服务器文件；滚动发布期间仍由 `upstream-control.sh` 修改服务器对应实例的 `down` 状态。

4. 在 `/etc/nginx/nginx.conf` 的顶层 main context 中（`events {}` 和 `http {}` 外）设置：

```nginx
worker_shutdown_timeout 75s;
```

仓库中的 `middle-platform-main.conf.example` 仅用于说明这个必须放在 main context 的全局指令。不要把它放进 `conf.d` 的站点文件。

5. 检查并重载 Nginx：

```bash
sudo nginx -t
sudo systemctl reload nginx
```

## 部署权限

SSH 发布用户需要拥有 `/data/middle-platform/run/nginx/` 和 `/data/middle-platform/.publish.lock` 的写权限，并能通过 Supervisor control socket 执行 `supervisorctl status/restart`。

发布用户只需要受限免密 sudo 权限来执行实际路径下的 `nginx -t`、`nginx -T` 和 `systemctl reload nginx`。例如，确认系统实际命令路径后，sudoers 可以按需配置：

```
deploy ALL=(root) NOPASSWD: /usr/sbin/nginx -t, /usr/sbin/nginx -T, /usr/bin/systemctl reload nginx
```

替换为实际用户和命令路径，并使用 `visudo -cf` 校验。不要授予部署用户不受限制的 sudo。

## 被动健康检查与请求重试

三个 upstream 使用 `least_conn`、`max_fails=1 fail_timeout=10s`，keepalive 分别为 Config Center 8、SDK Core 16、SDK Service 16。keepalive 是每个 worker 的空闲连接缓存上限，不是总连接池。

Nginx Open Source 观察真实代理请求的连接/超时失败，并在失败窗口内暂时避开故障实例；它不会主动轮询 `/health/ready`。HTTP 503 会直接返回，不作为 upstream 摘除条件，避免共享 MySQL 故障导致全部实例被误摘除。

代理重试仅包含传输错误、超时、无效响应头、HTTP 502 和 504；不启用 `non_idempotent`，避免已发送的订单登记、支付 POST 请求被 Nginx 重放。

## 滚动发布行为

发布前持有共享 `.publish.lock`，检查所有选中实例的 Supervisor RUNNING、Ready 与 upstream 格式。`validate` 只有在安全检查通过且实例明确标记为 `down` 时才返回专用状态（退出码 3 与 `UPSTREAM_DOWN` 标记）；其它校验失败直接停止。对于这些 down 实例，逐个确认 Supervisor RUNNING 且 `/health/ready` 连续三次返回 HTTP 200（间隔 2 秒），再通过 `recover` 移除 down、reload 并确认 worker 换代。恢复成功后再次校验；其后才替换应用文件。恢复失败时不替换应用文件；已经成功恢复的其它健康实例不重新摘除。未选中实例不自动恢复。新版发布脚本需与服务器新版 `upstream-control.sh` 配套使用。

注意：选中范围内人为维护性摘除但仍健康的实例也会被恢复；维护期间不要发布该服务，也不要并行手动修改 upstream。

对每个选中的 Supervisor 实例，按配置顺序逐个执行：

1. 验证 Nginx 正在运行、加载了对应 upstream 和 location，且该组至少还有一个可用实例。
2. 在对应的 `/data/middle-platform/run/nginx/*.conf` 中将当前实例标记为 `down`。
3. 执行 `nginx -t` 并 graceful reload。
4. Reload 前记录 systemd 管理的 Nginx master PID 和旧 worker PID；等待旧 PID 全部消失且出现新一代 active worker。只识别该 master 的真实 worker 进程；Reload 后进程查询失败、master 变化或超时均停止发布，实例保持摘除。顶层 `worker_shutdown_timeout 75s` 与发布默认等待上限 90 秒不变；超时日志输出剩余 PID。发布期间不得并行执行其他 Nginx reload/restart 或热升级。
5. 仅重启当前 Supervisor program。
6. 要求 Supervisor 状态为 RUNNING，并且本机 `/health/ready` 连续 3 次返回 HTTP 200。
7. 移除 `down`，再次校验并 reload Nginx，将实例恢复到流量中。

如果重启、Ready 或 Nginx reload 失败，发布立即停止，后续实例不再重启；已经摘除的实例保持 down（如 reload / worker 换代未确认，还需检查实际运行状态）。修复问题后可手动恢复；下次发布也会按上述预检规则自动恢复选中且健康的 down 实例。

完整发布逐个处理 12 个实例，不会停止整个 upstream 组。它提供滚动可用性，不承诺严格零错误；应用错误、共享 MySQL 故障、Nginx reload 失败或所有实例同时故障仍可能影响业务。发布不提供自动二进制回滚。

## 手动操作

```bash
# 校验实例
/data/middle-platform/ops/nginx/upstream-control.sh validate sdk-core 8211

# 摘除 / 恢复实例（通常由发布脚本调用）
/data/middle-platform/ops/nginx/upstream-control.sh drain sdk-core 8211
/data/middle-platform/ops/nginx/upstream-control.sh enable sdk-core 8211
```

重复 `drain` 已标记 down 的实例时会重新 reload 并确认 worker 换代，而不是仅根据磁盘标记判断已排空。恢复实例之前应确认 Supervisor RUNNING 和 `/health/ready` 正常。

执行手动操作时使用实际发布用户；操作后检查 `/data/middle-platform/run/nginx/*.conf` 并运行 `nginx -t`。

## 日志时区

站点配置使用 Nginx 的 `$time_iso8601` 输出 ISO 8601 时间和显式时区偏移。该变量按 Nginx 进程的本地时区生成；为避免修改服务器全局时区，仓库提供仅作用于 Nginx 服务的 systemd drop-in：`ops/systemd/nginx.service.d/10-timezone.conf`。

首次配置或更新时，管理员手动将该文件安装到 Nginx 实际 systemd unit 的 drop-in 目录（常见 unit 为 `nginx.service`）：

```bash
sudo install -D -m 0644 /data/middle-platform/ops/systemd/nginx.service.d/10-timezone.conf /etc/systemd/system/nginx.service.d/10-timezone.conf
sudo systemctl daemon-reload
sudo nginx -t
sudo systemctl restart nginx
```

重启 Nginx 才会让新进程取得 `TZ=Asia/Shanghai`；安排在维护窗口执行，可能造成短暂入口中断。若 unit 名称不同，按实际名称调整 drop-in 目录和重启命令。此设置只改变 Nginx 进程的日志时区，不修改操作系统时区，也不影响 Go 服务的显式日志时区。

该重启与[监听队列与突发建连](#监听队列与突发建连)中的 `restart nginx` 是同一操作，建议在同一次维护窗口中一并完成 sysctl、时区 drop-in 和站点配置的更新。
