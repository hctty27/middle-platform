# 游戏充值第三方接入说明

本说明面向游戏服务端，仅涉及订单登记及游戏发货通知。平台回调由 SDK Core 自行适配。

## 1. 接入信息

从中台管理员取得 `appid`、`env`（test/prod/review）、SDK Core 基础 URL 和 `gameSecret`，并将游戏通知 URL 配置到对应 `region.config.payNotifyUrl`。游戏密钥仅在服务端保存，建议使用高强度随机值。

## 2. 登记订单

```http
POST /api/core/{env}/{appid}/orders
Content-Type: application/json
x-app-id: 110006
x-env: test
x-timestamp: 1793700000
x-nonce: 0123456789abcdef
x-signature: <HMAC-SHA256 hex>
```

请求 Body：

```json
{
  "game_order_id": "G202611030001",
  "amount": 600,
  "attach": "roleId=10001&productId=gem60"
}
```

金额单位为分，`attach` 是必填字符串但允许为空；相同 AppID 的订单号跨环境唯一。成功返回 HTTP 200：

```json
{"code":0,"message":"success","data":{"game_order_id":"G202611030001","registration_status":"REGISTERED"}}
```

相同订单的完全一致参数重复登记为幂等成功；修改金额或 attach 则返回 409。**登记成功不代表支付成功。**

登记签名操作标识为 `order.register`，选取以下八个值：

```text
appid = 110006
env = test
method = POST
operation = order.register
timestamp = 1793700000
nonce = 0123456789abcdef
body.sha256 = hex_lower(SHA256(原始 HTTP Body))
body.game_order_id = G202611030001
```

按照**值**做字典序升序排序，用 `_` 连接后用游戏请求密钥计算 HMAC-SHA256，输出 64 位小写十六进制值写入 `x-signature`。不要对重新序列化后的 JSON 计算 Body Hash。

## 3. 接收发货通知

SDK Core 向 `payNotifyUrl` 发起 POST，同样使用 `x-app-id`、`x-env`、`x-timestamp`、`x-nonce`、`x-signature` 五个请求头。

```json
{
  "order_no": "P202611030001",
  "game_order_id": "G202611030001",
  "amount": 600,
  "attach": "roleId=10001&productId=gem60",
  "paid_at": "2026-11-03T03:00:00Z"
}
```

签名操作标识为 `payment.notify`，和订单登记采用相同规则，但最后一项选 `body.order_no`，而不是 `body.game_order_id`；密钥方向不同。

游戏服依次验证时间戳、nonce 格式、AppID/环境、V3 签名，并查询本地订单核对金额及 attach。确认成功后在事务中按 `game_order_id` 幂等发货，回复 **HTTP 200，Body 精确等于本次 `x-nonce` 字符串**（无 JSON 包装、空格或换行）。重复通知不能再次发货。

SDK Core 收不到正确确认时会重试；不能假设通知只发生一次。

## 4. 主密钥与派生

`gameSecret` 默认使用 UTF-8 字节；如果恰好为严格标准 Base64 且能解出 32 字节，则使用解码后的字节（保持现有密钥格式习惯）。HKDF-SHA256 的 salt 为空，输出 32 字节：

| 方向 | HKDF info |
| --- | --- |
| 游戏 → SDK Core | `middle-platform:sdk-core:game-request:v3` |
| SDK Core → 游戏 | `middle-platform:sdk-core:game-notify:v3` |

两边分别按这些 info 派生不同的 HMAC 密钥。签名字段按接口规则控制，完整算法见 [签名协议](sdk-signature.md)。

## 5. 注意事项

- V3 不兼容旧 V2：服务端上线前必须同步修改 Java/Go 接入端以及游戏通知验签。
- 时间戳允许前后 5 分钟，仅校验 nonce 格式不等于去重。充值务必采用订单幂等处理。
- 不得在客户端或日志中暴露 gameSecret；建议全程使用 HTTPS。
- 生产环境不应使用不验签的 `tools/mock-notify` 模拟服务。
- 平台向 SDK Core 的 SHA-1 回调**不属于本协议**，无需游戏端实现。
