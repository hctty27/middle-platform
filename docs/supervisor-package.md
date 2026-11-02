# Supervisor Linux 打包与部署

项目通过 `scripts/package-supervisor.ps1` 编译三个 Go 服务、迁移程序和 Vue 静态资源。生产服务器直接运行 Go ELF，不需要安装 Go、Node.js 或 Docker。

## 一、打包格式

| 运行环境 | 发布文件 | Linux 可执行权限 |
| --- | --- | --- |
| Windows PowerShell | `.zip` | 解压后执行 `chmod 755 bin/*` |
| Linux / GitHub Actions | `.tar.gz` | 已记录 `0755` 执行位，正常解压无需再 `chmod` |

两种格式都包含同一套发布内容：

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

- `sdk-core-1` 和 `sdk-core-2` 共用 `bin/sdk-core`。
- 真实数据库 DSN、生产 YAML、Webhook 密钥、Supervisor/Nginx 配置不进入发布包。
- `-SkipFrontend` 只输出后端和迁移程序，不包含 `admin/`。
- `-Arch amd64` 为默认值；ARM64 Linux 使用 `-Arch arm64`。
- 版本号只用于归档文件名和构建信息，**生产服务器不使用 releases/ 目录**。

## 二、Windows 本地打包

本地需安装 Go 1.27.3、Node.js 22 和 npm。在项目根目录运行：

```powershell
cd E:\Works\ai\sdk\middle-platform
.\scripts\package-supervisor.ps1 -Arch amd64 -Version v1.0.0
```

生成 `dist/supervisor/middle-platform-v1.0.0-linux-amd64.zip`。其中的 `bin/*` 已是 Linux ELF 程序，并非 Windows EXE；但 ZIP 从 Windows 解压到 Linux 后应补一次执行权限：

```bash
sudo mkdir -p /data/middle-platform
sudo unzip -q /tmp/middle-platform-v1.0.0-linux-amd64.zip -d /data/middle-platform
sudo chmod 755 /data/middle-platform/bin/*
```

## 三、GitHub Actions 生成可直接执行的 tar.gz（推荐）

在 GitHub 仓库打开 **Actions → Package Supervisor Linux → Run workflow**。工作流在 Ubuntu 上运行同一个 PowerShell 脚本，步骤包括：

1. 交叉编译 Linux/amd64 Go ELF，构建管理台。
2. 对四个 `bin/*` 设置 `0755`，打包为 `middle-platform-<commit>-linux-amd64.tar.gz`。
3. 将 tar.gz 解压到临时目录，验证全部文件的 SHA-256、ELF x86-64 格式和 `0755` 执行权限。
4. 上传名为 `middle-platform-supervisor-linux-amd64` 的 Artifact，保留 14 天。

**GitHub Actions 下载的 Artifact 可能是外层 ZIP，解开后再得到内层 `.tar.gz`。** 上传下载过程不会破坏内层 tar 记录的执行权限。有关权限保留见 [GitHub upload-artifact 文档](https://github.com/actions/upload-artifact#limitations)。

## 四、首次部署到 /data/middle-platform

将 Actions 下载并解出的 `.tar.gz` 上传到服务器 `/tmp/`，以文件名 `middle-platform-<commit>-linux-amd64.tar.gz` 为例：

```bash
sudo mkdir -p /data/middle-platform/{bin,config,admin,logs,supervisor,nginx}
sudo mkdir -p /tmp/middle-platform-stage
sudo tar --no-same-owner -xzpf /tmp/middle-platform-<commit>-linux-amd64.tar.gz -C /tmp/middle-platform-stage
cd /tmp/middle-platform-stage
sha256sum -c SHA256SUMS.txt
ls -l bin/
```

检查通过后，首次部署（尚未运行任何服务）可将文件放入固定目录：

```bash
sudo cp -a /tmp/middle-platform-stage/bin/. /data/middle-platform/bin/
sudo cp -a /tmp/middle-platform-stage/admin/. /data/middle-platform/admin/
```

生产配置需另外创建：

```text
/data/middle-platform/config/config-center.yaml
/data/middle-platform/config/config-center-access.yaml
/data/middle-platform/config/sdk-core.yaml
```

Supervisor `command` 使用固定路径：`/data/middle-platform/bin/config-center`、`/data/middle-platform/bin/config-center-access`、`/data/middle-platform/bin/sdk-core`。Nginx 负责 `admin/` 静态资源，不纳入 Supervisor。

**首次新建空权威库必须先执行独立迁移程序**：

```bash
/data/middle-platform/bin/migrate -config /data/middle-platform/config/config-center.yaml
sudo supervisorctl reread
sudo supervisorctl update
sudo supervisorctl status
```

迁移只允许在适配版本的数据库上操作；旧库先备份并确认迁移路径。不要删除旧业务表或手工修改迁移版本记录。

## 五、正式环境更新与回滚

固定二进制路径虽然简洁，但**不要对正在运行的二进制直接 `cp` 覆盖**。建议将新包解压到临时目录，完成校验后备份原文件，在同一文件系统下使用 `mv` 原子切换程序文件，并用 `supervisorctl restart <program>` 逐个重启。两个 SDK 实例共用一个二进制：需要 Nginx 摘流量、逐实例检查 `/health/ready`，不要同时重启。

例如仅更新 SDK Core，假设已准备好经校验的新版本 `/tmp/middle-platform-stage/bin/sdk-core`：

```bash
sudo cp -a /data/middle-platform/bin/sdk-core /data/middle-platform/bin/sdk-core.bak
sudo install -m 755 /tmp/middle-platform-stage/bin/sdk-core /data/middle-platform/bin/sdk-core.new
sudo mv -f /data/middle-platform/bin/sdk-core.new /data/middle-platform/bin/sdk-core
sudo supervisorctl restart sdk-core-1
curl -fsS http://127.0.0.1:8211/health/ready
sudo supervisorctl restart sdk-core-2
curl -fsS http://127.0.0.1:8212/health/ready
```

操作前仍应在 Nginx 分别摘除实例流量，确认剩余容量充足。二进制可恢复 `.bak` 后重启；**数据库迁移不能简单通过程序回滚**。

## 六、常见问题

- `command is not executable`：Windows ZIP 解压后忘记 `chmod`；优先使用 Actions 的 Linux tar.gz，并检查文件系统是否启用 `noexec`。
- `error obtaining VCS status: exit status 128`：脚本已经通过 `go build -buildvcs=false` 规避。
- `database migration state missing`：确认 DSN 指向正确数据库，并先执行 `bin/migrate`；非空历史库不能当作新库初始化。
