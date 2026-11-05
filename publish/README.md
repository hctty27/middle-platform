# 本地打包与发布

在 Windows/PowerShell 上构建 Linux ELF 和 Vue 静态文件，经 SSH/SCP 上传到现有服务器目录并直接更新。**不使用 releases 版本目录，不提供自动或手动回滚功能**；数据库 SQL 永远由管理员手动执行。

## 首次配置

复制 `publish.config.example.json` 为本地 `publish.config.json`，填写 `host`、`port`、`user`、`remoteDirectory`。该文件已加入 `.gitignore`，不要提交账号或密码。

自动重启只需在本地 `publish.config.json` 配置 `services`：**键是 Supervisor 进程名，值是该实例的本机健康检查端口**。

```json
"services": {
  "config-center-1": 8201,
  "config-center-2": 8202,
  "sdk-core-1": 8211,
  "sdk-core-2": 8212,
  "sdk-core-3": 8213,
  "sdk-service": 8221
}
```

支持 `config-center`、`config-center-1`、`config-center-2` 等命名；`sdk-core`、`sdk-service` 和 `config-center-access` 同理。只接受 **模块名** 或 **模块名-正整数**，不会将 `config-center-access` 误认为 `config-center`。

例如 `-Service config-center` 只重启已配置的 `config-center-1`、`config-center-2`；全量发布仅重启 `services` 中列出的实例，不执行 `supervisorctl restart all`。发布时按 JSON 声明的顺序逐个重启，每个都使用 `http://127.0.0.1:<端口>/health/ready` 验活。后续添加实例只改 JSON，不改脚本。若未配置匹配实例，发布在本地就会终止，避免更新文件后无法重启。执行 `-SkipRestart` 时不需要配置 `services`。

仅将经过严格校验的进程名与端口计划传给远端脚本，**绝不上传含主机、密码的完整 publish.config.json**。

推荐 SSH Ed25519 私钥认证（`password` 留空，SSH 可以交互输入口令）；若配置了明文 `password`，现有兼容逻辑会临时调用 `askpass.exe`，并只响应密码提示，不会自动信任未知 SSH 主机指纹。首次发布应手动 `ssh` 确认指纹。

## 使用命令

```powershell
# 完整打包，不上传；正式构建仍使用 npm ci
.\scripts\package.ps1 -Version v1.0.0

# 单独构建 SDK Core，或者仅构建管理端
.\scripts\package.ps1 -Version local-core -Service sdk-core
.\scripts\package.ps1 -Version local-admin -Service admin -Fast

# 默认：打包、上传、校验、直接更新、重启 Supervisor 并验活
.\publish\publish.ps1 -Version v1.0.0

# 只发布 sdk-core；保留其他服务二进制和 admin
.\publish\publish.ps1 -Service sdk-core -Version v1.0.1

# 快速构建：仅当 lock 文件哈希匹配时复用 node_modules
.\publish\publish.ps1 -Version v1.0.2 -Fast

# 已有同名归档，跳过编译；归档必须符合所选 Service
.\publish\publish.ps1 -Version v1.0.2 -SkipBuild

# 只更新文件，不重启进程
.\publish\publish.ps1 -Version v1.0.3 -SkipRestart
```

`-Service` 可选 `all`（默认）、`config-center`、`config-center-access`、`sdk-core`、`sdk-service`、`admin`。单独选服务时不构建 Vue；仅 `all` 模式默认构建 Vue，`admin` 仅构建 Vue。兼容 `-SkipFrontend`（不能与 `-Service admin` 同时使用）、`-Arch amd64|arm64` 和覆盖服务器参数 `-ServerHost`、`-Port`、`-User`、`-RemoteDirectory`。

`-SkipBuild` 必须显式指定 `-Version`，避免错误地上传历史文件。归档由本地包校验和服务器 SHA-256、架构、文件类型、服务选择器等多重检查拦截。构建失败不会连接服务器。

## 生产发布行为

本地完成构建和归档校验后上传到服务器临时目录，验证 SHA-256、文件类型、架构与选定服务，再直接更新固定的 `bin/`、`admin/`、`sql/`。生产 `config/`、`logs/` 和现有 `admin/config.js` 不会覆盖；SQL 仅分发，不会自动执行。

默认按 `publish.config.json` 中的 `services` 逐实例执行 Supervisor 重启与 `/health/ready` 验活；未列出的实例不会被重启。本发布流程**不提供自动回滚或零中断保证**，失败时需人工处置。完整的部署目录、Nginx、数据库升级与故障处理以 [生产运维](../docs/production.md) 为准。

## CI 和自测

仓库 `.github/workflows/package.yml` 在 Go/Vue/共享库/打包发布脚本修改后验证完整 Linux 归档，同时运行无实际 SSH 的发布冒烟测试：

```bash
python -I publish/ci_publish_smoke.py
```

在 CI 中优先复用各 Go 模块依赖缓存；`-Fast` 只供本地快速构建，正式发布建议省略以执行 `npm ci`。
