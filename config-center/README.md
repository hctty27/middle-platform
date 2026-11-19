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

数据库连接池默认每实例最多 20 个开放连接，`database.maxOpenConns` 可从 YAML 调整。最大空闲连接数固定为 `min(10, maxOpenConns)`，连接生命周期固定为 30 分钟，与 SDK Core / SDK Service 统一；连接池配置重启后生效。

检查 `GET /health/ready`。服务启动不自动执行 DDL，节点变更推进 `epoch/revision`；Webhook 只发版本通知，接入方根据修订号异步拉取。渠道包不参与三级节点同步。

## 内部 UTC 时间

内部时间统一为 UTC 秒级：数据库字段使用 `DATETIME(0)`，SQL 当前时间使用 `UTC_TIMESTAMP()`，接口输出 RFC3339 并以 `Z` 结尾。已有权威库备份后执行 `db/incremental/V0019__config_center_second_timestamps.sql`；该脚本会先截断历史小数秒，再修改字段精度，不四舍五入。V0016 是历史毫秒迁移，不再是最终精度。发布顺序见[升级与历史数据边界](../admin/public/docs/guide.html#production-11)。

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

### 同步请求防重放容量
配置中心的管理接口与订阅者同步接口共用同一个有界 nonce 存储，每进程最多保留 320,000 个已验证 nonce，通常保留 5 分钟；未来时间戳按签名有效窗口延长保留时间。按秒清理过期键，缓存满时拒绝新请求（HTTP 409），不会驱逐仍有效的 nonce。容量按单进程、所有管理请求及订阅者合计计算，不代表承诺 QPS；进程重启后缓存清空，多实例之间仍不共享 nonce，因此不能防止攻击者将同一请求重放到另一实例。当前未新增 Redis/数据库持久化，也无需数据库迁移。生产部署前应结合 GOMEMLIMIT、实例数和实际签名请求速率评估内存及容量。
