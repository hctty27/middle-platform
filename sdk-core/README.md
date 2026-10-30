# SDK Core · 登录充值核心服务

Go SDK 核心服务。目前提供按环境读取配置的接口，未来登录、充值等接口可按路由复用统一 JSON 响应映射。

## SDK 配置接口

```http
GET /api/core/{env}/{appid}
```

- `env` 只接受 `prod`、`test`、`review`，用于选择对应区服组，不用于决定映射规则。
- `appid` 为全局唯一的 `config_channel_app.code`。
- 应用、游戏、大区和区服组均须启用；环境没有有效绑定时返回 HTTP 404。
- 直接读取配置中心权威 MySQL，单条 JOIN 查询获取渠道、游戏、大区和区服组所需字段，无需拆分查询和额外的显式事务。
- 统一响应外壳为 `code`、`message`、`data`；`data` 字段由接口级映射决定。

### 全局响应映射

映射配置内置于 `internal/mapping/response-mapping.json`，由 `go:embed` 编译到程序中。所有 AppID 和环境共用相同的接口规则，不增加数据库字段或后台配置界面。

```json
{
  "GET /api/core/{env}/{appid}": {
    "snKey": "channel_app.code",
    "zoneKey": "server_group.code",
    "tsKey": "channel_app.config.tsKey"
  }
}
```

示例：`GET /api/core/prod/app1001`，当应用共享 JSON 配置中有 `{"tsKey":"abc123"}` 且正式服绑定的区服组 Code 为 `s1001` 时返回：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "snKey": "app1001",
    "zoneKey": "s1001",
    "tsKey": "abc123"
  }
}
```

支持的来源路径：

| 来源 | 含义 |
|---|---|
| `channel_app.id` / `channel_app.code` / `channel_app.name` | 渠道应用信息 |
| `channel_app.config` / `channel_app.config.xxx` | 完整共享应用 JSON / 其中字段 |
| `game.code` | 游戏 Code |
| `game.config` / `game.config.xxx` | 游戏完整 JSON / 游戏 JSON 内字段（支持多层） |
| `region.code` | 当前环境大区 Code |
| `region.config` / `region.config.xxx` | 大区完整 JSON / 其中字段 |
| `server_group.code` | 当前环境区服组 Code |
| `server_group.config` / `server_group.config.xxx` | 区服组完整 JSON / 其中字段 |
| `env` | 请求参数（环境名） |

例如在映射中添加 `"regionKey": "region.config.regionKey"`、`"gameKey": "game.config.gameKey"`，或 `"gameConfig": "game.config"` 返回完整游戏 JSON。单 SQL 已读取游戏、大区、区服组、渠道应用的配置，后续新增以上路径只需修改映射文件并重新编译部署，不需要再修改 SQL。

映射规则在启动时预编译每个接口的 JSON 来源依赖。请求只解析被实际映射引用的 `channel_app.config`、`game.config`、`region.config`、`server_group.config`；同一个来源即使被多个字段使用也只解析一次。若接口只映射 `code` 等普通字段，就不执行 JSON 解码。单条 SQL 目前仍返回四层 JSON 原始数据，此优化不减少数据库传输字节数。

映射字段保留 JSON 原始值类型；数字解析使用 `json.Number` 避免精度丢失。当前仅支持字段提取，不支持表达式、SQL 或默认值。

启动时解析、校验全部映射规则，并验证当前接口已配置。每次请求使用预编译的只读规则，不重复解析映射文件。修改映射后需重新构建和滚动发布 `sdk-core`。

**新接口的接入方式：** 在 JSON 增加 `"METHOD /route/{param}"` 键，在 Go 代码实现该接口查询并构建来源上下文，再通过同一个 Resolver 按路由映射。新增 JSON 键不会自动注册路由。

**缺失字段处理：** 映射指定的配置路径不存在、为 `null`，或中间对象缺失时，直接省略对应的 `data` 字段，仍返回 HTTP 200；其他已配置字段正常返回。`false`、`0`、空字符串属于有效值，保留输出。未绑定环境或目标配置停用仍返回 HTTP 404；**被当前接口映射引用的** JSON 不是合法对象、映射规则非法或数据库查询失败，仍按原有错误处理；未被引用的 JSON 不会解码。

**安全：** 此接口可被客户端访问。任何被映射的 JSON 值都会对外暴露，特别是 `game.config` 等完整对象会暴露对象内的全部属性。不要映射支付私钥、登录签名密钥、数据库密码等服务端秘密；这些应该保存在仅服务端可访问的位置。优先选取必须的子字段，而不是返回整个对象。

## 本地启动（PowerShell）

要求 Go 1.27.3、MySQL 8.0+，且共享库已由 `config-center` 完成 V11 及后续必要迁移。

```powershell
cd sdk-core
Copy-Item configs/config.yaml configs/config-dev.yaml
```

编辑 `configs/config-dev.yaml`，将 `database.dsn` 指向配置中心的**同一数据库**：

```yaml
server:
  address: ":8082"
database:
  dsn: "sdk_reader:password@tcp(127.0.0.1:3306)/config_center?parseTime=true&loc=UTC"
  maxOpenConns: 20
  maxIdleConns: 10
  connMaxLifetime: "30m"
```

```powershell
go mod download
go run ./cmd/sdk-core -config configs/config-dev.yaml
Invoke-RestMethod http://127.0.0.1:8082/api/core/prod/app1001
```

可用 `SDK_CORE_DB_DSN`、`SDK_CORE_ADDR` 环境变量覆盖配置文件。含凭据的本地配置文件已由仓库根目录 `.gitignore` 排除。

## 生产与集群

- 使用同一权威 MySQL，只读账号；不创建副本，不执行 DDL。
- Nginx 通过 `/api/core/` 转发到多个 `sdk-core` 实例。
- 映射随程序发布；修改后对多实例执行滚动更新，避免同一接口出现长时间不同 JSON 版本。
- 修改当前响应结构属于客户端接口契约变化，需协调现有 SDK 客户端。
- 单条 SELECT 在数据库内一次性读取同一快照，但不承诺性能倍增；建议结合实际访问量和执行计划评估索引与数据库连接池。
- 默认使用 `Cache-Control: no-store`，避免配置在共享代理中意外缓存。

## 验证

```powershell
go test -race ./...
go vet ./...
go build ./cmd/sdk-core
```
