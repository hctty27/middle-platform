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


## 第二阶段：写入与集群 Webhook（默认关闭）

Go 写接口和 Webhook 分发实现位于 `internal/config/write.go` 与 `delivery.go`。

- `CONFIG_GO_ENABLE_WRITES=false`：默认关闭管理写接口；启用后开放节点创建、更新、删除及新增订阅者。
- `CONFIG_GO_ENABLE_WEBHOOK=false`：默认关闭投递执行器；启用需同时启用写接口。
- `WEBHOOK_ALLOWED_HOSTS=localhost,127.0.0.1`：配置订阅者回调的允许主机；生产配置明确的 HTTPS 主机。
- 全局版本行先 `SELECT ... FOR UPDATE`，节点写入、revision 自增、change_log 插入同一事务；相同 JSON 内容更新不产生新版本。
- Webhook 使用 MySQL `FOR UPDATE OF d SKIP LOCKED` 抢占投递行、30 秒租约、HMAC SHA-256、失败指数退避。每轮至多 16 个任务，最多 4 个并发请求。
- Java 版本仍保持现有协议，Go 的写入/Webhook **不能与 Java 写实例同时启用投递**；需要完成真实 MySQL 并发和兼容回归后再切换。

默认阶段一的只读启动方法仍可使用。写入功能适合在专用测试库验证，**勿直接对生产库开启**。

## 第三阶段：真实 Flyway 兼容性验证与性能基线

CI `compatibility` 作业启动 MySQL 8.4，使用现有 Java Spring Boot + Flyway 执行真实 V1→V4 迁移，然后对同一个迁移后的数据库同时启动 Java 8080 和 Go 8081。

- `scripts/ci_go_compat.py` 对照读取接口、历史节点、搜索、快照和增量日志；交替用 Java / Go 写入后验证双端响应、幂等更新、删除与 revision。
- `scripts/ci_go_benchmark.py` 在同一 Runner 上分别测试 Java 和 Go 的典型只读接口，输出 QPS、P50、P95 和 P99，并上传 CI 构建产物。仅作趋势参考，不作为吞吐优势证明，也不设任意性能门槛。
- `sqlc.yaml` + `internal/sqlc/queries.sql` 开始以 sqlc v1.31.1 为新查询维护生成源。运行 `sqlc generate` 可生成 `internal/sqlcgen/`；CI 校验生成和编译。现有 Store SQL 尚未切换到生成层，生成层当前是过渡步骤。
- `internal/sqlc/schema.sql` 只是 sqlc 分析专用的 V4 字段快照，不能用于部署或替代 Flyway 迁移。

下一步需要在稳定生成的基础上逐个替换手写查询，并进一步验证 Webhook 目标地址 DNS 重绑定防护、生产时区和多实例压测。
