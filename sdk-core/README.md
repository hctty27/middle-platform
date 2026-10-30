# SDK Core · 登录充值核心服务

独立 Go 工程，第一阶段提供只读 SDK 配置查询。未来登录、充值接口在此工程中继续扩展。

## 接口

```http
GET /api/core/{env}/{appid}
```

- `env` 只接受 `test`、`review`、`prod`。
- `appid` 对应 `config_channel_app.code`。
- 返回三个环境共用的 `config_channel_app.config` JSON；根据 `prod_server_group_id` / `test_server_group_id` / `review_server_group_id` 选择唯一的环境区服组。
- `regions` 只包含当前 AppID 在指定环境下 **实际存在且已启用的绑定区服组**；没有有效绑定的大区不返回。
- 渠道应用及所属游戏必须启用，大区、区服组、绑定也必须启用。
- 已找到 AppID 但指定环境没有绑定，返回 200 和 `regions: []`。
- AppID 不存在或被禁用返回 404；跨游戏出现相同 AppID 返回 409，而非任意选择一个。
- 每次请求直接查询配置中心的权威 MySQL，没有缓存和延迟同步。

示例：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "appid": "huawei_10001",
    "env": "prod",
    "config": {"loginEnabled": true},
    "regions": [
      {
        "code": "region_1",
        "serverGroups": [
          {"code": "group_1001"},
          {"code": "group_1002"}
        ]
      }
    ]
  }
}
```

错误响应使用相应 HTTP 状态码：400 无效参数，404 不存在或已禁用，409 AppID 冲突，503 数据库不可用；不泄漏数据库错误。

健康检查：`GET /health/live`、`GET /health/ready`。

## 本地启动（PowerShell）

要求 Go 1.27.3、MySQL 8.0+，且共享库已由 `config-center` 完成最新版迁移。

```powershell
cd sdk-core
Copy-Item configs/config.yaml configs/config-dev.yaml
```

编辑 `configs/config-dev.yaml`，将 `database.dsn` 指向配置中心的 **同一数据库**（通常为 `config_center`）：

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
Invoke-RestMethod http://127.0.0.1:8082/api/core/prod/huawei_10001
```

可用 `SDK_CORE_DB_DSN`、`SDK_CORE_ADDR` 环境变量覆盖配置文件。含凭据的本地配置文件已经由仓库根目录 `.gitignore` 排除。

## 生产与集群

- 与 `config-center` 使用同一数据库和同一套表，不创建副本，也不执行 DDL。
- 建议单独创建 **只读 MySQL 用户**，避免 SDK 服务修改配置。
- Nginx 保留完整路径，将 `/api/core/` 转发到多个 `sdk-core` 实例。
- 查询使用只读 Repeatable Read 事务，保证渠道配置和绑定列表来自同一一致性快照。
- 注意目前数据库唯一索引为 `(game_id, code)`，不是全局 Code 唯一；同 Code 跨游戏时接口明确返回 409，需要先清理数据再考虑全局唯一索引。
- **安全：** 这是可被客户端访问的配置接口。不得在 `config_channel_app.config` 存放会通过此接口泄漏的渠道登录密钥、支付私钥、签名密钥、数据库密码。后续应将私有支付配置单独保存到服务端专用字段/表。

Nginx 示例：

```nginx
upstream sdk_core {
    server 127.0.0.1:8082;
    server 127.0.0.1:8083;
}

location ^~ /api/core/ {
    proxy_pass http://sdk_core;
}
```

## 验证

```powershell
go test -race ./...
go vet ./...
go build ./cmd/sdk-core
```
