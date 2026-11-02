# Go 配置中心接入端

接入端持有独立的 MySQL 副本，仅同步 game、region、server_group 三层节点与 epoch + revision 游标，不存储渠道应用配置。

## 启动

MySQL 8.0+ 中先创建空的 config_access 数据库：

```sql
CREATE DATABASE config_access CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
```

复制 `configs/config.yaml` 到 `configs/config-dev.yaml`，配置 database.dsn、配置中心地址和回调 HMAC 密钥：

```powershell
go run ./cmd/config-center-access -config configs/config-dev.yaml
```

首次启动仅在 **完全空库** 自动执行 `internal/store/sql/schema.sql` 创建两张表（`config_node`、`config_sync_state`）；已有副本只做结构校验，不执行历史 DDL、删除节点或重置同步游标。

**旧 V1～V5 副本**：必须使用基线整理前的历史脚本先迁到 V6，再部署当前版本；出现缺少列或旧 production CHECK 约束时会拒绝启动。不要直接在已有副本中执行 schema.sql。若可丢弃副本，请先确认数据只用于缓存，另建空库让接入端从权威库重新全量同步。

## 同步机制

- `POST /internal/config/callback`：校验时间窗口与 HMAC-SHA256 签名，接受后异步同步。
- `GET /api/access/nodes`、`GET /api/access/revision`：本地副本查询。
- `GET /health/live`、`GET /health/ready`：健康检查。
- 首次启动拉取完整一致性快照，之后根据 revision 增量拉取；epoch 变化或日志缺失时重建快照。
- 节点数据与同步游标通过同一事务提交；定时补偿检查遗漏的通知。

管理端创建订阅者时，配置的 Webhook 密钥需要和接入端 webhook.secret 一致；跨主机回调使用可信内网或 HTTPS。

## 验证

```bash
go test -race ./...
go vet ./...
go build ./cmd/config-center-access
```
