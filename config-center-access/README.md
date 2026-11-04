# config-center-access · 配置接入端

本工程是可选的 Go 接入服务，使用**独立**的 `config_access` MySQL 副本，仅缓存 `game`、`region`、`server_group` 三级节点和 `epoch + revision` 游标；不存储渠道应用配置。

## 本地启动

1. 创建完全空的 `config_access` 数据库。
2. 复制 `configs/config.yaml` 为 `configs/config-dev.yaml`；填写 `database.dsn`（副本库）、`configCenter.url`（权威配置中心地址）、`webhook.secret`（签名密钥）。
3. 在本目录执行：

```powershell
go run ./cmd/config-center-access -config configs/config-dev.yaml
```

首次使用**空库**时请先手工执行 `db/sql/initialize.sql` 创建 `config_node`、`config_sync_state`；接入端启动只校验，再全量拉取快照。已有副本只做结构校验；若表结构不兼容，先备份并核对后制定升级方案，不能在非空副本上执行初始化 SQL。

## 同步协议

- `POST /internal/config/callback` 使用 `X-Config-Timestamp`、`X-Config-Signature` 验证 HMAC-SHA256 签名，有效回调返回 HTTP 202，后台异步触发同步。
- `GET /api/access/nodes` 与 `GET /api/access/revision` 查询本地副本。
- 首次启动、epoch 变化或 revision 断档时执行完整快照；常规更新按日志增量获取节点 ID 并批量拉取。
- 节点和游标在同一事务内写入；周期性 revision 校对补偿丢失通知。
- 订阅者在配置中心注册，`secret` 必须和本进程一致；内部回调不可直接开放公网。

```bash
go test -race ./...
go vet ./...
go build ./cmd/config-center-access
```

[API 文档](../docs/api.md) · [同步架构](../docs/architecture.md) · [数据库结构](../docs/database.md) · [联调](../docs/development.md)

第三方同步接入协议见 [配置同步接入文档](../docs/config-center-access-sync.md)。
