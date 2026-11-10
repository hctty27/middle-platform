# config-center-access · 配置接入端

本工程是可选的 Go 接入服务，使用**独立**的 `config_access` MySQL 副本，仅缓存 `game`、`region`、`server_group` 三级节点和 `epoch + revision` 游标；不存储渠道应用配置。

## 本地启动

1. 创建完全空的 `config_access` 数据库。
2. 将 `configs/config.yaml` 复制为完整独立的 `configs/config-dev.yaml`；填写 `database.dsn`（副本库）、`configCenter.url`（权威配置中心地址）、`webhook.secret`（Webhook 接收密钥）、`configSync.secret`（主动拉取配置的 HMAC-SHA256 密钥）。启动只读取指定 YAML，不继承同目录的公共配置；不传 `-config` 时默认读取当前工作目录下的 `configs/config.yaml`。环境变量仍可覆盖指定字段：`CONFIG_SYNC_SECRET` 覆盖配置同步密钥，`CONFIG_WEBHOOK_SECRET` 覆盖 Webhook 密钥。
3. 在本目录执行：

```powershell
go run ./cmd/config-center-access -config configs/config-dev.yaml
```

首次使用**空库**时请先手工执行 `../db/full/config_access.sql` 创建 `config_node`、`config_sync_state`；接入端启动只校验，再全量拉取快照。已有副本只做结构校验；若表结构不兼容，先备份并核对后制定升级方案，不能在非空副本上执行初始化 SQL。

## 同步协议

- `POST /internal/config/callback` 使用订阅者密钥执行 HMAC-SHA256 验签，签名覆盖 HTTP 方法、实际路径、规范化 Query、时间戳、nonce 和原始 Body，成功返回 HTTP 202。接入端在单进程内拒绝重复 nonce。
- 主动拉取配置中心的 revision、changes、snapshot、节点详情和 batch-get 请求均使用 `configSync.secret` 执行 HMAC-SHA256 签名；该密钥必须与配置中心 `configSync.secret` 完全一致，至少 32 字节。
- `GET /api/access/nodes` 与 `GET /api/access/revision` 查询本地副本，也必须使用 `configSync.secret` 执行 HMAC-SHA256 签名。
- 首次启动、epoch 变化或 revision 断档时执行完整快照；常规更新按日志增量获取节点 ID 并批量拉取。
- 节点和游标在同一事务内写入；周期性 revision 校对补偿丢失通知。
- 订阅者在配置中心注册，`secret` 必须和本进程一致；内部回调不可直接开放公网。
- 旧版 SHA-1 排序签名 Webhook 不再兼容；配置中心和接入端必须同步升级。多实例场景应通过共享存储实现全局 nonce 去重。

```bash
go test -race ./...
go vet ./...
go build ./cmd/config-center-access
```

[统一接口与签名规范](../admin/public/docs/index.html) · [同步架构](../admin/public/docs/guide.html#architecture) · [数据库结构](../admin/public/docs/guide.html#database) · [联调](../admin/public/docs/guide.html#development)

第三方同步接入协议见 [配置同步接入文档](../admin/public/docs/index.html#s-intro)。
