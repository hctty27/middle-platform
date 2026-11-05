# sdk-service · 平台主动调用游戏

独立 Go 服务，直接访问 `config_center` 权威数据库（只读），当前实现 **用户核验**、**角色查询**、**区服查询**、**封禁与解禁**、**事件上报**、**订阅消息推送**。平台主动请求由 SDK Service 调用游戏适配器；游戏主动上报由 SDK Service 验签并转发平台。业务请求不落库、不自动重试。

## 接口和适配器

- 用户核验平台入口：`POST /api/service/user/check/{env}`；角色查询平台入口：`POST /api/service/role/query/{env}`；区服查询平台入口：`POST /api/service/server/query/{env}`；封解禁平台入口：`POST /api/service/role/control/{env}`。AppID 由 `x-app-id` 提供，`env` 为 `prod/test/review`。
- 游戏主动上报入口：`POST /api/service/events/{env}`，订阅消息入口：`POST /api/service/subscribe/{env}`。两者的 AppID 均来自 `x-app-id`，不再使用旧的 `/api/core/events/{env}`。
- 用户核验地址：`region.config.userCheckUrl`；角色查询地址：`region.config.roleQueryUrl`；区服查询地址：`region.config.serverQueryUrl`；封解禁地址：`region.config.roleControlUrl`，签名密钥：`game.config.gameSecret`。
- 平台验签密钥：`channel_app.config.appSecret`。
- `user_check`、`role_query`、`server_query`、`role_control`、`event_report` 和 `subscribe_message` 的全局与渠道环境开关均需开启，未绑定默认关闭。
- **默认适配器无需配置**：`game.config.serviceAdapters.user_check` 、`game.config.serviceAdapters.role_query` 、`game.config.serviceAdapters.server_query` 和 `game.config.serviceAdapters.role_control` 为空或未设置时自动使用 `standard`；仅特殊游戏显式选择已注册适配器。
- 用户核验标准游戏结果要求 HTTP 200、`code=0`、`data.exists` 为布尔值；角色查询标准结果要求 HTTP 200、`code=0`、`data` 为角色数组。游戏故障不能映射为空结果。
- 角色查询支持 `open_id`、`role_id`、`original_server_id`、`current_server_id`、`extension`，至少填写一个身份查询条件。请求 Body 顶层所有非空值参与签名。
- 角色查询的标准适配器位于 `internal/adapter/rolequery`；新增特殊游戏实现并注册后，即可通过游戏级 `serviceAdapters.role_query` 选择。
- 事件上报请求为 `{"event":"login","ext":{}}`；订阅消息使用 `open_id`、`tpl_id`、可选字符串 `data` 与 `ext`，其中 `data` 为包含 JSON 对象文本的字符串。游戏请求由 `gameSecret` 加 Header 与 Body 顶层值验签；平台请求改用 `appSecret` 只签 Header 四值。目标 URL 在 `sdk-service/configs/config.yaml` 的 `platform.eventReportURL` / `platform.subscribeMessageURL` 中配置。成功响应 `{"code":0,"message":"ok","data":true}`，同步转发、5 秒超时、不落库、不自动重试。
- 封解禁支持 `close`（封号）、`unClose`（解封）、`lock`（禁言）、`unLock`（解禁言）；封号、禁言必须提交非负整数 `duration`（单位秒，0 永久），至少一个身份目标字段。标准游戏结果需 HTTP 200、`code=0`、`message` 字符串、`data:null`，不自动重试。
- 区服查询支持 `original_server_id`、`current_server_id`、`page`、`page_size`、`zonekey`；`page` 必填，`page_size` 缺省时按 1000 查询，上限 1000。标准结果为 `data.total` 与 `data.list`，可通过 `serviceAdapters.server_query` 选择特殊适配器。

对外参数和签名规则分别见 [平台 HTML](../docs/index.html#p-start)、[游戏 HTML](../docs/index.html#g-start)。平台用户核验是否签 `open_id` 需要真实请求样本联调确认，正式接入前必须核准。

## 本地启动

```powershell
Copy-Item configs/config.yaml configs/config-dev.yaml
# 设置 database.dsn 指向 config_center
go run ./cmd/sdk-service -config configs/config-dev.yaml
```

默认监听 `:8221`，Nginx 转发 `/api/service/`，路由不包含 `/sdk`。服务运行时不会自动建表，需提前手动建立功能开关表并注册 `user_check`、`role_query`、`server_query`、`role_control`、`event_report`、`subscribe_message`。

```bash
go test -race ./...
go vet ./...
go build ./cmd/sdk-service
```

代码扩展：`internal/adapter/usercheck`、`internal/adapter/rolequery`、`internal/adapter/serverquery`、`internal/adapter/rolecontrol` 分别注册对应功能的游戏适配器，`internal/adapter/selection.go` 提供统一默认选择规则。生产配置见 [部署说明](../docs/production.md)。

## 事件迁移上线顺序

1. 已有库先手工执行 `config-center/db/sql/add_sdk_subscribe_message.sql`；`event_report` 已存在，不要重复插入。保留已有渠道环境绑定。
2. 将原 `sdk-core` 的 `platform.eventReportURL` 转移到 `sdk-service`，设置 `platform.subscribeMessageURL`，然后从 Core YAML 删除整段 `platform`。
3. 配置 Nginx `/api/service/` 指向 SDK Service，确保两类新路径可达，部署 Core 与 Service 新二进制。切换游戏端上报 URL 到 `/api/service/events/{env}`，订阅推送使用 `/api/service/subscribe/{env}`。
4. 旧 Core 事件接口已删除，调用将返回 404。避免在存在旧 Core 实例时向其继续投递事件。

`sdk-service` 启动会检查服务目录，手动 SQL 必须先于新版本启动执行。
