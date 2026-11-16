# Nginx 单文件站点配置与滚动发布

本配置适用于 `/data/middle-platform` 单机部署。站点配置统一维护在 `middle-platform.conf`，包含三组 upstream、HTTP/HTTPS、SSL、API 路由、代理公共参数和 Vue 管理后台。

## 实例拓扑

| Upstream | Supervisor programs | Local ports |
|---|---|---|
| `config_center` | `config-center-1..2` | 8201–8202 |
| `sdk_core` | `sdk-core-1..4` | 8211–8214 |
| `sdk_service` | `sdk-service-1..4` | 8221–8224 |

Supervisor 使用 `config-center`、`sdk-core`、`sdk-service` 三个异构分组；进程名带组前缀，例如 `sdk-core:sdk-core-1`。所有 Go 实例只监听 loopback，Nginx 是唯一公网入口。当前 2/4/4 拓扑不包含可选的 `config-center-access`。

## 配置文件结构

- `middle-platform.conf`：唯一需要维护的 Nginx 站点配置，包含三个 upstream 定义、80/443 server、三个 API location 和管理后台。
- `/data/middle-platform/run/nginx/*.conf`：服务器上的三个动态 upstream 实例列表，由 `upstream-control.sh` 在滚动发布时添加或移除 `down`。它们不属于 Git 仓库中的 `ops/`，也不是独立站点配置。
- `upstream-control.sh`：负责校验、摘除、恢复 upstream 实例。

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

2. 将唯一站点配置复制到 Nginx 站点目录：

```bash
sudo cp /data/middle-platform/ops/nginx/middle-platform.conf /etc/nginx/conf.d/middle-platform.conf
```

3. 首次手动创建三个动态 upstream 实例列表。将 `deploy` 替换为实际 SSH 发布用户。以下命令只创建缺失文件，不覆盖已有运行状态：

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

这三个文件只在首次部署时手动创建。后续不要从发布包覆盖它们；滚动发布期间由 `upstream-control.sh` 修改对应实例的 `down` 状态。

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

对每个选中的 Supervisor 实例，按配置顺序逐个执行：

1. 验证 Nginx 正在运行、加载了对应 upstream 和 location，且该组至少还有一个可用实例。
2. 在对应的 `/data/middle-platform/run/nginx/*.conf` 中将当前实例标记为 `down`。
3. 执行 `nginx -t` 并 graceful reload。
4. 等待旧 Nginx worker 退出。顶层 `worker_shutdown_timeout 75s` 限制排空时间；超时则停止发布，实例保持摘除。
5. 仅重启当前 Supervisor program。
6. 要求 Supervisor 状态为 RUNNING，并且本机 `/health/ready` 连续 3 次返回 HTTP 200。
7. 移除 `down`，再次校验并 reload Nginx，将实例恢复到流量中。

如果重启、Ready 或 Nginx reload 失败，发布立即停止，故障实例保持摘除，后续实例不再重启。修复问题并确认 Ready 后，再手动恢复实例。

完整发布逐个处理 10 个实例，不会停止整个 upstream 组。它提供滚动可用性，不承诺严格零错误；应用错误、共享 MySQL 故障、Nginx reload 失败或所有实例同时故障仍可能影响业务。发布不提供自动二进制回滚。

## 手动操作

```bash
# 校验实例
/data/middle-platform/ops/nginx/upstream-control.sh validate sdk-core 8211

# 摘除 / 恢复实例（通常由发布脚本调用）
/data/middle-platform/ops/nginx/upstream-control.sh drain sdk-core 8211
/data/middle-platform/ops/nginx/upstream-control.sh enable sdk-core 8211
```

执行手动操作时使用实际发布用户；操作后检查 `/data/middle-platform/run/nginx/*.conf` 并运行 `nginx -t`。
