# sdk-core · SDK 核心服务

本工程保持按环境与 AppID 查询 SDK 配置，同时提供订单登记、支付回调中转与发货通知。SDK Core **直接读取 `config_center` 权威库**，不建立副本，也不执行 DDL。

## 配置查询

```http
GET /api/core/{env}/{appid}
```

- `env` 只接受 `prod`、`test`、`review`；`appid` 是全局唯一 `config_channel_app.code`。
- 游戏、应用、大区、区服组均须启用，且环境有有效绑定；否则返回 HTTP 404。
- 一条 JOIN 查询获取游戏、大区、区服组和渠道应用所需字段。
- 响应外壳为 `code` / `message` / `data`；`data` 字段由接口级映射决定。

## 全局响应映射

`internal/mapping/response-mapping.json` 通过 `go:embed` 编译到可执行程序。默认内容：

```json
{
  "GET /api/core/{env}/{appid}": {
    "snKey": "channel_app.code",
    "zoneKey": "server_group.code",
    "tsKey": "channel_app.config.tsKey"
  }
}
```

应用 `app1001` 的共享 JSON 为 `{"tsKey":"abc123"}`，正式服绑定的区服组 Code 为 `s1001` 时：

```json
{"code":0,"message":"success","data":{"snKey":"app1001","zoneKey":"s1001","tsKey":"abc123"}}
```

支持的来源：`channel_app.id/code/name/config(.xxx)`、`game.code/config(.xxx)`、`region.code/config(.xxx)`、`server_group.code/config(.xxx)`、`env`。映射不区分环境；同一来源的 JSON 每请求至多按需解析一次，不被引用的 JSON 不解码，但 SQL 仍取回原始 JSON。

- 映射路径不存在、为 `null` 或中间对象缺失时，省略 `data` 的对应字段；`false`、`0`、空字符串有效。
- JSON 数值按原类型保留；当前不支持映射表达式、SQL 或默认值。
- 新增来源路径在支持范围内时只需修改映射并**重新编译滚动发布**；新增接口的 JSON 键不会自动注册 Go HTTP 路由。
- 请求中引用的 JSON 非法、映射规则非法或数据库失败仍按错误处理，不能当作“字段缺失”静默忽略。
- 渠道包 `channel_app.config.appSecret` 与游戏 `game.config.gameSecret` 均为**服务端密钥**：SDK Core 公开映射不允许直接引用这两项，整个配置对象映射时也会自动过滤。但配置中心自身还有完整节点和同步 API，生产启用游戏密钥前必须限制其访问。
- 默认设置 `Cache-Control: no-store`，避免共享代理意外缓存敏感配置。

## 本地启动

在本目录操作，配置 `configs/config-dev.yaml` 中的 `database.dsn`，指向已包含 `game_order` 表的 `config_center`。订单功能始终加载，数据库账号必须具备配置表 SELECT 和订单表 SELECT/INSERT/UPDATE 权限：

```powershell
Copy-Item configs/config.yaml configs/config-dev.yaml
go run ./cmd/sdk-core -config configs/config-dev.yaml
Invoke-RestMethod "http://127.0.0.1:8082/api/core/prod/app1001"
```

可通过 `SDK_CORE_DB_DSN`、`SDK_CORE_ADDR` 覆盖配置。尚无匹配 AppID/绑定时 404 属预期结果。生产以 Nginx 代理多实例，每个实例使用独立端口；修改响应映射是客户端契约变更，应协调已有 SDK。

```bash
go test -race ./...
go vet ./...
go build ./cmd/sdk-core
```

[HTTP API](../docs/api.md) · [开发联调](../docs/development.md) · [生产部署](../docs/production.md)


## 订单登记与平台回调

SDK Core 使用 `config_center` 同一个数据库，订单登记与支付回调路由始终启用。数据库表由管理员手动初始化，SDK Core 不执行 DDL。

| 来源 | 接口 | 鉴权 |
| --- | --- | --- |
| 游戏服 | `POST /api/core/{env}/{appid}/orders` | 游戏侧 V3：HMAC-SHA256 |
| 第三方平台 | `POST /api/core/{appid}/pay/notify` | 原生平台 V1：SHA-1 |
| SDK Core | `POST {payNotifyUrl}` | 游戏侧 V3：HMAC-SHA256 |
| 客户端 | `GET /api/core/{env}/{appid}` | 原公开查询规则不变 |

