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

SDK Core 与配置中心共用 `config_center` 数据库，订单表由管理员手动执行 SQL 建立，服务运行时不执行 DDL。

| 方向 | 接口 | 签名 |
| --- | --- | --- |
| 游戏 → SDK Core | `POST /api/core/{env}/{appid}/orders` | Java 风格参数排序 + MD5 大写 |
| 平台 → SDK Core | `POST /api/core/{appid}/pay/notify` | 平台原生 V1 SHA-1，保持不变 |
| SDK Core → 游戏 | `POST {region.config.payNotifyUrl}` | 与游戏登记相同的 MD5 算法 |
| 客户端 → SDK Core | `GET /api/core/{env}/{appid}` | 当前公开查询不变 |

订单首次登记时会将 `payNotifyUrl` 存为快照。支付平台回调成功后先保存状态并回显平台 `x-nonce`，游戏发货使用异步 Worker 补偿重试，至少一次投递，游戏侧必须保证同一订单只发货一次。

平台 v1 不对 `amount`、`attach`、`paid_at` 签名；SDK Core 只会与订单登记值比对，不新增二次查单逻辑。平台原生签名使用 `channel_app.config.appSecret`，与游戏通信使用的 `game.config.gameSecret` 分离。

### 游戏侧统一签名

只需要在 `game.config` 中配置 `gameSecret` 字符串；不使用 HKDF，不做 Base64 解码，直接按原始 UTF-8 字节签名。

```http
x-app-id: 110006
x-env: test
x-timestamp: 1793700000
x-nonce: 0123456789abcdef
x-signature: <32 位大写 MD5 hex>
```

参与签名的数据是请求 Body 的所有**顶层标量字段**，再加上：

```text
action=order.register   # 登记；游戏发货通知为 payment.notify
appid=110006
env=test
timestamp=1793700000
nonce=0123456789abcdef
```

将参数名称按字典序排序，跳过 null、空字符串和键名为 `sign` 的字段，对每个值执行与 Java `String.trim()` 一致的首尾字符去除，拼接为 `key=value&key=value`。末尾**直接追加 gameSecret，不加分隔符**，计算 UTF-8 的 MD5，并输出 32 位大写 hex。

```text
signature = UPPER_HEX(MD5(UTF8(sortJoinSign(data) + gameSecret)))
```

- 游戏登记与发货通知共用同一算法和密钥，但 `action` 不同，避免签名被跨业务直接复用。
- `x-env` 在登记时必须与 URL `env` 一致；发货通知取登记订单环境。
- 服务校验时间戳 ±5 分钟、nonce 为 16 位小写十六进制、签名恒定时间比较，但**不存储 nonce 去重**，仍需订单事务幂等。
- 不支持直接对 Map/数组嵌套值调用 Java `toString()` 参与跨语言签名。未来事件上报要先把嵌套参数按约定序列化为字符串，确保 Go 与 Java 字节一致。
- `attach` 等字段若包含 `&` 或 `=`，拼接串可能存在歧义；此方案是按游戏现有算法兼容的折中，不能代替 HTTPS、服务端校验和业务权限控制。
- 游戏侧旧的 V2 签名、请求头和 HKDF 派生密钥不再兼容。支付平台原生回调仍使用平台 V1。

详见 [游戏侧签名接入说明](../docs/game-signature.md) 与 [充值接入文档](../docs/recharge-integration.md)。

## 生产注意事项

SDK Core 公开响应映射过滤 `gameSecret`，但配置中心完整节点/同步/管理接口仍可能返回游戏原始配置。必须对配置中心访问进行服务端鉴权和网络隔离，不能仅依赖管理页面隐藏密钥。

MD5 密钥追加签名属于传统兼容方案，并不是现代推荐的消息认证算法。不要把游戏密钥放到客户端；生产请求优先使用 HTTPS，针对高风险操作应额外进行身份与权限校验。更改 `gameSecret` 可能让已有待补偿通知无法验签。

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
