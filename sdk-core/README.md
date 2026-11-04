# sdk-core · SDK 核心服务

SDK Core 直接访问 `config_center` 权威数据库，负责 SDK 配置查询、游戏订单登记、平台支付回调、异步发货通知及渠道事件同步转发；不使用 `config-center-access` 副本。

## 当前路由

| 方法 | 路径 | 方向 |
| --- | --- | --- |
| GET | `/api/core/config/{env}/{appid}` | 游戏/客户端 → Core |
| POST | `/api/core/orders/{env}/{appid}` | 游戏 → Core |
| POST | `/api/core/pay/notify/{appid}` | 平台 → Core（不含 env） |
| POST | `/api/core/events/{env}` | 游戏 → Core（AppID 在 x-app-id Header） |

**对外协议权威文档**：[游戏 HTML](../docs/game.html) · [平台 HTML](../docs/platform.html)。字段、签名、请求示例统一在这两份文件维护，模块 README 不再重复协议细节。

## 配置和业务约束

- `database.dsn` 指向 `config_center`；配置表只读、`game_order` 表允许 SELECT/INSERT/UPDATE，数据库结构需管理员事先手动执行 SQL。
- `platform.eventReportURL` 用于转发渠道事件，启用事件上报后必须配置；由服务端管理，不能让游戏请求指定目标。
- `game.config.gameSecret` 是游戏侧签名密钥；`channel_app.config.appSecret` 是平台侧密钥。
- 配置响应映射在 `internal/mapping/response-mapping.json` 编译进服务，修改需重新编译发布。禁止下发服务端密钥。
- 订单登记和异步发货 Worker 常驻。平台 V1 回调的签名未覆盖金额，Core 对比登记金额，但不额外查单。
- `event_report` 需要全局和渠道环境双开关；无绑定默认关闭。事件转发不落库、不自动重试；订单发货不受此开关影响。
- 游戏侧只使用当前 SHA-1 协议，不兼容旧 V2 签名。

## 本地启动

```powershell
Copy-Item configs/config.yaml configs/config-dev.yaml
# 按需配置 database.dsn / platform.eventReportURL
go run ./cmd/sdk-core -config configs/config-dev.yaml
```

```bash
go test -race ./...
go vet ./...
go build ./cmd/sdk-core
```

详见 [数据库与手动 SQL](../docs/database.md)、[开发联调](../docs/development.md)、[生产运维](../docs/production.md)。
