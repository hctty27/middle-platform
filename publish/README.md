# 本地打包与安全滚动发布

在 Windows/PowerShell 上构建 Linux ELF、Vue 静态文件和 SQL 发布包，经 SSH/SCP 上传到 Linux 服务器。生产目录统一为 `/data/middle-platform`。数据库 SQL 仅分发，不会自动执行。

## 生产实例拓扑

固定的 2/2/4 实例及健康检查端口由发布脚本定义，无需在 `publish.config.json` 中重复配置。实际文件请从 `publish.config.example.json` 复制，再填写 SSH 主机、端口和用户。文件已加入 `.gitignore`，不要提交账号或密码。

如果还需要自动重启不属于 2/2/4 Nginx upstream 的 `config-center-access`，可额外加入：

```json
"services": { "config-center-access": 8291 }
```

旧配置中的固定实例条目仍受支持，但名称和端口必须与脚本一致。未配置 `config-center-access` 时，不会自动重启它。

## 首次部署前必须完成

1. 按 [ops/README.md](../ops/README.md) 手动加载 Supervisor、Monit 配置，并确认对应 systemd 服务已由管理员安装、启用。
2. 按 [ops/nginx/README.md](../ops/nginx/README.md) 将唯一站点配置 `ops/nginx/middle-platform.conf` 复制到 `/etc/nginx/conf.d/middle-platform.conf`，并按文档中的命令手动创建三个动态 upstream 状态文件到 `/data/middle-platform/run/nginx/`；再在 `/etc/nginx/nginx.conf` 的 main context 配置 `worker_shutdown_timeout 75s`。仓库根目录 [`run/nginx/`](../run/nginx/) 提供三个初始配置，可按需手动复制缺失文件。它们不随发布包交付，发布不会自动初始化或覆盖服务器上的运行时文件。
3. 确认 Nginx 已由 systemd 管理且处于 active 状态，站点配置通过 `nginx -t`。
4. 为 SSH 部署用户配置受限免密 sudo：仅允许实际路径下的 `nginx -t`、`nginx -T`、`systemctl reload nginx`。发布脚本使用 `sudo -n`，不会交互式提权。
5. 确认 8 个 Supervisor program 均存在，Supervisor 以 root 运行并加载三个 `[group:...]` 分组；进程名需使用 `group:program` 形式。每个实例设置 `GOMEMLIMIT=128MiB`（Go Runtime 软限制，不是 RSS 硬上限）。SSH 发布用户可以通过 Supervisor control socket 执行 `supervisorctl status/restart`，配置文件、DSN、密钥和日志目录已就绪。

项目提供 Nginx、Supervisor 和 Monit 的配置模板及滚动发布控制逻辑，不提供系统服务安装脚本。整个 `ops/` 与 `run/` 均不进入发布包，管理员手动复制、独立维护。首次滚动发布前必须手动部署 `/data/middle-platform/ops/nginx/upstream-control.sh` 并赋予 0755 执行权限；不存在、不可执行或为符号链接时，涉及 Nginx 的滚动发布在替换应用文件前失败。只更新文件（`-SkipRestart`）、只发布管理端或单独重启不经过 Nginx 的 config-center-access 不要求该脚本。

从当前 2/4/10 缩容到 2/2/4 时，先备份 Supervisor 与 Nginx upstream 运行文件并安排维护窗口。修改 Supervisor `[group:...]` 的 `programs` 清单后执行 `reread/update` 可能重启受影响的整组服务；务必完成配置迁移并核验所有 8 个实例 Ready 后，再执行正常的逐实例滚动发布。

如果这些前置条件未完成，正常发布会在替换应用文件前失败。首次配置 Nginx 与系统服务必须由管理员审核，不会由发布脚本自动覆盖。

## 多机器顺序发布（不控制腾讯云 CLB）

每台服务器依旧使用本机 Nginx + Supervisor 的 2/2/4 滚动更新。`rolling.ps1` 只在发布电脑上**打包一次**，依次调用原有的 `publish.ps1 -SkipBuild`；**不调用 CLB API**，不增加版本目录，也不提供自动回滚。某台发布失败将立即停止，剩余机器不再更新。CLB 本身保持正常转发，因此发布期间仍可能短暂出现请求错误。

先复制两个未跟踪的配置文件：

```powershell
Copy-Item .\publish\publish.config.example.json .\publish\publish.config.json
Copy-Item .\publish\rolling.config.example.json .\publish\rolling.config.json
```

`rolling.config.json` 只需要 `remoteDirectory` 和 `nodes` 数组（至少两台，每台 `name`、`host`、`sshPort`、`user`）。真实服务器清单由 `.gitignore` 排除，请不要提交真实服务器账号和凭据。`publish.config.json` 仍然必须存在，因为原有单机脚本会读取它；建议使用 SSH 密钥认证。

```powershell
# 只验证本地 JSON 与节点清单，不连接远端
.\publish\rolling.ps1 -Version v1.0.1 -CheckOnly

# 编译一次，并逐台执行单机安全滚动发布
.\publish\rolling.ps1 -Version v1.0.1

# 只更新 SDK Service
.\publish\rolling.ps1 -Version v1.0.2 -Service sdk-service

# 复用 dist 中同版本的现有 amd64 归档
.\publish\rolling.ps1 -Version v1.0.2 -SkipBuild
```

