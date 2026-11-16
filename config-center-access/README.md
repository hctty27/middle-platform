# config-center-access · 配置接入端

本工程是可选的 Go 接入服务，使用**独立**的 `config_access` MySQL 副本，仅缓存 `game`、`region`、`server_group` 三级节点和 `epoch + revision` 游标；不存储渠道应用配置。

## 本地启动

1. 创建完全空的 `config_access` 数据库。
2. 将 `configs/config.yaml` 复制为完整独立的 `configs/config-dev.yaml`；填写 `database.dsn`（副本库）、`configCenter.url`（权威配置中心地址）、`subscriber.code` 和 `subscriber.secret`。其中编码和密钥必须与配置中心 `config_subscriber` 中同一条记录一致。编码最多 128 个字符；密钥必须唯一、长度为 32–255 字节且不得包含首尾空格。启动只读取指定 YAML，不继承同目录的公共配置；不传 `-config` 时默认读取当前工作目录下的 `configs/config.yaml`。环境变量 `CONFIG_SUBSCRIBER_CODE`、`CONFIG_SUBSCRIBER_SECRET` 可分别覆盖接入端编码和密钥。
3. 在本目录执行：

```powershell
go run ./cmd/config-center-access -config configs/config-dev.yaml
```

首次使用**空库**时请先手工执行 `../db/full/config_access.sql` 创建 `config_node`、`config_sync_state`；接入端启动只校验，再全量拉取快照。已有副本只做结构校验，不能在非空副本上执行初始化 SQL。已有副本升级到带游戏运营地区的版本前，先备份，再手工执行一次 `../db/incremental/V0016__add_config_access_game_market_type.sql`；游戏节点会先回填 `domestic`，随后由权威快照覆盖为 `domestic` 或 `overseas`。

## 同步协议

- 配置中心为每个 `config_subscriber.code` 保存一条唯一且独立的 `signing_secret`。本接入端配置的 `subscriber.code`、`subscriber.secret` 必须与该记录匹配。
- 接入端主动拉取 revision、changes、snapshot、节点详情和 batch-get 时，发送 `X-Subscriber-Code`，并使用该接入端自己的密钥执行 HMAC-SHA256 签名。
- `GET /api/access/nodes` 与 `GET /api/access/revision` 本地查询接口使用相同的接入端身份和密钥签名。
- 配置中心发送 Webhook 时使用该订阅记录的同一密钥；接入端回调校验接入端编码、方法、路径、规范化 Query、时间戳、nonce 和原始 Body。
- 首次启动、epoch 变化或 revision 断档时执行完整快照；常规更新按日志增量获取节点 ID 并批量拉取。
- 节点和游标在同一事务内写入；周期性 revision 校对补偿丢失通知。
- 停用订阅者会拒绝该接入端向配置中心发起的同步读取请求，并停止新的 Webhook 投递；接入端本地只读接口仍按本地配置密钥验签。
- 轮换密钥时，必须同步更新配置中心订阅记录和本接入端 `subscriber.secret`，再重启接入端，不能只改其中一侧。
- 旧版全局 `configSync.secret`、独立 `webhook.secret` 及未绑定接入端身份的签名协议不再兼容；配置中心和接入端必须同步升级。多实例场景应通过共享存储实现全局 nonce 去重。

## 内部 UTC 时间

接入端接受新带时区 RFC3339 时间并转 UTC，旧无时区字符串按 UTC 解释；本地 revision 的 `updated_at` 以 UTC RFC3339Nano（带 `Z`）输出，详见[权威接口文档的内部时间约定](../admin/public/docs/index.html#s-intro)。部署必须先升级所有 access，再升级 center；回滚顺序相反，先由 center 恢复旧输出，再回滚 access。历史无时区字段不得自动批量加减 8 小时，详见[升级与历史数据边界](../admin/public/docs/guide.html#production-11)。

```bash
go test -race ./...
go vet ./...
go build ./cmd/config-center-access
```

[统一接口与签名规范](../admin/public/docs/index.html) · [同步架构](../admin/public/docs/guide.html#architecture) · [数据库结构](../admin/public/docs/guide.html#database) · [联调](../admin/public/docs/guide.html#development)

第三方同步接入协议见 [配置同步接入文档](../admin/public/docs/index.html#s-intro)。
