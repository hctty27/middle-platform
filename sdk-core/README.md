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
- 渠道包 `channel_app.config.appSecret` 是平台验签密钥，公开字段映射禁止直接引用该路径；映射整个 `channel_app.config` 时会自动删除 `appSecret`，其他非敏感字段保持兼容。注意其他配置层级的秘密仍需自行控制映射与权限。
- 默认设置 `Cache-Control: no-store`，避免共享代理意外缓存敏感配置。

## 本地启动

在本目录操作，配置 `configs/config-dev.yaml` 中的 `database.dsn`，指向已完成 V11 及后续所需迁移的 `config_center` **只读账号**：

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

此模块需要在独立 MySQL 订单库手动执行 sdk-core/db/migration/V1__create_game_order.sql。
订单库连接独立于配置中心只读账号；未配置 orderDatabase.dsn 时，订单接口不会注册。

接口：
- POST /api/core/{env}/{appid}/orders：游戏服签名登记订单
- POST /api/core/{appid}/pay/notify：接收真正平台 v1 回调
- GET /api/core/{env}/{appid}：保持现有配置查询兼容

登记 Body：game_order_id（同 AppID 跨环境唯一）、amount（人民币分）、attach（原样字符串）。
服务从绑定大区的 region.config 顶层读取 payNotifyUrl，首次登记后快照存储。
登记签名 Header：X-Game-Key-Id、X-Game-Timestamp、X-Game-Signature（HMAC-SHA256）。
游戏登记签名的五行文本依次为：keyID、POST、外部请求路径、Unix秒时间戳、原始Body的SHA256小写十六进制；以 LF 连接。

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

配置示例参考 sdk-core/configs/config.yaml。平台 `appSecret` 在渠道包 `config` 中维护，**不要**配置 `payment.secrets`；游戏登记及 SDK Core → 游戏通知所用 HMAC 密钥仍保存在服务端私有配置中，长度至少 32 字节。如果已复制旧版 `config.yaml`，请删除旧的 `payment.secrets` 字段，否则配置严格校验会拒绝启动。
可使用 SDK_CORE_ORDER_DB_DSN 覆盖订单库 DSN。
验证：在 sdk-core 目录执行 go test -race ./...、go vet ./...、go build ./cmd/sdk-core。
