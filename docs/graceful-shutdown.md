# Go 服务优雅停机与启停管理

## 停机流程

三个 Go 常驻进程（`config-center`、`config-center-access`、`sdk-core`）均响应 Ctrl+C / SIGINT 和 SIGTERM。

1. 收到信号后将就绪状态置为 DOWN：`GET /health/ready` 返回 HTTP 503；`GET /health/live` 保持存活探针语义。
2. 使用 `http.Server.Shutdown` 停止监听新连接，等待已有 HTTP 请求结束，上限 30 秒；超时调用 `Server.Close` 并返回错误。
3. `config-center` 取消 Webhook Dispatcher 后等待其退出；`config-center-access` 取消同步 Worker 后等待其退出。正常情况下，Worker 结束后才释放数据库连接池。
4. `sdk-core` 暂无后台 Worker，完成 HTTP 排空后关闭数据库连接池。
5. 记录开始停机、停止耗时及异常。

超过 30 秒时属于**异常强制关闭路径**，后台任务若仍阻塞，进程会返回错误；未提交的同步、Webhook 任务需要依靠已有的游标、事务和租约机制在重启后继续处理。MySQL DDL 不保证中断后回滚，数据库迁移必须备份、检查并在发布时单独管理。

## Windows 本地

分别在各工程目录启动，按 Ctrl+C 即可停止：

` ``powershell
go run ./cmd/config-center -config configs/config-dev.yaml
go run ./cmd/config-center-access -config configs/config-dev.yaml
go run ./cmd/sdk-core -config configs/config-dev.yaml
```

这三条命令需要分别在对应工程根目录执行。`go run` 由包装进程启动可执行程序；如需验证生产进程信号行为，建议先 `go build` 再直接运行生成的二进制。

不要使用 `taskkill /F` 或 `Stop-Process -Force` 代替优雅停机。

## Linux 生产（systemd）

仓库已提供 `deploy/systemd/` 下的三个 unit 示例。请提前创建独立运行账号 `middle-platform`，并准备：

- 可执行程序：`/opt/middle-platform/<service>/<service>`
- 私有 YAML 配置：`/etc/middle-platform/<service>.yaml`
- 账号对目录和配置文件的读取权限，以及对应数据库所需的最小权限。

以 SDK Core 为例，在仓库根目录执行：

```bash
sudo cp deploy/systemd/sdk-core.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now sdk-core
sudo systemctl status sdk-core
sudo systemctl stop sdk-core
sudo systemctl start sdk-core
sudo journalctl -u sdk-core -f
```

其余两个服务将 `sdk-core` 替换为 `config-center` 或 `config-center-access`。示例使用 `Restart=on-failure`；管理员手动停止不会触发自动重启。`TimeoutStopSec=40s` 比代码停机预算多 10 秒，用于进程清理与输出日志。

## Nginx 多实例滚动更新

1. 在负载均衡层先摘除目标实例，确认没有新流量进入。
2. 检查其他实例仍可承担请求。
3. 执行 `systemctl stop <service>`，部署新版本，再执行 `systemctl start <service>`。
4. 确认 `GET /health/ready` 返回 200，再把实例加入负载均衡，依次更新下一台。

**注意：** 静态 Nginx upstream 不会自动读取服务的就绪探针；需要明确的摘流量发布步骤，或采用支持主动健康检查的网关。`http.Server.Shutdown` 不会自动关闭 WebSocket / hijacked 连接；若以后 SDK 服务增加此类连接，需要额外实现连接排空和退出等待。

## 验证建议

在三个工程目录分别执行：

```bash
go test ./...
go vet ./...
go build ./cmd/...
```

再使用本地二进制发出 SIGINT/SIGTERM，确认日志出现 `shutting down` 和 `stopped`，并验证数据库连接在 Worker 退出前没有被主动释放。
