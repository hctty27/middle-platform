# SDK Core 统一签名协议 V3

## 目标

内部游戏通信使用平台风格的 Header 和“取值 → 过滤空字符串 → 排序 → 下划线拼接”规则，但实际摘要算法为 **HMAC-SHA256**（不是第三方平台的 SHA-1）。平台原有 V1 算法保持原状，不走本组件。

服务端规则定义位于 `sdk-core/internal/auth/signature-rules.json`，编译时内嵌。新接口须新增操作标识、Go 路由和规则；仅添加 JSON 不会自动开放接口。

## Header

| Header | 约束 |
| --- | --- |
| `x-app-id` | 必须与 SDK Core 路由 AppID 对应 |
| `x-env` | test/prod/review；登记需与路由环境一致，通知由订单环境确定 |
| `x-timestamp` | Unix 秒，当前时间前后不超过五分钟 |
| `x-nonce` | 8 字节密码学随机数的小写 hex，16 字符 |
| `x-signature` | 64 字符小写 hex |

上述字段均为必填；V2 请求头不再兼容。

## 规则示例

```json
{
  "order.register": {
    "algorithm": "hmac-sha256",
    "fields": [
      "appid", "env", "method", "operation", "timestamp", "nonce",
      "body.sha256", "body.game_order_id"
    ]
  },
  "payment.notify": {
    "algorithm": "hmac-sha256",
    "fields": [
      "appid", "env", "method", "operation", "timestamp", "nonce",
      "body.sha256", "body.order_no"
    ]
  }
}
```

`appid`、`env`、`method`、`operation`、`timestamp`、`nonce`、`body.sha256` 是强制字段，其余顶层 `body.xxx` 可根据接口增加、删除。缺少的可选业务字段和空字符串不参与拼接；数字 `0`、布尔值 `false` 正常参与。

`body.sha256` 是原始 HTTP Body 字节的 SHA256 小写 hex，避免因 JSON 重排导致与实际 HTTP 请求不符。

## 计算过程

1. 根据内部路由确定 `operation`，根据路径或订单环境确定 `env` 与 `appid`；不信任客户端自行指定操作。
2. 按规则读取字段值，将非空值转为字符串。
3. 对所有参与签名的**值**做字典序升序排序，使用 ASCII 下划线 `_` 拼接，中间不插入换行。
4. 使用方向专属的 HKDF-SHA256 密钥执行 HMAC-SHA256，输出小写 hex。

```text
game-request key = HKDF_SHA256(gameSecret, salt="", info="middle-platform:sdk-core:game-request:v3", length=32)
game-notify  key = HKDF_SHA256(gameSecret, salt="", info="middle-platform:sdk-core:game-notify:v3", length=32)

signature = hex_lower(HMAC_SHA256(derivedKey, sort(nonEmptyValues).join("_")))
```

密钥用途隔离可避免双向复用。Go 统一组件使用 `crypto/hmac.Equal` 恒定时间比较签名。详情参见 [Go HMAC 文档](https://pkg.go.dev/crypto/hmac) 和 [HKDF RFC 5869](https://www.rfc-editor.org/rfc/rfc5869)。

## 协议边界

- SDK Core 不接受调用者传来的字段选择或算法名称。
- 禁止禁用固定安全字段；修改规则必须重新打包，并同时更新 Java/Go 客户端及管理端联调页面。
- 签名覆盖完整 Body，但不能代替业务的幂等性校验、权限控制或敏感接口的 nonce 去重存储。
- URL 路径、域名、端口和代理前缀不参与内部签名；业务操作和环境参与。
- 第三方平台 V1 回调继续独立校验 SHA-1；它不签署 amount/attach/paid_at，不能当作金额完整性证明。
