# sdk-service · 平台主动调用游戏

独立 Go 服务，直接访问 `config_center` 权威数据库（只读），当前仅实现 **用户核验**。平台请求由 SDK Service 校验后调用游戏适配器，不保存、不缓存核验结果，也不自动重试。

## 接口和适配器

- 平台入口：`POST /api/service/user/check/{env}`，AppID 由 `x-app-id` 提供，`env` 为 `prod/test/review`。
- 游戏地址：`region.config.userCheckUrl`，签名密钥：`game.config.gameSecret`。
- 平台验签密钥：`channel_app.config.appSecret`。
- `user_check` 的全局与渠道环境开关均需开启，未绑定默认关闭。
- **默认适配器无需配置**：`game.config.serviceAdapters.user_check` 为空或未设置时自动使用 `standard`；仅特殊游戏显式选择已注册适配器。
- 标准游戏结果要求 HTTP 200、`code=0`、`data.exists` 为布尔值；游戏故障不能映射为用户不存在。

对外参数和签名规则分别见 [平台 HTML](../docs/platform.html)、[游戏 HTML](../docs/game.html)。平台用户核验是否签 `open_id` 需要真实请求样本联调确认，正式接入前必须核准。

## 本地启动

```powershell
Copy-Item configs/config.yaml configs/config-dev.yaml
# 设置 database.dsn 指向 config_center
go run ./cmd/sdk-service -config configs/config-dev.yaml
```

默认监听 `:8221`，Nginx 转发 `/api/service/`，路由不包含 `/sdk`。服务运行时不会自动建表，需提前手动建立功能开关表并注册 `user_check`。

```bash
go test -race ./...
go vet ./...
go build ./cmd/sdk-service
```

代码扩展：`internal/adapter/usercheck` 注册具体游戏适配器，`internal/adapter/selection.go` 提供统一默认选择规则。生产配置见 [部署说明](../docs/production.md)。
