# 游戏充值接入说明

面向第三方游戏服务端。游戏只负责订单登记、接收支付成功发货通知；第三方支付平台原生回调由 SDK Core 自行处理。

## 1. 接入配置

从中台取得 `appid`、`env`（test / prod / review）、SDK Core 基础 URL、游戏专属 `gameSecret`，并由中台管理员配置对应大区的 `region.config.payNotifyUrl`。

游戏密钥只保存在服务端，作为普通 UTF-8 字符串使用；不要做 Base64 解码、HKDF 派生或 MD5 转换。

## 2. 游戏登记订单

```http
POST /api/core/{env}/{appid}/orders
Content-Type: application/json
x-app-id: 110006
x-timestamp: 1793700000
x-nonce: 0123456789abcdef
x-signature: <40 位小写 SHA-1>
```

```json
{"game_order_id":"G100","amount":600,"attach":"roleId=1"}
```

`game_order_id` 是游戏订单号，`amount` 单位分、必须大于 0，`attach` 为必填字符串但可以为空。

登记使用固定操作标识 `order.register`，签名参与值为：`gameSecret`、appid、timestamp、nonce、URL 路由中的 env、`order.register`、Body 所有顶层非空值。

过滤空字符串，按**值**排序，用 `_` 拼接后计算 SHA-1 小写 hex。游戏只能发这四个请求头，不需要 `x-env` 或操作标识 Header；env/操作标识由签名双方按约定确定。

```json
{"code":0,"message":"success","data":{"game_order_id":"G100","registration_status":"REGISTERED"}}
```

登记成功仅代表记录订单，不代表支付完成。同一 AppID 下的订单号跨环境唯一，相同参数重复登记为幂等成功；环境、金额或 attach 与原记录冲突返回 409。

## 3. SDK Core 发货通知

支付成功后，SDK Core 向登记时保存的 `payNotifyUrl` POST：

```json
{"order_no":"P100","game_order_id":"G100","amount":600,"attach":"roleId=1","paid_at":"2026-11-03T00:00:00Z"}
```

请求头仍为 `x-app-id`、`x-timestamp`、`x-nonce`、`x-signature`，使用同一份 `gameSecret`。发货签名的操作标识固定为 `payment.notify`，环境为**订单登记时保存的 env**，其余全部 Body 顶层非空字段参与签名。

游戏服务端必须验证签名、时间戳、AppID、订单 ID、金额和订单支付/发货状态，在事务内幂等发货。成功后返回 **HTTP 200，响应 Body 必须精确等于本次 x-nonce**（纯文本，不要加引号、换行、空格或 JSON）。

响应失败、非 200、超时或 nonce 不一致均会进入持久化重试。发货通知为至少一次投递，不能重复发放道具或货币。

## 4. 签名规则

完整字段选择、Java 示例与注意事项见 [游戏侧统一签名](game-signature.md)。不要把平台向游戏发货的原生签名规则直接照搬：内部协议增加了 env、operation 和完整 Body 字段签名；而平台支付回调继续走独立的平台 V1 SHA-1 规则。

## 5. 注意事项

- 不兼容 V2；上线前必须同步升级游戏签名和发货通知验签。
- 仅校验 nonce 格式而未记录已使用 nonce，不能把时间戳窗口当成完全防重放机制；订单依靠事务幂等。
- 传统 SHA-1 值拼接方案不具备现代 HMAC 的安全保证，生产使用 HTTPS 和高强度密钥。
- 平台 V1 的金额、attach 与 paid_at 并不被平台签名覆盖；中台只核对登记数据，不做二次查单。
