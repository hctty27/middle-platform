# 研发中台生产部署（Go + Supervisor / systemd + Nginx）

本项目使用 **Go 编译产物直接运行**，管理后台由 Nginx 托管静态文件，数据库为独立的 MySQL 8.0+。
详细打包操作见 [Supervisor Linux 打包说明](supervisor-package.md)。生产服务器无需安装 Go、Node.js。

> 配置中心和管理页面没有完整的用户权限系统；管理 API、配置同步接口及 Webhook 回调只能在可信内网开放。单个 Nginx 和单个 MySQL 仍是单点，需要自行规划备份与高可用。

## 1. 服务与端口

| 服务 | 建议监听端口 | 访问范围 |
| --- | --- | --- |
| config-center | 8201（可按节点扩容） | 内网 |
| sdk-core-1 | 8211 | 网关转发 |
| sdk-core-2 | 8212 | 网关转发 |
| config-center-admin | 8281 | 仅管理网关或内网 |
| config-center-access（可选） | 8291 | 内网及签名回调 |

可在多台应用机上运行相同二进制，各实例通过监听环境变量使用不同端口。配置中心实例共用权威库；接入端使用独立副本库。Nginx 网关配置由运维环境独立维护，按实际部署设置 upstream 节点、TLS 证书、域名和管理白名单。

## 2. 构建和上传

开发机在仓库根目录执行：

```powershell
.\scripts\package-supervisor.ps1 -Arch amd64 -Version v1.0.0
```

也可以通过 GitHub Actions 的 **Package Supervisor Linux** 工作流生成归档。上传 `dist/supervisor/middle-platform-v1.0.0-linux-amd64.zip` 到应用机并校验 SHA256：

```bash
sudo mkdir -p /opt/middle-platform/releases/v1.0.0
sudo unzip -q /tmp/middle-platform-v1.0.0-linux-amd64.zip -d /opt/middle-platform/releases/v1.0.0
cd /opt/middle-platform/releases/v1.0.0
sha256sum -c SHA256SUMS.txt
sudo chmod 0755 bin/*
```

生产配置独立存放于 `/etc/middle-platform/`，不得打包真实数据库密码或 Webhook 密钥。

## 3. 安装并启动服务

服务管理配置不再随代码仓库及发布包提供，建议由生产机器上的 Supervisor 或 systemd 独立管理。为 Go 服务创建低权限系统账号 `middle-platform`，并确保程序有权读取对应 YAML。

以下示例以 `config-center` 为例（文件路径和服务账号需要根据实际环境调整）：

```bash
sudo install -d -m 0750 /etc/middle-platform
sudo tee /etc/systemd/system/config-center.service >/dev/null <<'EOF'
[Unit]
Description=Middle Platform Config Center
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=middle-platform
Group=middle-platform
Environment=CONFIG_GO_ADDR=:8201
ExecStart=/opt/middle-platform/releases/v1.0.0/bin/config-center -config /etc/middle-platform/config-center.yaml
Restart=on-failure
RestartSec=3
KillSignal=SIGTERM
TimeoutStopSec=40s

[Install]
WantedBy=multi-user.target
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now config-center
sudo systemctl status config-center
```

创建 unit 前先确保二进制已部署、账号存在，且 `/etc/middle-platform/config-center.yaml` 中已经填写有效 DSN 和其他生产配置。两个 SDK Core 实例使用同一个 `bin/sdk-core`，但必须分别创建服务、配置 `SDK_CORE_ADDR=:8211` 与 `:8212`，按数据库容量分配连接池。接入端仅在需要本地副本时部署。

> 停机宽限时间应大于应用自身的优雅退出窗口；不要在多个实例间共享相同的监听端口。完整启停原则参见 [优雅停机说明](graceful-shutdown.md)。

## 4. MySQL 迁移

创建 `config_center` 数据库，分配配置中心所需的最小权限账号；`sdk-core` 使用只读账号。旧库操作前先**备份、停写、确认迁移方案**。可先运行发布包的独立 `bin/migrate` 校验和迁移，再启动服务：

```bash
/opt/middle-platform/releases/v1.0.0/bin/migrate -config /etc/middle-platform/config-center.yaml
```

MySQL DDL 非事务性；失败后不得盲目清理迁移记录或直接重试。历史迁移见 [配置中心文档](../config-center/README.md)。

## 5. Nginx 静态管理页与网关

将发布包中的 `admin/` 文件同步到 `/opt/middle-platform/admin/`。自行配置静态站点（建议内网端口 `8281`），`root` 指向该目录，`/api/` 不应回退到 Vue SPA。生产网关与应用静态站点的 Nginx 配置不再由代码仓库托管。

```bash
sudo nginx -t
sudo systemctl reload nginx
curl -fsS http://127.0.0.1:8201/health/ready
curl -fsS http://127.0.0.1:8211/health/ready
```

外部网关将 `/api/core/**` 转发到 SDK Core，管理域名将 `/api/config/**` 转发到配置中心；管理入口需要 IP 白名单和认证。严禁公开 `/api/config/admin/**`、`/api/config/sync/**`、`/internal/config/callback`。

## 6. 发布、回滚与监控

发布单个节点前，先从 Nginx upstream 摘除该节点；确认已有请求排空后停止服务、备份当前二进制并部署新版本。依次检查 `/health/live`、`/health/ready`、日志与回调同步，再恢复路由，随后更新下一节点。

systemd 使用 `journalctl -u config-center` 等命令查看日志；Supervisor 部署可通过 `supervisorctl status` 查看进程状态。二进制可按版本回滚，但**数据库迁移不会随程序版本自动回滚**，必须提前验证新旧代码与数据库兼容性。