为避免冲突，建议只在一台固定的发布电脑或同一个 CI 部署入口执行：脚本只有**本机发布锁**，没有跨电脑的分布式锁。执行前确认每台机器已安装 Nginx、Supervisor、Monit 和 `ops/nginx/upstream-control.sh`，每台 `2/2/4` 实例全部 Ready；单机原有发布逻辑负责各实例 Nginx 摘除、重启和验活，但不会摘除整台 CLB 后端服务器。

## 使用命令

```powershell
# 完整打包，不上传
.\publish\package.ps1 -Version v1.0.0

# 完整发布：校验、逐实例 Nginx 摘除、Supervisor 重启、Ready 验活、重新加入
.\publish\publish.ps1 -Version v1.0.0

# 只滚动更新 SDK Core 的 2 个实例
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

完整发布会先在本地构建并验证归档；服务器校验 SHA-256、架构、文件类型、服务选择器、全部 2/2/4 进程与 Nginx runtime include，再原子替换文件。随后按脚本定义的方式滚动更新（仅更新实际运行二进制不一致或无法验证的实例）：

1. 将当前实例在对应 Nginx upstream 标记为 `down`。
2. 执行 `nginx -t` 并 graceful reload。
3. 等待旧 Nginx worker 退出；`worker_shutdown_timeout 75s` 限制排空时间。
4. 调用 `supervisorctl restart <group>:<program>`，例如 `supervisorctl restart sdk-core:sdk-core-1`。
5. 等待 Supervisor RUNNING，并要求本机 `/health/ready` 连续 3 次 HTTP 200。
6. 移除 `down`，再次校验并 reload Nginx，将实例重新加入流量。
7. 完成当前实例后才处理下一个实例。

发布前会验证所有 Supervisor program 和 Nginx upstream 配置。若服务器手动安装了新版 upstream-control.sh（支持 batch-v1），全量发布按 config-center / sdk-core / sdk-service 每组每轮至多一个实例并行更新：批量摘除、一次 reload 并等待旧 worker 退出，随后并行重启与连续 Ready 检查，再批量恢复健康实例。2/2/4 拓扑最多四轮。单服务仍逐实例更新；旧版 helper 自动回退到原串行模式。每组拒绝摘除最后一个可用实例；全量并行要求新旧服务版本兼容，且剩余实例能够承担流量。需要避免跨组并行时，分别使用 -Service 发布。若本轮任一实例重启或 Ready 失败，等待本轮任务结束并恢复健康实例，失败实例保持摘除，不启动下一轮。Nginx 操作失败则立即停止。修复原因并确认 Ready 后，管理员再手动恢复该实例。

这是单机多实例滚动更新，不承诺严格零错误：共享 MySQL 故障、应用逻辑错误、全部实例故障、Nginx reload 失败仍会影响业务。远端发布期间持有 `/data/middle-platform/.publish.lock`；Monit 健康检查和重启共用该锁，发布期间 Monit 跳过本轮检查，Monit 正在重启时发布会在改文件前退出。本流程不提供自动二进制回滚；发布失败时旧进程可能仍在运行，但新二进制文件已原子替换，需人工排查。

Nginx Open Source 使用被动健康检查：`max_fails=1 fail_timeout=10s`。实例故障后，真实请求触发摘除；超过失败窗口后，Nginx 会通过后续真实请求重新尝试。它不主动轮询 `/health/ready`。代理重试不启用 `non_idempotent`，避免已发送的订单登记和支付 POST 被重复提交。

生产 `config/`、`logs/` 和现有 `admin/config.js` 不会覆盖；`ops/` 不打包、不上传、不覆盖，系统配置由管理员独立维护；滚动发布调用服务器现有 `upstream-control.sh` 修改 `run/nginx/` 实例状态并重载 Nginx；SQL 仅分发，不自动执行。SSH 使用 Ed25519 私钥认证优先；首次连接应先手动确认主机指纹。

## CI 与自测

`.github/workflows/package.yml` 会验证 Linux/Windows 发布归档（只允许 `bin/`、`sql/`、`admin/`）、Shell 语法、Monit 检查、Nginx upstream 排空/恢复脚本，以及无实际 SSH 的发布冒烟测试：

```bash
python -I scripts/ci_publish_smoke.py
bash ops/nginx/test-upstream-control.sh
```

正式发布建议省略 `-Fast`。前端源码（含本地文件）、构建工具链、相关环境及 dist 内容的 SHA-256 指纹一致时，直接复用已成功构建的 dist；任一项变化或缓存缺失时重新构建，默认仍使用锁文件执行 `npm ci`。指定 `-Fast` 时，仅在依赖指纹匹配时复用 node_modules。需要强制重新构建时，可删除 admin/node_modules/.middle-platform-build.sha256；若还需重新安装依赖，则省略 -Fast。

服务器跳过字节和权限完全一致的普通文件。仅在 Supervisor 当前运行的 PID 对应 /proc/PID/exe 与本次包中的二进制 SHA-256 相同，且 Ready 检查通过时，才跳过该实例重启；不以磁盘文件是否已更新作为运行版本依据。未能读取或确认运行产物时仍执行安全滚动重启。发布日志记录打包、上传、远程预检、文件更新和滚动发布耗时。

## 配置精简升级提示

新版可调整 YAML 参数缩减为业务地址、密钥、数据库最大连接数、缓存 TTL、回调并发、游戏超时和日志开关等。上线前必须先按 [配置迁移说明](../ops/CONFIGURATION-MIGRATION.md) 清理服务器上的独立配置文件；发布包不会自动覆盖生产 YAML，旧字段在严格解析模式下会阻止服务启动。建议先备份每台机器的 `/data/middle-platform/config/`，预先清理字段后再滚动发布新版二进制。
