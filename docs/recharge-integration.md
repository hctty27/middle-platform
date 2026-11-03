# 游戏充值第三方接入说明

> 适用对象：接入研发中台充值功能的游戏服务端开发者。  
> 对应版本：以本文件所在分支的 SDK Core 实际部署版本为准。  
> 仅介绍游戏方需要实现的订单登记、发货通知与签名，不涉及支付平台内部回调、数据库和任务调度。

## 1. 接入流程

1. **配置接入信息**：向中台管理员获取 `appid`、`env`、`sdkCoreBaseUrl` 和游戏密钥，并提供发货通知地址。
2. **登记订单**：玩家下单后，由游戏服务端向 SDK Core 登记订单，再继续业务支付流程。
3. **接收发货通知**：支付成功后，中台通知游戏服务端；游戏服验签、核对订单、幂等发货并确认。

> 登记成功不代表支付成功。不要在登记成功时发货。

## 2. 接入参数

| 参数 | 说明 |
| --- | --- |
| `appid` | 渠道应用 Code，例如 `110006` |
| `env` | `test` 测试、`prod` 正式、`review` 提审 |
| `sdkCoreBaseUrl` | SDK Core 服务地址，由中台管理员提供 |
| `gameSecret` | 共享游戏主密钥，**任意非空字符串**（无需 Base64 或固定长度）；仅保存在服务端，建议使用足够长的随机密钥 |
| `payNotifyUrl` | 游戏服接收发货通知的 HTTP/HTTPS 地址；由管理员配置到对应 `region.config` |

双方使用同一份 `gameSecret`，但会通过 HKDF-SHA256 分别派生订单登记密钥和发货通知密钥；不可直接用主密钥签名。普通字符串按 UTF-8 原始字节处理；**为兼容旧配置**，如果字符串恰好是标准 Base64 且解码后为 32 字节，仍按解码后的二进制密钥处理。请确保游戏方采用相同规则。

## 3. 登记订单

**接口：** `POST /api/core/{env}/{appid}/orders`

请求头：

| Header | 内容 |
| --- | --- |
| `Content-Type` | `application/json` |
| `X-Game-Timestamp` | Unix 秒时间戳 |
| `X-Game-Signature` | 64 位小写十六进制 HMAC-SHA256 签名 |

请求 Body：

```json
{
  "game_order_id": "G202611030001",
  "amount": 600,
  "attach": "roleId=10001&productId=gem60"
}
```

| 字段 | 说明 |
| --- | --- |
| `game_order_id` | 游戏订单号；同一 `appid` 下跨环境唯一 |
| `amount` | 金额单位：人民币**分**，`600` 表示 6 元 |
| `attach` | 自定义透传字符串，必填，可传空字符串 |

