# Linux 发布包构建

唯一需要手动运行的打包入口为 `scripts/package.ps1`；Windows PowerShell 和 GitHub Actions 共用该脚本。脚本交叉编译四个 Linux ELF，并可同时构建 Vue 静态资源。**打包与进程管理方式无关**，不要求安装 Supervisor 才能打包。

## 构建依赖与命令

开发机要求 Go 1.27.3；包含 Vue 前端时还需 Node.js 22 / npm。仓库根目录执行：

```powershell
.\scripts\package.ps1 -Arch amd64 -Version v1.0.0

# 可选：Linux ARM64
.\scripts\package.ps1 -Arch arm64 -Version v1.0.0

# 可选：仅服务端
.\scripts\package.ps1 -SkipFrontend -Version v1.0.0
```

省略 `-Version` 时自动生成带 Git 提交号或本地时间的版本标识。Go 编译使用 `-buildvcs=false`，避免开发机 VCS 状态异常影响编译。

## 产物目录

成功后 `dist/` 直接生成**唯一一个** `middle-platform-v1.0.0-linux-amd64.tar.gz`，不保留中间解压目录或其他脚本生成的历史包。

```text
dist/
└── middle-platform-v1.0.0-linux-amd64.tar.gz
```

归档内只包含：

```text
bin/
├── config-center
├── config-center-access
├── sdk-core
└── migrate
admin/                      # -SkipFrontend 时不存在
├── index.html
├── config.js               # 可修改的公开 API 地址配置
└── assets/...
```

生产部署时，可将 `admin/config.js` 复制到 `/data/middle-platform/config/admin.js`，再通过 Nginx 精确匹配提供给前端，避免更新静态文件覆盖配置。详见 [生产部署与运维](production.md)。

归档**不包含**生产 YAML、真实密码、Supervisor/systemd/Nginx 配置、源码、`BUILD_INFO.txt` 或 `SHA256SUMS.txt`。`scripts/package-archive.go` 只是内部 tar.gz 辅助工具，保证从 Windows 打包时 Linux ELF 仍保存 `0755` 可执行权限；不需要也不应该单独运行。

发布前可检查（Linux）：

```bash
tar -tzf dist/middle-platform-v1.0.0-linux-amd64.tar.gz
```

## CI 打包

GitHub Actions → **Package Linux** → **Run workflow**。`.github/workflows/package.yml` 验证归档条目、ELF 架构及权限，并上传原始 tar.gz，不额外包一层 ZIP。CI 可以验证校验和，但**不会把校验文本写进交付包**。

## 发布边界

打包脚本不安装 Supervisor、不修改 Nginx、不部署数据库、不执行迁移，也不代替备份与回滚。生产更新必须先解压到临时目录、验证 ELF 与权限，再逐实例摘流量与更新；切勿覆盖正在执行的二进制。详见 [生产部署与运维](production.md)。
