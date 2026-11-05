# sdk-service · 平台主动调用游戏

独立 Go 服务，直接访问 `config_center` 权威数据库（只读），当前实现 **用户核验**、**角色查询**、**区服查询**、**封禁与解禁**。平台请求由 SDK Service 校验后调用游戏适配器，不保存业务操作记录，也不自动重试。

## 接口和适配器

- 用户核验平台入口：`POST /api/service/user/check/{env}`；角色查询平台入口：`POST /api/service/role/query/{env}`；区服查询平台入口：`POST /api/service/server/query/{env}`；封解禁平台入口：`POST /api/service/role/control/{env}`。AppID 由 `x-app-id` 提供，`env` 为 `prod/test/review`。
- 用户核验地址：`region.config.userCheckUrl`；角色查询地址：`region.config.roleQueryUrl`；区服查询地址：`region.config.serverQueryUrl`；封解禁地址：`region.config.roleControlUrl`，签名密钥：`game.config.gameSecret`。
- 平台验签密钥：`channel_app.config.appSecret`。
- `user_check`、`role_query`、`server_query` 和 `role_control` 的全局与渠道环境开关均需开启，未绑定默认关闭。
- **默认适配器无需配置**：`game.config.serviceAdapters.user_check` 、`game.config.serviceAdapters.role_query` 、`game.config.serviceAdapters.server_query` 和 `game.config.serviceAdapters.role_control` 为空或未设置时自动使用 `standard`；仅特殊游戏显式选择已注册适配器。
- 用户核验标准游戏结果要求 HTTP 200、`code=0`、`data.exists` 为布尔值；角色查询标准结果要求 HTTP 200、`code=0`、`data` 为角色数组。游戏故障不能映射为空结果。
- 角色查询支持 `open_id`、`role_id`、`original_server_id`、`current_server_id`、`extension`，至少填写一个身份查询条件。请求 Body 顶层所有非空值参与签名。
- 角色查询的标准适配器位于 `internal/adapter/rolequery`；新增特殊游戏实现并注册后，即可通过游戏级 `serviceAdapters.role_query` 选择。
- 封解禁支持 `close`（封号）、`unClose`（解封）、`lock`（禁言）、`unLock`（解禁言）；封号、禁言必须提交非负整数 `duration`（单位秒，0 永久），至少一个身份目标字段。标准游戏结果需 HTTP 200、`code=0`、`message` 字符串、`data:null`，不自动重试。
- 区服查询支持 `original_server_id`、`current_server_id`、`page`、`page_size`、`zonekey`；`page` 必填，`page_size` 缺省时按 1000 查询，上限 1000。标准结果为 `data.total` 与 `data.list`，可通过 `serviceAdapters.server_query` 选择特殊适配器。

对外参数和签名规则分别见 [平台 HTML](../docs/index.html#p-start)、[游戏 HTML](../docs/index.html#g-start)。平台用户核验是否签 `open_id` 需要真实请求样本联调确认，正式接入前必须核准。

## 本地启动

```powershell
Copy-Item configs/config.yaml configs/config-dev.yaml
# 设置 database.dsn 指向 config_center
go run ./cmd/sdk-service -config configs/config-dev.yaml
```

默认监听 `:8221`，Nginx 转发 `/api/service/`，路由不包含 `/sdk`。服务运行时不会自动建表，需提前手动建立功能开关表并注册 `user_check`、`role_query`、`server_query`、`role_control`。

```bash
go test -race ./...
go vet ./...
go build ./cmd/sdk-service
```

代码扩展：`internal/adapter/usercheck`、`internal/adapter/rolequery`、`internal/adapter/serverquery`、`internal/adapter/rolecontrol` 分别注册对应功能的游戏适配器，`internal/adapter/selection.go` 提供统一默认选择规则。生产配置见 [部署说明](../docs/production.md)。