成功返回 HTTP 200：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "game_order_id": "G202611030001",
    "registration_status": "REGISTERED"
  }
}
```

相同订单号、环境、金额和 `attach` 可重复登记；参数冲突返回 HTTP 409。

### 订单登记签名

首先派生登记密钥：

```text
IKM    = gameSecret 的 UTF-8 字节；旧版标准 Base64 的 32 字节密钥仍按 Base64 解码后的字节
salt   = nil
info   = middle-platform:sdk-core:order-register:v2
长度   = 32 字节
```

对**实际发送的原始 JSON 字节**计算 `bodyHash = hex_lower(SHA256(rawBody))`，拼接五行字符串，行间为换行符（LF），末尾无换行：

```text
POST
test
110006
<Unix 秒时间戳>
<bodyHash>
```

其中第二行是环境 `env`，第三行是渠道应用 `appid`，必须与请求路由值一致，防止跨环境、跨应用重放。

```text
X-Game-Signature = hex_lower(HMAC_SHA256(登记密钥, 五行字符串))
```

时间戳允许前后 5 分钟偏差。**签名不包含 URL、域名、端口或请求路径**，无需关心 Nginx / API 网关的路径前缀。

## 4. 接收支付成功通知

游戏方只需提供 `payNotifyUrl` 接口，不需要实现支付平台的 V1 回调。

中台会向该地址发起 POST，Body 示例：

```json
{
  "order_no": "P202611030001",
  "game_order_id": "G202611030001",
  "amount": 600,
  "attach": "roleId=10001&productId=gem60",
  "paid_at": "2026-11-03T03:00:00Z"
}
```

请求头：

| Header | 含义 |
| --- | --- |
| `X-SDK-AppID` | 渠道应用 Code |
| `X-SDK-Timestamp` | Unix 秒时间戳 |
| `X-SDK-Nonce` | 16 位小写十六进制随机值 |
| `X-SDK-Signature` | 64 位小写十六进制签名 |

游戏服需要：

1. 校验签名与时间戳。
2. 按 `game_order_id` 查本地原始订单，核对 `appid`、金额、`attach`。
3. **事务内幂等发货**：同一订单无论收到几次通知，最多发货一次。
4. 业务处理成功后回复 **HTTP 200，Body 必须精确等于本次 `X-SDK-Nonce`**，不可附加 JSON 或换行。

### 发货通知验签

使用同一个 `gameSecret` 派生另一把密钥：

```text
IKM    = gameSecret 的 UTF-8 字节；旧版标准 Base64 的 32 字节密钥仍按 Base64 解码后的字节
salt   = nil
info   = middle-platform:sdk-core:payment-notify:v2
长度   = 32 字节
```

拼接五行签名串（LF 分隔，末尾无换行）：

```text
<appid>
POST
<X-SDK-Timestamp>
<X-SDK-Nonce>
<hex_lower(SHA256(原始通知 Body))>
```

```text
X-SDK-Signature = hex_lower(HMAC_SHA256(发货通知密钥, 五行字符串))
```

**发货通知签名不包含通知 URL 路径**。游戏服仍必须校验 AppID、签名、时间戳、订单和金额，并确保幂等发货。

若通知超时、响应不是 HTTP 200，或 Body 不等于随机 nonce，中台会自动重试。因此游戏端必须保证订单幂等，不应把“通知只调用一次”作为前提。

## 5. 联调检查

- [ ] 已确认 `appid`、`env`、`sdkCoreBaseUrl`、`gameSecret` 和 `payNotifyUrl`。
- [ ] 订单登记返回 `REGISTERED`；重复请求不会创建重复订单。
- [ ] 同一订单号改变金额后返回 HTTP 409。
- [ ] 支付通知能通过验签并匹配原始游戏订单。
- [ ] 成功发货时返回 HTTP 200，Body 与 `X-SDK-Nonce` 完全一致。
- [ ] 重复通知不会重复加道具或货币。

## 6. 注意事项

- **V2 协议变更**：登记签名和发货通知签名均不再包含请求路径，HKDF `info` 从 `:v1` 升级为 `:v2`。已接入的游戏服必须同步修改签名和验签代码；旧版签名不会被新版 SDK Core 接受，旧版游戏服也不会接受新版发货通知。
- `gameSecret` 不得放在客户端或日志中；正式环境推荐使用 HTTPS。虽然取消了长度限制，但过短或容易猜测的密钥仍存在风险，生产建议使用高强度随机密钥。
- 若 `gameSecret` 恰好符合旧版 32 字节标准 Base64 格式，将优先按旧格式解码；接入方应保持相同的兼容策略。
- `payNotifyUrl` 在订单登记时保存快照，后续修改不会改变已登记订单的目标地址。
- 目前没有独立的游戏方订单状态查询 API；请自行保存订单和发货记录。

**参考源码：** [订单登记](https://github.com/hctty27/middle-platform/blob/main/sdk-core/internal/api/order.go) · [验签/HKDF](https://github.com/hctty27/middle-platform/blob/main/sdk-core/internal/auth/game.go) · [通知签名](https://github.com/hctty27/middle-platform/blob/main/sdk-core/internal/notify/client.go)
