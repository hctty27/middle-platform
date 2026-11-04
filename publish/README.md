# 本地打包与发布

在 Windows/PowerShell 上构建 Linux ELF 和 Vue 静态文件，经 SSH/SCP 上传到现有服务器目录并直接更新。**不使用 releases 版本目录，不提供自动或手动回滚功能**；数据库 SQL 永远由管理员手动执行。

## 首次配置

复制 `publish.config.example.json` 为本地 `publish.config.json`，填写 `host`、`port`、`user`、`remoteDirectory`。该文件已加入 `.gitignore`，不要提交账号或密码。

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

1. 本地构建并输出各阶段耗时，生成 `dist/middle-platform-<版本>-linux-<架构>.tar.gz`；成功后清理匹配命名规则的其他旧本地归档。
2. 上传到服务器 `.publish-* ` 临时目录，校验 SHA-256、文件类型、架构和全部必需文件。
3. 在**已有的** `bin/`、`admin/`、`sql/` 内逐文件以同目录临时文件和 rename 更新，不使用 `releases/`。
4. `config/`、`logs/` 不更新；已有 `admin/config.js` 始终保留。数据库 SQL 只传输、不执行。
5. 默认按服务重启对应 Supervisor 程序，轮询 `/health/ready`；`sdk-core-1`、`sdk-core-2` 依次重启。使用 `-SkipRestart` 可只更新文件。
6. 服务健康检查成功后，归档保存到服务器 `packages/`，临时目录清理；失败时立即报错，保留暂存目录供人工排查。

预设 Supervisor 名称：`config-center`、`config-center-access`（全量发布时可选）、`sdk-core-1`、`sdk-core-2`、`sdk-service`。健康检查地址为本机 `127.0.0.1` 的 `8201`、`8291`、`8211`、`8212`、`8221` 对应的 `/health/ready`。**如果线上实例命名或端口不同，必须先修改 `publish/publish-remote.sh` 对应映射，或者使用 `-SkipRestart` 后手动维护。** 发布账号需要执行 `supervisorctl restart` 的权限。

**不提供回滚：** 多文件逐一替换不具备全量事务性。替换中途失败可能留下部分新文件；进程验活失败时也不会恢复旧版本。发布前请确保服务与数据库结构兼容；需要手工恢复时自行处理。普通 Nginx 静态 upstream 不会因健康接口不可用就主动摘流量，因此顺序重启两个 Core 实例也**不保证零中断**。

## CI 和自测

仓库 `.github/workflows/package.yml` 在 Go/Vue/共享库/打包发布脚本修改后验证完整 Linux 归档，同时运行无实际 SSH 的发布冒烟测试：

```bash
python -I publish/ci_publish_smoke.py
```

在 CI 中优先复用各 Go 模块依赖缓存；`-Fast` 只供本地快速构建，正式发布建议省略以执行 `npm ci`。
