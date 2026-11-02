# sdk-core · SDK 核心服务

本工程当前实现的是按环境与 AppID 查询 SDK 配置。登录、充值等能力属于后续规划；不要把服务名称当作这些接口已经上线的证明。SDK Core **直接读取 `config_center` 权威库**，不建立副本，也不执行 DDL。

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
- 不要映射支付私钥、签名密钥、数据库密码等服务端秘密；完整 `game.config` / `channel_app.config` 可能暴露所有属性。
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
