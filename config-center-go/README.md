# Go 配置中心（阶段一：只读并行实现）

这是独立的 Go 配置中心读取实现，不替换 Java 生产服务。继续使用现有 MySQL V4 Schema 和 /api/config 无版本接口，不创建、不迁移、不修改任何数据库表。

## 技术

- Go 1.27.3、Chi v5.5.0、database/sql、go-sql-driver/mysql 1.10.1。
- 首阶段原生参数化 SQL；后续在完成 MySQL V4 Schema 的 sqlc 类型与 JSON 一致性验证后迁移到 sqlc，避免贸然生成不兼容查询。
- 独立进程、无状态、只读数据库账号部署。默认端口 8081，避免与原 Java 服务 8080 冲突。

## 启动

```bash
cd config-center-go
go mod tidy
export CONFIG_DB_DSN='readonly_user:password@tcp(127.0.0.1:3306)/config_center?charset=utf8mb4&parseTime=true&loc=UTC'
export CONFIG_GO_ADDR=':8081'
go run ./cmd/config-center
```

建议使用具备 SELECT 权限的 MySQL 独立账号。启动时检查数据库连通和 config_revision 表是否存在；需要现有 Flyway V4 迁移完成的 schema。运行 `go test ./...` 执行协议单测。

## 已实现

- `GET /api/config/admin/nodes`、`/tree`、`/children`、`/{id}/children`、`/search`、`/{id}/path`、`/{id}`。
- `GET /api/config/nodes/{id}`、`POST /api/config/nodes/batch-get`。
- `GET /api/config/sync/revision`、`/changes`、`/snapshot`。
- `GET /api/config/admin/subscribers`（不返回 Webhook Secret）。
- `GET /health/live` 和 `GET /health/ready`。

所有写操作目前返回 405。节点 JSON 仍是 **字符串类型**，与 Java 当前响应一致；时间按无时区 LocalDateTime 字符串序列化。快照使用 MySQL REPEATABLE READ 只读事务。

## 下一阶段（切流阻塞项）

1. 补齐写 API、全局 revision 行锁、节点与 change_log 同事务、幂等更新。
2. 实现持久化 Webhook lease 抢占、批量投递、HMAC SHA-256 签名、失败重试和回调安全验证。
3. MySQL 集成测试、和 Java 返回 JSON 的对照测试、并发/事务/断线恢复测试。
4. 生成并使用 sqlc 类型安全查询，跑压测和 P95/P99 比对，再考虑逐步迁移真实流量。

**警告**：不要把本阶段的 Go 服务部署为 Java 配置中心的直接替代品。管理端写接口尚未实现，且未进行与现有 Java 实现的 MySQL 集成对照测试。保持 Nginx 正式路由不变。
