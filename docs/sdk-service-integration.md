# SDK Service 用户核验接入说明

## 组件与路由

平台调用 `POST /api/service/user/check/{env}`，AppID 在 `x-app-id`，Body 仅含字符串 `open_id`。不使用 `/sdk` 前缀。
`sdk-service` 直接查询 `config_center`，依据渠道包绑定找到游戏和大区；先检查 `user_check` 服务全局开关及渠道环境开关，再用平台密钥验签，通过注册表选择游戏适配器并同步调用。

平台成功响应为 `{"code":0,"message":"ok","data":true}`。用户不存在为 `{"code":1021003,"message":"用户信息不存在","data":null}`。
游戏故障不映射成用户不存在。

## 游戏配置

```json
{
  "gameSecret": "服务端密钥"
}
```

默认不需要配置适配器。针对特殊协议，才添加 `game.config.serviceAdapters`，例如：

```json
{
  "serviceAdapters": {
    "user_check": "game_a"
  }
}
```

适配器选择由通用的 `NameForService` 统一处理；以后新增角色查询、区服查询等接口也遵循“未配置使用 standard、明确指定使用对应实现”的规则。明确指定但未注册的名称报错，不自动回退。

大区 `region.config`：

```json
{"userCheckUrl":"https://game.example.com/internal/user/check"}
```

渠道 `config_channel_app.config.appSecret` 存平台签名密钥。无需新增协议映射表。自定义游戏协议由新的 Go `UserCheckAdapter` 实现，每个功能的适配器相互独立。

## 重要协议确认

目前平台《服务端集成文档》对角色/区服查询明确规定平台出站签名含 Body 顶层所有非空值，但**核验用户信息章节未独立列出签名字段**。当前实现按 `open_id` 参与签名，联调时需要平台提供签名样本。如果平台实际规则不同，应修改并锁定验签测试，不可无条件兼容两套签名规则。

平台是否支持同一 AppID 为不同 `prod/test/review` 环境提供独立回调地址，也需要上线前确认。支付回调与事件上报原有接口保持不变。

## 错误与边界

缺少参数返回 400、签名错误 401、功能未启用 403、渠道未绑定环境 404、游戏上游异常 502、游戏超时 504、配置或数据库异常 503。上述异常 HTTP 码属于中台建议，需与平台确认非 200 行为。

访问日志不记录原始 `open_id`、签名和密钥。游戏核验不缓存、不落库、不自动重试。外部平台请求无法指定内网转发地址，客户端禁止重定向。公网目标优先 HTTPS，内网 HTTP 需网络隔离。
