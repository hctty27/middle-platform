# 本地打包与安全滚动发布

在 Windows/PowerShell 上构建 Linux ELF、Vue 静态文件和运维模板，经 SSH/SCP 上传到 Linux 服务器。生产目录统一为 `/data/middle-platform`。数据库 SQL 仅分发，不会自动执行。

## 生产实例拓扑

固定的 2/4/4 实例及健康检查端口由发布脚本定义，无需在 `publish.config.json` 中重复配置。实际文件请从 `publish.config.example.json` 复制，再填写 SSH 主机、端口和用户。文件已加入 `.gitignore`，不要提交账号或密码。

如果还需要自动重启不属于 2/4/4 Nginx upstream 的 `config-center-access`，可额外加入：

```json
"services": { "config-center-access": 8291 }
```

旧配置中的固定实例条目仍受支持，但名称和端口必须与脚本一致。未配置 `config-center-access` 时，不会自动重启它。

## 首次部署前必须完成

1. 按 [ops/README.md](../ops/README.md) 安装并启用 Supervisor、Monit。
2. 按 [ops/nginx/README.md](../ops/nginx/README.md) 安装 Nginx upstream include、合并三个 API location、配置 `worker_shutdown_timeout 75s`，初始化 `/data/middle-platform/run/nginx/`。
3. 确认 Nginx 由 systemd 管理且处于 active 状态。
4. 为 SSH 部署用户配置受限免密 sudo：仅允许实际路径下的 `nginx -t`、`nginx -T`、`systemctl reload nginx`。发布脚本使用 `sudo -n`，不会交互式提权。
5. 确认 10 个 Supervisor program 均存在，Supervisor 以 root 运行并加载三个 `[group:...]` 分组；进程名需使用 `group:program` 形式。每个实例设置 `GOMEMLIMIT=128MiB`（Go Runtime 软限制，不是 RSS 硬上限）。SSH 发布用户可以通过 Supervisor control socket 执行 `supervisorctl status/restart`，配置文件、DSN、密钥和日志目录已就绪。

首次从旧版未分组配置迁移时，先备份 Supervisor 配置并安排维护窗口。切换到 `[group:...]` 后，进程标识会从 `sdk-core-1` 变为 `sdk-core:sdk-core-1`，`supervisorctl reread/update` 可能重启所有实例并移除第 5 个 Core/Service 实例；完成后再执行正常的逐实例滚动发布。

如果这些前置条件未完成，正常发布会在替换应用文件前失败。首次安装 Nginx 与系统服务配置必须由管理员审核，不会由发布脚本自动覆盖。

## 使用命令

```powershell
# 完整打包，不上传
.\scripts\package.ps1 -Version v1.0.0

# 完整发布：校验、逐实例 Nginx 摘除、Supervisor 重启、Ready 验活、重新加入
.\publish\publish.ps1 -Version v1.0.0

# 只滚动更新 SDK Core 的 4 个实例
.\publish\publish.ps1 -Service sdk-core -Version v1.0.1

# 只滚动更新配置中心的 2 个实例
.\publish\publish.ps1 -Service config-center -Version v1.0.2

# 只滚动更新 SDK Service 的 4 个实例
.\publish\publish.ps1 -Service sdk-service -Version v1.0.3

# 只更新文件，不重启、不执行 Nginx 摘除
.\publish\publish.ps1 -Version v1.0.4 -SkipRestart
```

`-Service` 支持 `all`（默认）、`config-center`、`sdk-core`、`sdk-service`、可选的 `config-center-access` 和 `admin`。单独发布后端服务时不构建 Vue；`admin` 只构建前端。兼容 `-SkipFrontend`、`-Arch amd64|arm64`、`-Fast`、`-SkipBuild`、`-SkipRestart` 和覆盖服务器参数。

## 滚动发布流程

完整发布会先在本地构建并验证归档；服务器校验 SHA-256、架构、文件类型、服务选择器、全部 2/4/4 进程与 Nginx runtime include，再原子替换文件。随后按脚本定义的实例顺序逐个执行：

1. 将当前实例在对应 Nginx upstream 标记为 `down`。
2. 执行 `nginx -t` 并 graceful reload。
3. 等待旧 Nginx worker 退出；`worker_shutdown_timeout 75s` 限制排空时间。
4. 调用 `supervisorctl restart <group>:<program>`，例如 `supervisorctl restart sdk-core:sdk-core-1`。
5. 等待 Supervisor RUNNING，并要求本机 `/health/ready` 连续 3 次 HTTP 200。
6. 移除 `down`，再次校验并 reload Nginx，将实例重新加入流量。
7. 完成当前实例后才处理下一个实例。

发布前会验证所有 Supervisor program 和 Nginx upstream 配置。一次只摘除一个实例，并拒绝摘除某个 upstream 的最后一个可用实例。若重启、Ready 或 Nginx reload 失败，发布立即停止，故障实例保持摘除，未处理的实例不再重启。修复原因并确认 Ready 后，管理员再手动恢复该实例。

这是单机多实例滚动更新，不承诺严格零错误：共享 MySQL 故障、应用逻辑错误、全部实例故障、Nginx reload 失败仍会影响业务。远端发布期间持有 `/data/middle-platform/.publish.lock`；Monit 健康检查和重启共用该锁，发布期间 Monit 跳过本轮检查，Monit 正在重启时发布会在改文件前退出。本流程不提供自动二进制回滚；发布失败时旧进程可能仍在运行，但新二进制文件已原子替换，需人工排查。

Nginx Open Source 使用被动健康检查：`max_fails=1 fail_timeout=10s`。实例故障后，真实请求触发摘除；超过失败窗口后，Nginx 会通过后续真实请求重新尝试。它不主动轮询 `/health/ready`。代理重试不启用 `non_idempotent`，避免已发送的订单登记和支付 POST 被重复提交。

生产 `config/`、`logs/` 和现有 `admin/config.js` 不会覆盖；`ops/` 只交付模板，不会自动安装或重载 Supervisor/Monit/Nginx；SQL 仅分发，不自动执行。SSH 使用 Ed25519 私钥认证优先；首次连接应先手动确认主机指纹。

## CI 与自测

`.github/workflows/package.yml` 会验证 Linux/Windows 发布归档、运维模板、Shell 语法、Monit 检查、Nginx upstream 排空/恢复脚本，以及无实际 SSH 的发布冒烟测试：

```bash
python -I publish/ci_publish_smoke.py
bash ops/nginx/test-upstream-control.sh
```

正式发布建议省略 `-Fast`，使用锁文件执行 `npm ci`。
