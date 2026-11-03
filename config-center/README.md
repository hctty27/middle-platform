# config-center · 权威配置中心

本工程提供三级配置节点（`game → region → server_group`）、独立 `channel_app`、订阅者管理、增量同步与 Webhook 投递。权威 MySQL 数据库为 `config_center`；运行时不依赖接入端副本。

## 本地启动

1. 创建空 `config_center` 库，复制 `configs/config.yaml` 为 `configs/config-dev.yaml`。
2. 填写 `database.dsn`；管理读写接口与 Webhook Worker 均随服务启动。**管理 API 无内建鉴权，必须限制到可信内网或受认证的管理网关**。
3. 先通过数据库客户端执行 `db/sql/initialize.sql`（仅新空库）；已有权威库缺订单表时仅执行 `db/sql/add_game_order.sql`。然后在本目录运行：

```powershell
go run ./cmd/config-center -config configs/config-dev.yaml
```

启动后检查 `GET /health/ready`；Webhook 每 3 秒查询待投递订阅者，运行中新增无需重启。回调支持公网 IP、内网 IP、域名和 localhost 的 HTTP/HTTPS 地址，无需维护白名单；元数据及特殊网络地址仍被禁止。管理写路由始终注册，部署时必须先完成网关访问控制。

## 关键约束

- `config_node` 只有三级节点，不保存 `parent_id`；`region.env_type` 为 `test`、`review`、`prod`。
- `config_channel_app.code` 全局唯一；每个 AppID 各环境最多绑定一个区服组，共用一份 `config` JSON。
- 仅三级节点增删改会推进 `epoch/revision`；渠道应用变更不参与节点同步。
- SQL 由人工审核和执行；运行时只检查表、字段与必要索引，不根据旧 V11/V12 版本记录判断。
- 历史 `config_schema_migrations` 表不再使用，也不会更新；保留旧数据，切勿为修复问题直接删表。

## 常用配置

| YAML | 对应环境变量 | 说明 |
| --- | --- | --- |
| `database.dsn` | `CONFIG_DB_DSN` | 权威库连接 |
| `server.address` | `CONFIG_GO_ADDR` | HTTP 监听地址 |

## 测试与详细文档

```bash
go test -race ./...
go vet ./...
go build ./cmd/config-center
```

[接口文档](../docs/api.md) · [数据库与手动 SQL](../docs/database.md) · [本地开发](../docs/development.md) · [生产部署](../docs/production.md)

已移除废弃配置 `webhook.enabled`、`migration.allowLegacyUpgrade` 和环境变量 `CONFIG_GO_ENABLE_WEBHOOK`。升级旧配置时请删除对应 YAML 字段。

已移除 `admin.writesEnabled` 和 `CONFIG_GO_ENABLE_WRITES`。旧 YAML 的 `admin` 块需删掉；合并代码后管理写接口将始终存在。仅从防火墙/网关受控网络允许访问 `/api/config/`，不要公开包含游戏和渠道密钥的原始配置接口。
