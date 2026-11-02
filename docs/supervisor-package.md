# Supervisor Linux 打包和部署

## 产物约定

Windows PowerShell 与 GitHub Actions 均使用 `scripts/package-supervisor.ps1`，在仓库根目录的 **`dist/` 直接生成一个 `.tar.gz`**，不生成 `dist/supervisor/`、解压目录、ZIP 或中间文件。

```text
dist/
└── middle-platform-v1.0.0-linux-amd64.tar.gz
```

tar.gz 内只包含运行需要的文件（`-SkipFrontend` 时不含 `admin/`）：

```text
bin/
├── config-center
├── config-center-access
├── sdk-core
└── migrate
admin/
├── index.html
└── assets/...
```

不包含 `BUILD_INFO.txt`、`SHA256SUMS.txt`、生产 YAML、数据库密码、Supervisor/Nginx 配置或源码；临时构建目录会自动清理。打包仍可在 CI 中校验归档、ELF 架构和权限，但不会把校验信息塞进交付包。

所有 `bin/` 可执行文件由 Go 的 tar 归档工具显式设置为 **Unix 0755**（即使在 Windows 上生成也是如此），Linux 解压后无需再 `chmod`。归档采用 Go 标准库 `archive/tar`，不依赖额外的 tar 命令或 WSL。

## Windows 本地打包

要求 Go 1.27.3；包含前端时还需要 Node.js 22 和 npm。

在仓库根目录执行：

```powershell
cd E:\Works\ai\sdk\middle-platform
git pull origin main
.\scripts\package-supervisor.ps1 -Arch amd64 -Version v1.0.0
```

生成 `dist/middle-platform-v1.0.0-linux-amd64.tar.gz`。

其他选项：

```powershell
# Linux ARM64
.\scripts\package-supervisor.ps1 -Arch arm64 -Version v1.0.0

# 不包含 Vue 管理端
.\scripts\package-supervisor.ps1 -SkipFrontend -Version v1.0.0
```

打包成功后清理脚本创建的旧版归档及历史 `dist/supervisor/` 目录，但不会删除 `dist` 下用户自己放入的其他文件。每次重新打包无需手工清理临时目录。

## GitHub Actions

打开 **Actions → Package Supervisor Linux → Run workflow**。Ubuntu Runner 调用同一份 PowerShell 脚本、验证 `dist/*.tar.gz` 中只含 `bin/` 和 `admin/`、四个 ELF 的架构及可执行位，再使用 `actions/upload-artifact@v7` 的 `archive: false` 上传 **原始 tar.gz**，不额外套 ZIP。Artifact 保留 14 天。

## Linux 服务器首次部署

将归档上传服务器 `/tmp/` 后，创建生产根目录：

```bash
sudo mkdir -p /data/middle-platform/{bin,config,admin,supervisor,nginx,logs}
sudo tar --no-same-owner -xzpf /tmp/middle-platform-v1.0.0-linux-amd64.tar.gz -C /data/middle-platform

ls -l /data/middle-platform/bin/
file /data/middle-platform/bin/*
```

`bin/` 下四个程序应为 Linux ELF，权限为 `-rwxr-xr-x`。配置文件不随包发布，应单独放在：

```text
/data/middle-platform/config/config-center.yaml
/data/middle-platform/config/config-center-access.yaml
/data/middle-platform/config/sdk-core.yaml
```

首次启动前，仅在**确认空库或已按要求完成历史迁移**后执行独立迁移工具：

```bash
/data/middle-platform/bin/migrate -config /data/middle-platform/config/config-center.yaml
sudo supervisorctl reread
sudo supervisorctl update
sudo supervisorctl status
```

旧库先备份，不能删除迁移状态表后强制初始化。

## 正式环境更新

不要在 Go 服务运行时直接用 `tar -xzf ... -C /data/middle-platform` 覆盖二进制文件。应先解压到临时目录，检查权限和程序架构，再备份旧程序、同文件系统原子替换、经 Nginx 摘流量后逐实例重启 Supervisor。两个 SDK 进程共享同一个 `sdk-core` 可执行文件，但应分别健康检查与恢复流量。

程序回滚不等于数据库结构回滚；数据库迁移需先确认版本兼容性。

## 参考

- [Go archive/tar](https://pkg.go.dev/archive/tar)
- [GitHub Actions 单文件不压缩上传](https://github.blog/changelog/2026-02-26-github-actions-now-supports-uploading-and-downloading-non-zipped-artifacts/)
