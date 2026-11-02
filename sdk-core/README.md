# sdk-core · SDK 核心服务

本工程保持按环境与 AppID 查询 SDK 配置；订单登记与支付通知中转作为可选模块开发，正式支付尚不可上线。SDK Core **直接读取 `config_center` 权威库**，不建立副本，也不执行 DDL。

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

在本目录操作，配置 `configs/config-dev.yaml` 中的 `database.dsn`，指向已完成 V12 迁移的 `config_center`：若 `order.enabled=false` 可使用只读账号；启用订单时，需要同一账号拥有订单表限定读写权限：

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


## 订单登记与支付通知（功能分支）

订单表 `game_order` 与所有配置中心表**共用一个 `config_center` 数据库**。
数据库建表只由 `config-center/cmd/migrate` 统一执行 V12（从 V11 升级），
不再提供 SDK Core 独立的 V1 建表脚本。
只保留 **`database.dsn` 一个配置项** 和一个 `*sql.DB` 连接池，`configStore`、`orderStore` 共享连接。
`order.enabled=false`（默认）不注册订单/支付回调路由，不启动补偿 Worker，也不需要订单表 DML 权限。
显式设置 `order.enabled=true` 后，启动时要求 V12 `game_order` 表存在并具备订单表查询及写入权限；配置表依旧只读。

接口：
- POST /api/core/{env}/{appid}/orders：游戏服签名登记订单
- POST /api/core/{appid}/pay/notify：接收真正平台 v1 回调
- GET /api/core/{env}/{appid}：保持现有配置查询兼容

登记 Body：game_order_id（同 AppID 跨环境唯一）、amount（人民币分）、attach（原样字符串）。
服务从绑定大区的 region.config 顶层读取 payNotifyUrl，首次登记后快照存储。
登记签名 Header：X-Game-Key-Id、X-Game-Timestamp、X-Game-Signature（HMAC-SHA256）。
游戏登记不再使用 `X-Game-Key-Id`。仅需 `X-Game-Timestamp` 与 `X-Game-Signature`：先从 AppID 所属游戏读取 `game.config.gameSecret`，经 HKDF 派生登记密钥，签名串为 `POST`、请求路径、Unix 秒时间戳、原始 Body 的 SHA256 小写十六进制四段，LF 连接且无末尾换行。具体见下文。

平台回调使用文档指定的 v1 SHA-1 协议，并校验已登记订单及绑定的平台订单号。平台密钥不是按环境配置在本服务 YAML 中，而是通过全局唯一 AppID 从 `config_channel_app.config` 的顶层 `appSecret` 读取；渠道包 JSON 跨 `prod/test/review` 环境共享同一份支付密钥，不要求当前环境仍处于绑定/启用状态，避免历史回调受影响。

渠道包示例（仅后台服务端配置，真实密钥不要提交仓库）：

```json
{"tsKey":"public-client-key","appSecret":"platform-server-secret"}
```

`env` 只用于记录原订单归属、选择登记时大区的 `payNotifyUrl`，以及决定是否允许非正式支付联调；不参与平台密钥查找。修改渠道包密钥前应保留历史回调所需密钥版本，目前尚未实现旧密钥轮换兼容。
**重大安全限制：平台 v1 未签名 amount、attach、paid_at，不能证明真实支付金额。正式环境回调默认且无条件返回失败（不会确认支付）。**
仅非正式环境在明确开启 payment.allowUnverifiedNonProd 后可以进行链路联调。
上线生产需先接入平台可信查单或可验证金额的升级签名协议。

SDK Core -> 游戏的发货通知不采用平台签名透明转发，而是独立签名。
请求 Body 包含 order_no、game_order_id、amount、attach、paid_at。
Header：X-SDK-AppID、X-SDK-Timestamp、X-SDK-Nonce、X-SDK-Signature。
签名串依次为 appid、POST、通知 URL 路径、timestamp、nonce、原始 Body 的 SHA256 小写十六进制，LF 连接，HMAC-SHA256。
游戏服须验证签名并按 game_order_id 数据库事务幂等发货，返回 HTTP 200，Body 精确等于 X-SDK-Nonce。
真实平台通知成功应答为 HTTP 200，Body 精确等于平台本次 x-nonce。

补偿 Worker 每两秒检查已支付未通知订单，领取任务并更新持久化下次重试时间。
重试签名独立生成，不复用过期平台 Header；通知发送不持有数据库事务。
仅允许 gameNotify.allowedHosts 明确批准的 HTTPS 公网域名，生产限制 443 端口且不跟随重定向。

