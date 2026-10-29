# Go 配置中心

Go 1.27 + Chi + MySQL 8.4，兼容四级配置树、`/api/config` 和 `epoch + revision` 协议。MySQL 表仍由 Java 工程 Flyway V1–V4 迁移维护。

## 启动

```bash
cd config-center-go
export CONFIG_DB_DSN='user:password@tcp(127.0.0.1:3306)/config_center?charset=utf8mb4&parseTime=true&loc=UTC'
export CONFIG_GO_ADDR=':8081'
go run ./cmd/config-center
```

默认仅开放读取接口，修改配置及 Webhook 分发由以下环境变量显式开启：

- `CONFIG_GO_ENABLE_WRITES=true`：开启配置写入。
- `CONFIG_GO_ENABLE_WEBHOOK=true`：开启回调，需要同时开启写入。
- `WEBHOOK_ALLOWED_HOSTS=callback.example.com`：逗号分隔的精确回调主机白名单，默认仅允许 localhost 与 127.0.0.1。

Webhook 在实际连接前检查目标 DNS/IP，并连接已验证 IP 防止 DNS 重绑定；禁止重定向。域名回调只允许公网 IP，可信私网业务需使用明确列入白名单的固定 IP。

常用版本查询、版本锁、变更日志使用 sqlc 生成代码（已提交 `internal/sqlcgen`），动态查询仍使用参数化 SQL。修改 SQL 时运行 `sqlc generate`，CI 会检查生成结果。

测试命令：`go test ./...`、`go vet ./...`、`go build ./cmd/config-center`。CI 还涵盖 MySQL 8.4 集成、真实 Flyway 与 Java/Go 协议对照。原 Java 服务保留，生产部署不自动切换。
