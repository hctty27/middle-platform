# Go 配置中心

Go + MySQL 8.0+ 权威配置服务。配置树为 game → region → server_group；渠道应用由 config_channel_app 独立管理，code 全局唯一，每个应用最多绑定一个 prod、test、review 区服组。

## 数据库初始化和升级

- **新建空库**：独立执行迁移 CLI，一次性初始化 V11 基线。
- **已有 V11 库**：保留所有业务数据及 version=11、dirty=0 的迁移记录，CLI 幂等校验。
- **已有 V1～V10 库**：必须先用基线重构前的代码升级到 V11 并备份。新版本会拒绝旧库，不自动删除或覆盖数据。
- **已有表但缺少迁移记录的库**：拒绝认领为新库，必须人工核查。
- **未来升级**：在 `internal/migration/sql` 中新增 V12 等增量文件，并在 `migration.go` 维护 `Latest` 和 `upgrades`。
- **服务启动**：只执行结构检查，不做 DDL。集群仅在发布阶段执行一次迁移命令。

先创建 MySQL 数据库：

```sql
CREATE DATABASE config_center CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
```

复制 `configs/config.yaml` 为本地配置，填写 `database.dsn` 后，在 `config-center` 目录下执行：

```powershell
go run ./cmd/migrate -config configs/config-dev.yaml
go run ./cmd/config-center -config configs/config-dev.yaml
```

验证迁移状态：

```sql
SELECT version, dirty FROM config_schema_migrations WHERE id=1;
```

预期为 11 / 0。上线前备份所有权威库，数据库结构变更前停止旧版写入进程。MySQL DDL 不是完整事务：失败后 dirty=1，必须核对实际表结构并从备份恢复，禁止直接修改版本表。

## 配置文件

通用配置在 `configs/config.yaml`；开发和正式配置可复制成 `configs/config-dev.yaml` / `config-prod.yaml`。配置优先级：默认值、通用 YAML、指定 YAML、环境变量。

主要配置：
- `database.dsn` / `CONFIG_DB_DSN`：权威 MySQL 连接。
- `server.address` / `CONFIG_GO_ADDR`：监听地址。
- `admin.writesEnabled` / `CONFIG_GO_ENABLE_WRITES`：写入开关。
- `webhook.enabled` / `CONFIG_GO_ENABLE_WEBHOOK`：变更通知开关。
- `webhook.allowedHosts` / `WEBHOOK_ALLOWED_HOSTS`：回调白名单。

管理接口无账号权限系统，只应开放在可信内网或受保护网关中。

## 服务能力

- `/api/config/admin/**`：三级节点、渠道应用、订阅者管理。
- `/api/config/sync/**`：epoch + revision 增量同步和快照。
- `/health/live`、`/health/ready`：健康与就绪检查。
- Webhook：签名回调通知接入端自行拉取配置；只有三级节点变更推进 revision。

三个环境映射到 `config_channel_app` 的 `prod_server_group_id`、`test_server_group_id`、`review_server_group_id`，公共 JSON 不分环境。

## 检查

```bash
go test -race ./...
go vet ./...
go build ./cmd/config-center ./cmd/migrate
```

更多说明参见 ../docs/database-cleanup.md 和 ../docs/production-deployment.md。
