# 渠道包事件上报接入说明

本接口仅支持**已绑定环境的渠道包事件**，不支持不带 AppID 的区服事件。游戏统一上报 SDK Core，由 Core 验签后同步转发平台。

## 1. 配置与开关

- 在配置中心为游戏设置 `game.config.gameSecret`，为渠道包设置 `channel_app.config.appSecret`。
- 管理后台「服务管理」启用 `event_report` 全局开关。
- 「渠道应用 → 服务配置」选择对应 `prod/test/review` 环境，启用事件上报。未配置时默认关闭。
- SDK Core 的 `configs/config.yaml` 配置 `platform.eventReportURL`，例如测试环境 `https://openapiv3.qk6.top/report/event`。测试和正式部署应分别设置各自确认过的平台地址，不在代码中写死域名。

## 2. 游戏请求

```http
POST /api/core/events/test
Content-Type: application/json
x-app-id: 110006
x-timestamp: <Unix 秒>
x-nonce: <16 位小写十六进制>
x-signature: <40 位小写 SHA-1>
```

```json
{
  "event": "login",
  "ext": {
    "areaId": "1",
    "openId": "user123",
    "roleId": "45940775",
    "roleLevel": 80
  }
}
```

- `event`：非空字符串，最长 128 字节。
- `ext`：JSON 对象，不能为空或数组。方舟、玩心等渠道的业务字段由游戏侧提供，Core 不修改其键名与结构。
- Body 只允许 `event`、`ext` 两个顶层字段；不允许重复键或额外字段。
- 请求体最大 64 KiB；不接受 URL 查询参数。

## 3. 游戏 → Core 签名

沿用现有游戏侧平台风格签名算法：

1. 取 `gameSecret`、Header 中 `x-app-id`、`x-timestamp`、`x-nonce` 以及 Body 顶层所有非空值。
2. `event` 作为普通字符串；`ext` 先转为紧凑 JSON 字符串并作为一个完整值。
3. 过滤空字符串、按**值**字典序升序排序、使用下划线连接、计算 SHA-1 小写十六进制。
4. `env`、接口路径和操作名称均不参与此签名；Core 单独检查 AppID 的环境绑定。

跨语言注意：对于嵌套 `ext`，Go 侧在压缩 JSON 时保留字段顺序，游戏侧须按照实际请求中 `ext` 的键顺序和 JSON 序列化格式生成签名。详情见 [游戏侧签名协议](game-signature.md)。

## 4. Core → 平台

- 固定请求 `POST {platform.eventReportURL}`，请求 Body 的 `event` 和 `ext` 原样透传。
- 按平台原生入站规则生成新的签名：仅包含 `appSecret`、AppID、时间戳、nonce，**不包含 Body**。
- 平台返回 HTTP 200、`code=0` 且 `data=true` 时视为成功。
- Core 成功响应：

```json
{"code":0,"message":"success","data":true}
```

## 5. 失败处理

| 情况 | Core HTTP 状态 |
| --- | --- |
| 请求参数或 JSON 不合法 | 400 |
| 游戏验签失败 | 401 |
| 对应渠道环境未启用事件上报 | 403 |
| 渠道包不存在或未绑定环境 | 404 |
| 游戏或渠道密钥配置不合法 | 422 |
| 平台转发成功 | 200 |
| 平台超时、HTTP/业务响应失败 | 502 |
| SDK Core 未配置平台地址、数据库不可用 | 503 |

平台转发设置 5 秒超时，不跟随 HTTP 重定向。当前不保存事件、不自动重试；游戏服可在失败后重试。需要注意：平台可能已经处理事件、但响应丢失，重试可能造成重复上报，应由游戏与平台共同考虑事件幂等性。

## 6. 上线顺序

1. 人工确认 SDK 服务表结构：无服务表执行 `add_sdk_services.sql`，已有通用绑定表执行 `migrate_sdk_service_channel.sql`；新空库执行 `initialize.sql`。操作前备份并停写。
2. 为 SDK Core 的数据库账号授予服务开关表 SELECT 权限。
3. 更新 SDK Core YAML 中的平台上报地址，再部署 SDK Core。
4. 管理端开启全局与目标渠道环境开关。
5. 以测试渠道验证验签失败、开关关闭、平台成功和超时场景。

现有订单登记、支付回调和发货通知接口与签名规则不变。
