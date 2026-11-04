# 本地发布工具

脚本可以提交。连接信息写在 `publish.config.json`，该文件已加入 `.gitignore`，不要提交。

原有的 `scripts/package.ps1` 和 `scripts/package-archive.go` 保持原位置与内容。发布入口通过本地 `package-local.ps1` 包装器执行原有打包逻辑，压缩包仍生成到仓库根目录 `dist/`。

包装器用 .NET 读取原文件，在系统临时目录生成 UTF-8 BOM 的 PowerShell 副本和原始字节的 Go 副本：前者兼容 Windows PowerShell 5.1 的中文脚本读取，后者规避当前机器上 Go 从原路径读取到 NUL 字节的异常。仅在临时 PowerShell 副本中固定仓库路径，不修改原文件；成功或失败都会清理本次临时副本。若原有脚本布局发生变化，包装器会明确报错，不会猜测路径。

## 使用

首次使用时，把 [publish.config.example.json](publish.config.example.json) 复制为同目录的 `publish.config.json` 再填写。没有这个文件，或少了 `host`、`port`、`user`、`remoteDirectory`，脚本会直接报错、不会连接。命令行参数优先于配置文件。

```json
{
  "host": "203.0.113.10",
  "port": 22,
  "user": "deploy",
  "remoteDirectory": "/opt/middle-platform",
  "password": ""
}
```

`203.0.113.10` 只是文档示例地址，不能拿来发布。`password` 留空时仍在 `ssh` / `scp` 交互输入，可能问多次。填上之后，这几次认证用配置里的密码，密码不出现在命令行上，打包过程也不会把它带给 `go` 或 `npm`。此时只走密码认证，不再尝试本机私钥。密码是明文，只留在本机的 `publish.config.json`，该文件不会进 Git。

在仓库根目录执行：

```powershell
.\publish\publish.ps1
```

可选：

```powershell
.\publish\publish.ps1 -SkipFrontend
.\publish\publish.ps1 -Version v1.0.0
.\publish\publish.ps1 -ServerHost 10.0.0.8 -Port 22
```

`-ServerHost`、`-Port`、`-User`、`-RemoteDirectory` 覆盖配置文件，`-Arch amd64|arm64` 指定架构。

需要本机具备原有打包依赖、.NET Framework 的 `csc.exe`（仅在填写了密码时用来生成密码助手）和 OpenSSH 的 `ssh`、`scp`；服务器需提供 Bash、GNU tar/coreutils。首次连接时应先手动 `ssh` 核对主机指纹。密码助手只回答密码提问，不会自动接受新的主机指纹。

## 行为与边界

- 打包成功后才上传，上传后校验 SHA-256、架构、归档路径与文件类型。
- 在服务器独立暂存目录解压，再逐文件更新 `bin/`、`sql/` 和可选的 `admin/`。
- 保留已有 `admin/config.js`，不修改 `config/` 和 `logs/`，不清空目标目录。
- 以同目录临时文件加重命名替换程序，不直接覆盖运行中的二进制。
- 成功后归档保留在 `packages/`，本次暂存目录删除；失败留下的 `.publish-*` 目录需自行清理。
- 不停止或重启服务，不修改 Nginx/Supervisor，不执行 SQL，不备份或回滚。运行中的后端需要手动重启后才使用新程序。
- 文件逐个替换，不是整个版本的原子切换；失败可能已更新部分文件，前端更新期间可能短暂混用新旧资源。

## 只验证打包，不上传

在仓库根目录执行本地包装器（仍使用原有打包依赖）：

```powershell
.\publish\package-local.ps1 -Version local-check
```

与原有打包脚本一致，成功后只保留本次构建的归档，会清理 `dist/` 下本脚本命名规则内的旧归档。

## 本地测试

不构建真实发布包、不连接服务器：

```bash
python -I publish/ci_publish_smoke.py
```
