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


## 订单登记与支付通知

订单与配置中心共用 `config_center` 数据库；管理员手动执行建表 SQL，SDK Core 不自动迁移。订单登记、平台支付回调与异步发货服务始终启用。

| 请求方向 | 接口 | 签名规则 |
| --- | --- | --- |
| 游戏 → SDK Core | `POST /api/core/{env}/{appid}/orders` | 平台式 SHA-1 + 固定环境/业务/Body 值 |
| 平台 → SDK Core | `POST /api/core/{appid}/pay/notify` | 第三方平台原生 V1 SHA-1（不修改） |
| SDK Core → 游戏 | `POST {region.config.payNotifyUrl}` | 平台式 SHA-1 + 固定环境/业务/Body 值 |
| 客户端 → SDK Core | `GET /api/core/{env}/{appid}` | 公开查询逻辑不变 |

订单首次登记时把大区配置的 `payNotifyUrl` 保存为快照；支付平台回调通过独立 V1 验签，确认支付后保存状态并原样回显平台 `x-nonce`。游戏发货由持久化 Worker 异步重试，多实例使用数据库事务领取任务。游戏必须按订单号事务幂等发货。

平台 V1 通过 `channel_app.config.appSecret` 取密钥：只签 `order_no`、`game_order_id`、`appSecret`、`nonce`、`timestamp` 五个值，不签 `amount`、`attach`、`paid_at`。SDK Core 会比对登记金额和 attach，但不能把这种比较当成回调字段完整性证明；平台支付校验由平台负责，不增加二次查单。

### 游戏侧统一 SHA-1 签名

每个游戏只在 `game.config` 顶层配置一份 `gameSecret`：

```json
{"gameSecret": "高强度随机密钥"}
```

`gameSecret` 按原始 UTF-8 字符串读取，不再进行 Base64 特殊解码或 HKDF 派生。游戏登记与发货通知使用统一的四个请求头：

```http
x-app-id: 110006
x-timestamp: 1793700000
x-nonce: 0123456789abcdef
x-signature: <40 位小写 SHA-1 十六进制字符串>
```

和平台原始算法一样，所有参与签名的**值**过滤空字符串、按字典序升序排序，用 `_` 拼接后做 SHA-1。内部游戏接口除游戏密钥、appid、timestamp、nonce 之外，还要签接口固定的 `env`、`operation` 和全部 Body 顶层非空值：

| 操作 | operation 值 | env 来源 |
| --- | --- | --- |
| 订单登记 | `order.register` | URL 中的 `{env}` |
| 发货通知 | `payment.notify` | 订单登记时保存的环境；游戏服使用自身环境进行验签 |

签名不含 URL 路径和域名，也不额外增加 `x-env` Header。字符串空值和 null 不签，但数值 0、布尔 false 要签；嵌套对象或数组整体压缩为 JSON 文本再作为一个值。**签名值只做平台式排序，不按字段名排序**。现有订单登记与通知 Body 都只有普通字符串/整数，跨语言可直接复现。

校验时间戳前后 5 分钟，nonce 为 8 字节随机值的小写 hex；不建立 nonce 去重表。订单业务依靠数据库唯一索引与事务防止重复创建、重复发货。游戏通知要求返回 HTTP 200，响应 Body 精确等于本次 nonce，否则自动重试。

**本次不兼容旧版 V2**：须同时升级游戏服订单登记请求、游戏回调验签及管理端充值模拟。第三方平台 V1 协议保持完全不变。

详细接入示例见 [游戏侧签名协议](../docs/game-signature.md) 和 [充值接入说明](../docs/recharge-integration.md)。

### 安全边界

这是为了兼容第三方平台风格而采用的原生 SHA-1 规则，**不是标准 HMAC**。生产必须使用 HTTPS、高强度服务端密钥、业务授权和订单幂等，不能只依赖签名。按值排序不绑定字段名，理论上存在值交换歧义。现有配置中心部分完整配置接口会返回游戏原始 config，必须在服务端限制其访问，不允许经公开配置接口泄露游戏密钥。

直接轮换 gameSecret 可能让待补偿订单的签名与游戏侧密钥不一致，需协调游戏服同步切换。

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
