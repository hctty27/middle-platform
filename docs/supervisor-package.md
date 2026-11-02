# Supervisor Linux 打包说明

项目提供 `scripts/package-supervisor.ps1`，可在 Windows PowerShell 或 GitHub Actions 上生成 Linux 发布压缩包。**正式服务器不需要安装 Go、Node.js 或 Docker**。

## 一、本地打包

在项目根目录执行：

```powershell
cd E:\Works\ai\sdk\middle-platform
.\scripts\package-supervisor.ps1

# 固定发布版本
.\scripts\package-supervisor.ps1 -Version v1.0.0

# ARM64 Linux 服务器
.\scripts\package-supervisor.ps1 -Arch arm64 -Version v1.0.0

# 只构建 Go，跳过 Vue 前端
.\scripts\package-supervisor.ps1 -SkipFrontend
```

本地需要安装 Go 1.27.3、Node.js 22 与 npm。默认生成 linux/amd64 程序。脚本会编译三个 Go 服务、独立迁移工具并运行 Vue 构建，编译失败即停止。

输出文件在 `dist/supervisor/`，例如 `middle-platform-v1.0.0-linux-amd64.zip`：

```text
bin/
  config-center
  config-center-access
  sdk-core
  migrate
admin/
  index.html
  assets/...
BUILD_INFO.txt
SHA256SUMS.txt
```

- Core-1 和 Core-2 使用同一个 `bin/sdk-core`，不需要单独编译两次。
- 生成包**不包含**数据库密码、生产 YAML、Webhook 密钥、源码或 node_modules。
- `-SkipFrontend` 不构建 `admin/`。
- 默认版本号包括 Git 提交号与构建时间；`-Version` 可指定自定义版本。

## 二、GitHub Actions 一键打包

打开仓库 **Actions → Package Supervisor Linux → Run workflow**。

工作流会运行同一个 PowerShell 脚本，校验发布包目录及 SHA-256，然后上传名为 `middle-platform-supervisor-linux-amd64` 的 Artifact，保留 14 天。可以直接下载，正式服不需要编译器。

## 三、上传正式 Linux 服务器

在本地上传，例如：

```powershell
scp .\dist\supervisor\middle-platform-v1.0.0-linux-amd64.zip deploy@10.0.0.11:/tmp/
```

在服务器上解压：

```bash
sudo apt-get install -y unzip supervisor nginx
sudo mkdir -p /opt/middle-platform/releases/v1.0.0
sudo unzip -q /tmp/middle-platform-v1.0.0-linux-amd64.zip \
  -d /opt/middle-platform/releases/v1.0.0

sudo chmod 0755 /opt/middle-platform/releases/v1.0.0/bin/*
cd /opt/middle-platform/releases/v1.0.0
sha256sum -c SHA256SUMS.txt
```

ZIP 不能可靠保留 Windows → Linux 的文件可执行位，解压后要运行 `chmod +x`。

将生产配置放在 `/etc/middle-platform/config-center.yaml`、`sdk-core.yaml` 和可选的 `config-center-access.yaml`。Supervisor 启动命令的二进制路径应指向实际发布版本，服务地址用环境变量覆盖：

| 进程 | 监听环境变量 |
| --- | --- |
| config-center | `CONFIG_GO_ADDR="127.0.0.1:8201"` |
| sdk-core-1 | `SDK_CORE_ADDR="127.0.0.1:8211"` |
| sdk-core-2 | `SDK_CORE_ADDR="127.0.0.1:8212"` |
| config-center-access | `CONFIG_ACCESS_ADDR="127.0.0.1:8291"` |

管理台的 `admin/` 交由 Nginx 托管（内部端口 8281），不需要 Supervisor。原有 Docker 版静态 Nginx 配置的 `root /usr/share/nginx/html;` 要改成 Linux 服务器上的实际目录。

发布时先备份数据库并完成兼容性迁移；滚动发布前摘除目标 Nginx upstream，更新后验证 `/health/ready` 再恢复流量。不要直接把旧、新二进制覆盖在相同目录；保留版本目录以便回滚。共享 `current` 软链接不是严格的逐实例原子切换：应给需要分别滚动的服务设置独立版本路径或软链接，避免其他进程意外重启时加载新版本。

## 常见错误：error obtaining VCS status (exit status 128)

Go 编译默认尝试从 Git 仓库写入版本控制信息；Windows 工作树的 Git 状态不可读取时可能导致编译失败。
本项目打包脚本在 `go build` 中显式启用 `-buildvcs=false`，仅关闭二进制内部的自动 Git 元数据，不影响程序运行或模块依赖解析。
发布包仍会在 `BUILD_INFO.txt` 中记录版本号与可用的 Git commit；如果 Git 读取失败，则记录 `unknown`，打包继续。

更新代码后重新运行：

```powershell
git pull
.\scripts\package-supervisor.ps1
```

如果还想调查本机 Git 错误根因，可以在仓库根目录执行 `git status` 和 `git rev-parse --show-toplevel`；这不影响使用新脚本打包。