配置示例参考 sdk-core/configs/config.yaml：平台 `appSecret` 配置在渠道包 JSON，游戏通信 `gameSecret` 配置在游戏 JSON；YAML 中不再维护 `payment.secrets`、`gameAuth.keys` 或 `gameNotify.secrets`。已有私有 YAML 的上述三个字段必须删掉，否则严格校验会拒绝启动。保留 `gameNotify.allowedHosts`。
不再使用 `SDK_CORE_ORDER_DB_DSN`；唯一 DSN 可由 `SDK_CORE_DB_DSN` 覆盖。旧的 `orderDatabase` YAML 块必须从私有配置中删除（严格配置校验会拒绝该未知字段）。
验证：在 sdk-core 目录执行 go test -race ./...、go vet ./...、go build ./cmd/sdk-core。

## 游戏主密钥与双向 HKDF 派生

只在每个游戏节点的 `game.config` 顶层配置一个 `gameSecret`（**32 个安全随机字节的标准 Base64 表示**），游戏服与 SDK Core 持有同一主密钥。生成建议：`openssl rand -base64 32`。不要把私钥、示例短密码写进仓库。

```json
{"gameSecret": "<base64-encoded-32-random-bytes>"}
```

使用 Go 标准库 `crypto/hkdf.Key(sha256.New, masterSecret, nil, info, 32)`：
- 登记：`info=middle-platform:sdk-core:order-register:v1`
- 通知：`info=middle-platform:sdk-core:payment-notify:v1`

两个子密钥由相同主密钥自动生成，无需分别配置，且不可混用于另一种请求。
`gameSecret` 在三个环境中共享；环境隔离要求较高时应改为不同环境的主密钥。

### 游戏服 → SDK Core（订单登记）

请求仍是 `POST /api/core/{env}/{appid}/orders`，请求头为 `X-Game-Timestamp` (Unix 秒) 和 `X-Game-Signature` (64 位小写十六进制)。

签名内容由以下四行以 LF 连接，无末尾换行，使用上述登记子密钥计算 HMAC-SHA256：

```text
POST
/api/core/test/app1001/orders
<timestamp>
<sha256-hex-of-raw-body>
```

SDK Core 通过 AppID/环境查找已绑定且启用的游戏，从 `game.config.gameSecret` 读取并验证签名。业务参数中的游戏和 AppID 不允许任意指定密钥来源。

### SDK Core → 游戏服（发货通知）

保留 `X-SDK-AppID`、`X-SDK-Timestamp`、`X-SDK-Nonce`、`X-SDK-Signature`，
继续使用原有六段签名串，只把 HMAC key 替换为上述通知子密钥。
`game_order.game_id` 是登记时的游戏 ID 快照，后台失败补偿通过原始游戏 ID 取密钥。
游戏服仍须验证时间戳、签名，并保证同一订单仅发货一次。

### 上线限制

**当前 config-center 的完整节点/批量/快照/管理 API 尚有输出原始 game.config 的路径**。SDK Core 的响应字段过滤不能防护这些接口。
在配置真实 `gameSecret` 之前，必须为配置中心和相关同步接口完成服务端访问控制或安全分发；
不能仅依赖前端隐藏字段。游戏服应使用受信任的服务端渠道取得主密钥。

当前没有密钥版本与双密钥轮换；直接更换 gameSecret 可能让存量未发货订单的补偿失败。
已完成的派生逻辑仅在 PR 分支；**真正平台 v1 不签金额，prod 支付确认仍然禁用**。


## 单库单连接池配置（默认关闭订单）

```yaml
database:
  dsn: "sdk_core:密码@tcp(127.0.0.1:3306)/config_center?parseTime=true"
  maxOpenConns: 20
  maxIdleConns: 10
  connMaxLifetime: "30m"
order:
  enabled: false
```

`order.enabled=true` 仅用于明确开启功能，不解除平台 v1 未签金额的正式环境安全门禁。
**升级旧版 YAML**：删除 `orderDatabase`，不要保留 `SDK_CORE_ORDER_DB_DSN`。
启用订单前先备份数据库，由 `config-center/cmd/migrate` 将 V11 升至 V12；
运行时账号仅需配置表 SELECT、订单表 SELECT/INSERT/UPDATE，不需要 DDL。
如果原来两个账号分离，需新建或授权合并后的 `sdk_core` 账号；不能单纯沿用只有配置 SELECT 权限的账号。
已有人工创建的 `game_order` 需先核对迁移，切勿直接删除旧订单数据。
