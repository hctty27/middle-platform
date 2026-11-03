# 充值接入说明

游戏服只需要实现**订单登记**和**接收发货通知**。SDK Core 负责接受平台支付回调、记录支付状态以及失败后重试通知。

## 订单登记

```http
POST /api/core/{env}/{appid}/orders
Content-Type: application/json
x-app-id: 110006
x-env: test
x-timestamp: 1793700000
x-nonce: 0123456789abcdef
x-signature: <32 位 MD5 大写>
```

```json
{"game_order_id":"G100","amount":600,"attach":"roleId=1"}
```

金额单位为分；`attach` 必须存在但可以是空字符串。签名规则和 Java 工具见 [游戏侧统一签名](game-signature.md)。计算时把 Body 的所有顶层标量字段与 `action=order.register`、appid、env、timestamp、nonce 放进 Map，用 `gameSecret` 计算签名。签名必须与实际请求体一致。

成功返回：

```json
{"code":0,"message":"success","data":{"game_order_id":"G100","registration_status":"REGISTERED"}}
```

同一 AppID 的订单号跨环境唯一。参数一致的重复登记是幂等操作；金额或 attach 不同返回 409。订单登记成功不代表支付成功。

## 发货通知

游戏需要提供 `payNotifyUrl`，由管理员写入对应大区 `region.config`。SDK Core 向该地址 POST：

```json
{"order_no":"P100","game_order_id":"G100","amount":600,"attach":"roleId=1","paid_at":"2026-11-03T00:00:00Z"}
```

请求头同样为 `x-app-id`、`x-env`、`x-timestamp`、`x-nonce`、`x-signature`。签名算法完全一致，仅 `action=payment.notify` 不同。

游戏服收到后验签、核对本地订单/金额/attach，**在事务内幂等发货**。成功返回 HTTP 200，响应 Body 必须精确等于本次 `x-nonce`，不能加 JSON、空格或换行。响应错误或超时会自动重试。

## 重要说明

- `gameSecret` 为游戏配置中的原始 UTF-8 字符串，不再使用 HKDF；不兼容 V2。
- 生产尽量使用 HTTPS，校验时间戳与业务归属，不向客户端提供游戏密钥。
- 游戏侧签名使用传统的 MD5 密钥后缀方案，不是高强度 MAC；业务字段含 `&`、`=` 时应特别注意签名文本歧义。
- 支付平台 v1 回调由 SDK Core 独立处理，游戏方无需实现其 SHA-1 签名。