支付平台的 V1 校验仍使用 `channel_app.config.appSecret`，对 `order_no`、`game_order_id`、`app_secret`、`nonce`、`timestamp` 做排序、下划线拼接并计算 SHA-1；**该协议不覆盖金额、attach、paid_at**，SDK Core 仍按订单登记值进行业务一致性校验，不增加二次查单。

订单首次登记时保存目标大区 `region.config.payNotifyUrl` 快照。支付回调先持久化确认并原样回显平台 `x-nonce`，随后由 Worker 异步通知游戏服。通知失败使用持久化重试；至少一次投递，游戏必须按订单号事务幂等发货。游戏通知成功条件为 HTTP 200、响应 Body 精确等于通知的 `x-nonce`。

## 游戏侧统一 V3 签名

游戏节点 `game.config.gameSecret` 维护唯一主密钥（建议高强度随机字符串）。解析时普通字符串使用 UTF-8，严格标准 Base64 且解码后恰好为 32 字节的密钥使用二进制字节。

游戏与 SDK Core 的通信使用同一协议，平台风格的请求头为：

```http
x-app-id: 110006
x-env: test
x-timestamp: 1793700000
x-nonce: 0123456789abcdef
x-signature: <64 位小写十六进制签名>
```

`x-env` 为内部新增字段：订单登记需要与路由环境相同；发货通知由 SDK Core 按登记环境传入。时间戳使用 Unix 秒，允许前后 5 分钟；nonce 为 8 随机字节的小写 hex。**时间窗口和 nonce 格式校验不等于防重放去重**，敏感的非幂等接口需要独立的短期去重存储。

签名字段来自编译内嵌的 `internal/auth/signature-rules.json`，由服务端通过操作标识选择，而不是请求自行提交字段列表。目前规则为：

- `order.register`：appid、env、method、operation、timestamp、nonce、body.sha256、body.game_order_id
- `payment.notify`：appid、env、method、operation、timestamp、nonce、body.sha256、body.order_no

取值后跳过空字符串，按**字符串字典序升序排序**并用下划线连接，再用 HMAC-SHA256 计算签名。 `body.sha256` 为原始 HTTP Body 字节的 SHA256 小写 hex；`body.xxx` 引用 Body 顶层字段。Body 若为字符串，直接取字符串内容；JSON 数字、布尔和结构对象使用无多余空白的 JSON 表示。

HKDF 使用 SHA-256、空 salt，派生 32 字节密钥；游戏请求的 `info=middle-platform:sdk-core:game-request:v3`，SDK Core 发货的 `info=middle-platform:sdk-core:game-notify:v3`。两种方向不能混用。游戏端必须按相同规则验签，不依赖 HTTP URL 或网关前缀。

修改签名规则必须**重新打包并同步升级游戏服**。此次 V3 不保留 V2 验签；已部署的 V2 游戏服无法继续登记或确认新通知。签名原理和完整接入示例见 [统一签名说明](../docs/sdk-signature.md) 与 [充值接入文档](../docs/recharge-integration.md)。

## 上线安全说明

公开配置映射不会输出 gameSecret，但配置中心管理、同步及完整节点读取仍可能返回原始配置。将主密钥写入游戏配置前，应确保相关接口受服务端权限控制，不能仅依赖前端隐藏字段。

平台 v1 未签署金额字段的风险仍在，必须确认平台服务端可信、支付成功校验有效，并结合源站访问控制降低伪造回调风险。直接轮换 gameSecret 可能导致已有未完成通知的验签失败。正式运行应优先使用 HTTPS。

## 单库单连接池配置（订单与通知模块常驻）

```yaml
database:
  dsn: "sdk_core:密码@tcp(127.0.0.1:3306)/config_center?parseTime=true"
  maxOpenConns: 20
  maxIdleConns: 10
  connMaxLifetime: "30m"
```

原配置中的 `order.enabled`、`payment.allowUnverifiedNonProd` 均需删除（严格 YAML 校验会拒绝旧字段）。支付回调通过 v1 验签、订单与金额一致性核对后进入支付确认和游戏发货，不额外查单。
**升级旧版 YAML**：删除 `orderDatabase`，不要保留 `SDK_CORE_ORDER_DB_DSN`。
启用订单前先备份数据库，对已有权威库手动执行 `config-center/db/sql/add_game_order.sql`；
运行时账号仅需配置表 SELECT、订单表 SELECT/INSERT/UPDATE，不需要 DDL。
如果原来两个账号分离，需新建或授权合并后的 `sdk_core` 账号；不能单纯沿用只有配置 SELECT 权限的账号。
如果 `game_order` 已存在，则无需重复执行 SQL；先核对表结构，切勿删除旧订单数据。
