# sdk-core · SDK 核心服务

SDK Core 直接访问 `config_center` 权威数据库，负责 SDK 配置查询、游戏订单登记、平台支付回调及异步发货通知；不使用 `config-center-access` 副本。

## 当前路由

| 方法 | 路径 | 方向 |
| --- | --- | --- |
| GET | `/api/core/config/{env}/{appid}` | 游戏/客户端 → Core |
| POST | `/api/core/orders/{env}/{appid}` | 游戏 → Core |
| POST | `/api/core/pay/notify/{appid}` | 平台 → Core（不含 env） |

**对外协议权威文档**：[游戏 HTML](../docs/index.html#g-start) · [平台 HTML](../docs/index.html#p-start)。字段、签名、请求示例统一在此文档维护，模块 README 不再重复协议细节。

## 配置和业务约束

- `database.dsn` 指向 `config_center`；配置表只读、`game_order` 表允许 SELECT/INSERT/UPDATE，数据库结构需管理员事先手动执行 SQL。
- `sdk_node_config` 中游戏节点的 `gameSecret` 是游戏侧签名密钥；`sdk_channel_app.config.appSecret` 是平台侧密钥。
- 配置响应映射在 `internal/mapping/response-mapping.json` 编译进服务，修改需重新编译发布。禁止下发服务端密钥。
- 订单登记和异步发货 Worker 常驻。平台 V1 回调的签名未覆盖金额，Core 对比登记金额，但不额外查单。
- 事件上报和订阅消息已移交 `sdk-service`，旧路由 `/api/core/events/{env}` 不再注册。此服务承担 SDK 配置/渠道包/服务开关管理、核心配置查询、订单、充值和发货。
- 游戏侧只使用当前 SHA-1 协议，不兼容旧 V2 签名。

## 本地启动

```powershell
Copy-Item configs/config.yaml configs/config-dev.yaml
# 配置 database.dsn
go run ./cmd/sdk-core -config configs/config-dev.yaml
```

```bash
go test -race ./...
go vet ./...
go build ./cmd/sdk-core
```

详见 [数据库与手动 SQL](../docs/database.md)、[开发联调](../docs/development.md)、[生产运维](../docs/production.md)。


## SDK 管理接口（需网关鉴权）

- `GET/PUT /api/core/admin/node-configs/{id}`：统一管理游戏、大区及区服组 SDK JSON。
- `GET/POST /api/core/admin/channel-apps`、`PUT/DELETE /api/core/admin/channel-apps/{id}`：渠道包。
- `GET/PUT /api/core/admin/services[/{id}]`：全局功能开关（GET 列表、PUT 单项）。
- `GET/PUT /api/core/admin/channel-apps/{id}/services[/{serviceId}]`：渠道包环境服务开关。

管理路由目前无应用内登录，**必须用 Nginx 私有管理网关隔离**。公开 SDK 网关要返回 403，不得公开代理这些路由。

已有库需手工执行 `config-center/db/sql/migrate_sdk_node_config.sql`，之后再同时部署 Core、Service、配置中心和前端。详情参见 [数据库迁移](../docs/database.md)。
