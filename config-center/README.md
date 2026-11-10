# config-center · 权威配置中心

负责游戏、大区、区服组基础节点及其同步版本、订阅者管理、增量/快照接口和 Webhook 投递。使用共享权威库 `config_center`；SDK 专属节点配置、渠道包及功能开关由 `sdk-core` 管理，不在这里重复维护。

## 本地启动

复制 YAML 后，为 `adminAuth.secret` 配置至少 32 字节的高熵密钥；`ADMIN_AUTH_SECRET` 可覆盖管理密钥。

1. 新建空库时手工执行 `../db/full/config_center.sql`；已有库按 [数据库文档](../admin/public/docs/guide.html#database) 核对后选择增量 SQL，禁止重新执行全量初始化。
2. 将 `configs/config.yaml` 复制为**完整独立**的 `configs/config-dev.yaml` 并填写 `database.dsn`。启动时只读取指定文件；不传 `-config` 时默认读取当前工作目录下的 `configs/config.yaml`。环境变量仍可覆盖指定字段。
3. 在本目录运行：

```powershell
go run ./cmd/config-center -config configs/config-dev.yaml
```

检查 `GET /health/ready`。服务启动不自动执行 DDL，节点变更推进 `epoch/revision`；Webhook 只发版本通知，接入方根据修订号异步拉取。渠道包不参与三级节点同步。

## 安全与参考

`/api/config/admin/**` 始终使用 HMAC-SHA256，密钥为 `adminAuth.secret`，须与 SDK Core 的管理密钥一致。`/api/config/sync/**`、`/api/config/nodes/**` 根据请求头 `X-Subscriber-Code` 查找 `config_subscriber` 对应记录，并使用该记录的 `signing_secret` 验签。配置中心发送 Webhook 时也使用同一条订阅记录的密钥。每个接入端必须使用唯一且独立的密钥，长度为 32–255 字节且不得包含首尾空格；停用订阅者会拒绝该接入端的同步读取请求并停止新的 Webhook 投递。已有数据库升级时先备份并检查旧 `webhook_secret` 是否重复、长度不在 32–255 字节范围内或包含首尾空格，再手工执行 `db/incremental/V0012__rename_webhook_secret_to_signing_secret.sql`。密钥轮换必须同步更新接入端 `subscriber.secret` 并重启接入端；不能只更新一侧。

所有签名均绑定接入端编码、方法、转义路径、规范化 Query、时间戳、nonce 和原始 Body。密钥不能提交到仓库；生产必须使用 HTTPS 并限制网关访问范围。

- [统一接口与签名规范](../admin/public/docs/index.html)
- [架构与内部路由](../admin/public/docs/guide.html#architecture)
- [数据库与手动 SQL](../admin/public/docs/guide.html#database)
- [本地开发](../admin/public/docs/guide.html#development)
- [生产运维](../admin/public/docs/guide.html#production)

```bash
go test -race ./...
go vet ./...
go build ./cmd/config-center
```
