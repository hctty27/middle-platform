# sdk-service · 平台主动调用游戏

独立 Go 进程，直接读取现有 `config_center` 数据库（只读）。首期仅支持用户核验，用户信息不落库、不缓存，也不自动重试。

## 接入入口

```http
POST /api/service/user/check/{env}
Content-Type: application/json
x-app-id: 110006
x-timestamp: <Unix 秒>
x-nonce: <16 位小写十六进制>
x-signature: <40 位小写 SHA-1>
```

请求 Body：`{"open_id":"user123"}`，环境限定 `prod/test/review`。
成功返回 `{"code":0,"message":"ok","data":true}`，用户不存在返回 `{"code":1021003,"message":"用户信息不存在","data":null}`。
游戏故障与用户不存在严格区分。

平台主动请求签名**暂按** `appSecret + appId + timestamp + nonce + open_id` 值排序、`_` 拼接、SHA-1 小写校验；平台文档没有单独明确用户核验的 Body 是否签名，**必须先用平台真实请求向量联调确认，未确认前不要开放正式流量**。

## 配置

- `game.config.gameSecret`：标准游戏适配器的签名密钥。
- `game.config.serviceAdapters.user_check`：注册的适配器名称，首版为 `standard`。
- `region.config.userCheckUrl`：该环境游戏核验的完整 HTTP/HTTPS 地址。
- `channel_app.config.appSecret`：平台请求验签密钥。
- `sdk_service + sdk_service_channel`：全局 + 渠道环境两级开关，未绑定默认关闭。

扩展不同游戏：在 `internal/adapter/usercheck` 编写实现并注册，在游戏配置中选择名称即可。适配器只负责游戏请求构建、认证与响应转换；平台响应由统一 HTTP 层生成。不引入动态映射引擎。

## 标准游戏侧协议

```http
POST {region.config.userCheckUrl}
Content-Type: application/json
x-app-id: 110006
x-timestamp: <Unix 秒>
x-nonce: <16 位小写十六进制>
x-signature: <SHA-1 小写十六进制>
```

请求：`{"open_id":"user123"}`

响应：`{"code":0,"data":{"exists":true}}`（不存在时 `exists=false`）。
游戏侧签名沿用 SDK Core 规则：`gameSecret + AppID + timestamp + nonce + open_id` 按值排序 SHA-1。
标准实现严格要求 HTTP 200、`code=0`、`data.exists` 为 bool，拒绝重定向、超时及非法响应。

## 开发和上线

1. 在已有库上**手动**执行 `config-center/db/sql/add_sdk_user_check.sql`；先确认 SDK 服务表已经存在。程序不自动运行 DDL。
2. 在配置中心启用全局 `user_check`，并在对应渠道包环境启用服务。
3. 配置游戏的适配器和大区核验地址，使用只读账号连接权威库。
4. `cp configs/config.yaml configs/config-dev.yaml`，设置 `database.dsn`。也支持环境变量 `SDK_SERVICE_DB_DSN` 和 `SDK_SERVICE_ADDR`。
5. `go test ./...`、`go vet ./...`、`go run ./cmd/sdk-service -config configs/config-dev.yaml`。
6. Nginx 转发 `/api/service/` 到本机 `8221`，不附加 `/sdk` 前缀；生产由 Supervisor 管理。

需要配置网络访问控制，保护包含密钥的管理/同步 API。核验接口不可让调用方指定转发 URL；URL 必须来自可信配置中心。首版不支持平台按一个 AppID 动态推断 `env`，各环境回调地址需要平台分别配置或采用隔离的部署入口。
