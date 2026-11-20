# sdk-admin · SDK 管理服务

独立承载 SDK 节点配置、渠道包、服务开关及订单管理接口。原 `/api/core/admin/**` 已迁移为 `/api/sdk/admin/**`。SDK Core 继续处理业务配置、订单、支付及发货 Worker。

- 与 SDK Core 共享 MySQL `config_center`，但使用独立连接池；不建表，不启动发货 Worker。
- 全部 `/api/sdk/admin/**` 使用现有 HMAC-SHA256 管理签名，并绑定新路径；与配置中心配置相同的 `adminAuth.secret`。
- 默认监听 `:8231`，生产由 Supervisor 指定 `SDK_ADMIN_ADDR=127.0.0.1:8231`；可用 `SDK_ADMIN_DB_DSN` 覆盖 DSN。
- 订单管理仍为每实例最多 2 个在途、3 秒预算；手动补偿只修改调度时间，发货由 SDK Core Worker 执行。
- 支持 `/health/live` 与 `/health/ready`；管理接口通过受控网关访问。

使用 `go run ./cmd/sdk-admin -config configs/config.yaml` 本地启动，运行前填入数据库 DSN 和至少 32 字节管理密钥。接口详情见 `../admin/public/docs/index.html`。
