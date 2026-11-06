# 中台签名模块复用说明

## 设计原则

统一的是签名**计算和基础校验工具**，不是强制所有接口签相同的参数。各个业务仍在自己的处理层定义参与签名字段，保留原有平台、游戏和管理接口协议，不要求第三方重新接入。

| 现有场景 | 签名字段 | 共享实现 |
|---|---|---|
| 游戏 → SDK Core 订单登记 | gameSecret、AppID、时间戳、Nonce、Body 顶层业务值 | `BodyValues` + `SignValues` |
| SDK Core → 游戏发货通知 | 同上 | `BodyValues` + `SignValues` |
| 平台 → SDK Core 支付回调 V1 | order_no、game_order_id、appSecret、Nonce、时间戳 | `SignAllValues`（**保留空值**） |
| 游戏 → SDK Service 事件及订阅 | gameSecret、AppID、时间戳、Nonce、Body 顶层业务值 | `BodyValues` + `SignValues` |
| 平台 → SDK Service 用户、角色、区服查询 | appSecret、AppID、时间戳、Nonce、接口指定的业务值 | `SignValues` |
| SDK Service → 平台事件、订阅 | appSecret、AppID、时间戳、Nonce（不签 Body） | `SignValues` |
| 第三方 → 管理 API | 管理密钥、方法、路径、查询串、请求体摘要、时间戳、Nonce | `SignAdmin` |
| 配置中心 → 订阅者 Webhook | subscriberSecret、时间戳、Nonce、epoch、revision（十进制字符串） | `SignValues` + `ValidHeaders` + `EqualHex` |

## 共享工具

- `SignValues(values...)`：过滤空字符串，按值排序、下划线拼接、SHA-1 小写 hex。
- `SignAllValues(values...)`：不过滤空字符串，供支付平台 V1 兼容使用。
- `BodyValues(body)`：提取 JSON 对象顶层非空业务值，嵌套 JSON 压缩成紧凑字符串。
- `ValidHeaders(timestamp, nonce, signed, now)`：验证时间窗口（前后 5 分钟）、Nonce 和签名的格式。
- `EqualHex(expected, provided)`：常量时间比较签名十六进制字符串。
- `SignAdmin(...)`：管理请求专属的签名字段组装和计算。

## 边界

- `sdk-core/internal/auth`、`sdk-core/internal/payment`、`sdk-service/internal/auth`、`sdk-service/internal/platform` 继续负责业务字段组装、密钥查找、AppID 归属、环境与权限校验。
- `shared/signature` 不查数据库，也不判断订单金额、渠道、业务操作权限。
- 游戏、平台和管理接口的既有 SHA-1 算法、字段、路径和 Header 不变；配置同步 Webhook 从 HMAC-SHA256 改为共享排序签名，第三方接入端必须同步升级。
- SHA-1 是平台兼容的既有协议，不是现代新协议的安全建议；生产必须使用 HTTPS、保护密钥。
- 时间戳/Nonce 格式校验不等于跨实例的 Nonce 原子去重；业务层必须处理必要的幂等性。
- 回归测试中保留已知签名向量，重点检查空字符串、null、嵌套 JSON、时间偏差、支付回调 V1 特例。
