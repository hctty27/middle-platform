# Go 配置中心

Go 1.27、Chi、MySQL 8.4。兼容现有四级配置树、`/api/config` 接口和 `epoch + revision` 同步协议。

## 代码目录

- `cmd/config-center`：进程入口。
- `internal/app`：数据库、HTTP 服务及回调任务的初始化与优雅退出。
- `internal/settings`：读取、校验环境变量。
- `internal/api`：HTTP 路由、节点管理、配置同步与订阅者接口。
- `internal/model`：节点、同步版本、订阅者以及请求模型。
- `internal/service`：四级节点层级规则及业务字段校验。
- `internal/store`：MySQL 查询、事务、版本及变更记录。
- `internal/webhook`：多实例租约、回调签名、重试与地址安全校验。
- `internal/sqlc`：sqlc 的 SQL 源文件；`internal/sqlcgen`：自动生成代码，禁止手工修改。

## 启动

```bash
cd config-center
export CONFIG_DB_DSN='user:password@tcp(127.0.0.1:3306)/config_center?charset=utf8mb4&parseTime=true&loc=UTC'
export CONFIG_GO_ADDR=':8081'
go run ./cmd/config-center
```

默认仅提供只读接口。以下配置显式开启修改和投递：

- `CONFIG_GO_ENABLE_WRITES=true`：允许配置写入。
- `CONFIG_GO_ENABLE_WEBHOOK=true`：允许回调投递，要求同时开启写入。
- `WEBHOOK_ALLOWED_HOSTS=callback.example.com`：回调地址精确主机白名单，默认 `localhost,127.0.0.1`。

Webhook 校验目标 DNS/IP 并直接连接已验证 IP，防止 DNS 重绑定；不跟随重定向。域名回调必须解析到公网 IP，固定内网 IP 需要显式列入白名单。

## 数据库

MySQL 结构继续由 Java Flyway V1–V4 维护，不在 Go 进程中自动迁移。部分静态查询由 sqlc 生成；修改 SQL 后运行 `sqlc generate`。

## 测试

```bash
go mod tidy
go fmt ./...
go test -count=1 -race ./...
go vet ./...
go build ./cmd/config-center
```

GitHub Actions 还执行 MySQL 8.4 集成测试、真实 Flyway 迁移及 Java/Go 接口兼容性测试。
