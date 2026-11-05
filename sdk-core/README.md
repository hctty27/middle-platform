# sdk-core · SDK 核心服务

直接访问共享权威库 `config_center`，负责 SDK 节点配置、渠道包与功能开关管理，以及配置查询、订单登记、平台支付回调和异步游戏发货。事件上报与订阅消息已归 `sdk-service` 处理。

## 主要入口

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/core/config/{env}/{appid}` | 获取 SDK 配置 |
| POST | `/api/core/orders/{env}/{appid}` | 登记游戏订单 |
| POST | `/api/core/pay/notify/{appid}` | 平台支付回调 |
| GET/PUT | `/api/core/admin/node-configs/{id}` | SDK 节点配置管理 |
| GET/POST | `/api/core/admin/channel-apps` | 渠道包查询与新增 |
| GET/PUT | `/api/core/admin/services[/{id}]` | 服务开关查询与更新 |

上述表格仅用于快速定位；完整字段、全部管理路由、签名、错误码和请求示例统一以 [接口文档](../docs/index.html) 为准。

## 本地启动

```powershell
Copy-Item configs/config.yaml configs/config-dev.yaml
# 设置 database.dsn 指向 config_center
go run ./cmd/sdk-core -config configs/config-dev.yaml
```

新库按 [数据库文档](../docs/guide.html#database) 初始化，旧库手动检查增量 SQL；服务不会执行 DDL。订单与发货必须幂等，支付回调是否已成功完成需基于持久化状态判断。接口字段映射编译在 `internal/mapping/response-mapping.json`，修改后需要重新构建。

`/api/core/admin/**` 默认使用 `adminAuth` 签名，与配置中心使用相同管理密钥；生产仍需网关隔离并启用 HTTPS，不应公开暴露管理路由。

```bash
go test -race ./...
go vet ./...
go build ./cmd/sdk-core
```

[架构](../docs/guide.html#architecture) · [本地开发](../docs/guide.html#development) · [生产运维](../docs/guide.html#production)
