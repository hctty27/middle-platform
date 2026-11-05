# HTTP API 契约

本文件仅维护内部 HTTP 路由索引。对外请求字段、签名规则与代码示例以 [统一接口文档](index.html) 为准。本地默认监听：配置中心 `:8080`、接入端 `:8081`、SDK Core `:8082`、SDK Service `:8221`。生产按 Nginx 转发规则调整。

## 1. 配置中心（config-center）

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| GET | `/api/config/admin/nodes` | 列出完整三级节点 |
| GET | `/api/config/admin/nodes/tree?parentId=0` | 懒加载目录树 |
| GET | `/api/config/admin/nodes/children?parentId=...&page=1&size=20&keyword=...&envType=...` | 直接子节点分页 |
| GET | `/api/config/admin/nodes/{id}/children` | 通过路径指定父节点的分页子项 |
| GET | `/api/config/admin/nodes/search?keyword=...&limit=30` | 搜索节点及祖先路径 |
| GET | `/api/config/admin/nodes/{id}/path` | 节点祖先路径 |
| GET | `/api/config/admin/nodes/{id}` | 单节点查询 |
| POST / PUT / DELETE | `/api/config/admin/nodes`、`/api/config/admin/nodes/{id}` | 创建 / 修改 / 删除节点 |
| GET / POST | `/api/config/admin/subscribers` | 查询 / 注册 Webhook 订阅者 |
| GET / POST | `/api/core/admin/channel-apps` | 按 `gameId` 查询 / 创建渠道应用 |
| PUT / DELETE | `/api/config/admin/channel-apps/{id}` | 修改 / 删除渠道应用 |
| GET | `/api/config/admin/channel-apps/server-groups?gameId=...&envType=...&keyword=...` | 绑定区服组候选项 |
| GET | `/api/core/admin/server-groups/{id}/channel-apps` | 查询绑定到区服组的应用 |
| GET | `/api/config/nodes/{id}` | 同步侧单节点查询 |
| POST | `/api/config/nodes/batch-get` | 批量查询节点，JSON 请求 `{"ids":[1,2]}`，最多 500 个 |
| GET | `/api/config/sync/revision` | 当前 `epoch` 与 `revision` |
| GET | `/api/config/sync/changes?afterRevision=0&limit=200` | 变更记录，`limit` 最大 500 |
| GET | `/api/config/sync/snapshot` | 一致性三级节点快照 |

管理写路由始终注册。管理 API 没有内建登录与 RBAC，必须由私网或认证网关限制访问。

创建节点示例（`config` 为 JSON 字符串）：

```http
POST /api/config/admin/nodes
Content-Type: application/json

{"parentId":0,"nodeType":"game","code":"game_demo","name":"示例游戏","config":"{}"}
```

大区类型由 `envType` 指定 `prod`、`test` 或 `review`。`config_channel_app` 的请求包括 `gameId`、`code`、`config` 和 `prodServerGroupId`、`testServerGroupId`、`reviewServerGroupId`；三个绑定字段可为空，且每个环境至多一个绑定。

增量接口示例：

```http
GET /api/config/sync/revision
```

```json
{"epoch":"00000000-0000-0000-0000-000000000001","revision":12}
```

`epoch` 示例值仅为格式示意。查询 `changes` 时必须验证 revision 连续性；断档时拉取 `snapshot`，不能自行跳过事件。

## 2. 接入端（config-center-access）

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| GET | `/api/access/nodes` | 查询本地同步的三级节点 |
| GET | `/api/access/revision` | 查询本地同步游标 |
| POST | `/internal/config/callback` | 接收签名通知并异步触发同步 |

Webhook JSON 只包括 `epoch`、`revision`；请求必须携带 `X-Config-Timestamp`（Unix 秒）与 `X-Config-Signature`（`sha256=<hex>`）。签名输入为 `timestamp + "." + epoch + "." + revision`，密钥为订阅者配置的 HMAC secret。服务端要求时间戳在前后五分钟内。验签成功返回 HTTP 202，失败返回 401；**不要直接将回调接口暴露公网**。

接入端只存储 `game`、`region`、`server_group`，不包含 `channel_app`，不承诺渠道应用配置的同步。

## 3. SDK Core

当前业务路由如下（不保留旧路径兼容）：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/core/config/{env}/{appid}` | 查询 SDK 配置 |
| POST | `/api/core/orders/{env}/{appid}` | 游戏登记订单 |
| POST | `/api/core/pay/notify/{appid}` | 第三方平台支付回调（无 env） |

```http
GET /api/core/config/{env}/{appid}
```

- `env` 只能是 `prod`、`test`、`review`；`appid` 是 `config_channel_app.code`。
- AppID 未找到、环境未绑定或目标节点停用时返回 HTTP 404；无效参数为 400。
- 成功时响应外壳固定 `code` / `message` / `data`，`data` 的字段由内置映射决定。

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

当前映射规则来自 `sdk-core/internal/mapping/response-mapping.json`，按接口维度全局配置，不因环境而变化。支持 `channel_app`、`game`、`region`、`server_group` 的普通字段与 `config.xxx` 路径，JSON **按映射依赖解析**。不存在或为 null 的映射值会被省略，`false`、`0`、空字符串仍保留；修改映射必须重新构建并发布，添加 JSON 规则**不会自动注册新路由**。

SDK Core 错误外壳同样带 `code` 与 `message`；典型 HTTP 状态：400 参数无效、404 配置不存在、409 AppID 冲突、500 映射引用的配置无效、503 数据库服务不可用。具体契约及安全限制见 [SDK Core 模块说明](../sdk-core/README.md)。

## 4. SDK Service 事件上报与订阅消息推送

```http
POST /api/service/events/{env}
Content-Type: application/json
x-app-id: 110006
x-timestamp: <Unix 秒>
x-nonce: <16 位小写十六进制>
x-signature: <40 位小写 SHA-1>
```

```json
{"event":"login","ext":{"roleId":"10001"}}
```

只支持已绑定环境的渠道包。SDK Service 从 Header 读取 AppID，使用所属游戏密钥验签；全局与渠道环境的 `event_report` 开关同时开启后才转发平台。平台请求使用渠道包 `appSecret` 重新签名，签名不包含 Body。当前不落库也不自动重试事件。旧的 `/api/core/events/{env}` 已删除。

订阅消息推送接口：`POST /api/service/subscribe/{env}`，Body：`{"open_id":"u1","tpl_id":"template","data":"{}"}`；`open_id`、`tpl_id` 必填字符串，`data`、`ext` 可选字符串（`data` 非空时必须是 JSON 对象文本）。使用相同的游戏请求验签与平台 Header 签名机制，功能编码为 `subscribe_message`，平台调用地址来自 `sdk-service` 的 `platform.subscribeMessageURL`。

事件和订阅成功均返回 `{"code":0,"message":"ok","data":true}`，表示平台已接收；失败返回对应 HTTP 4xx/5xx。不开启重试、异步处理或数据存储。

## 5. SDK 功能管理（config-center）

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/core/admin/services` | 功能目录及全局开关 |
| PUT | `/api/config/admin/sdk-services/{id}` | 修改全局开关 |
| GET | `/api/config/admin/channel-apps/{id}/services?env=test` | 查看渠道包指定环境的服务配置 |
| PUT | `/api/config/admin/channel-apps/{id}/services/{serviceId}?env=test` | 保存渠道包指定环境的服务配置 |

目前实现了 `event_report`、`user_check`、`role_query`、`server_query`、`role_control`、`subscribe_message`。功能必须同时通过全局开关、渠道包环境开关，未配置的绑定默认关闭。订单登记、充值回调和发货不受上述开关控制。

## 6. SDK Service 用户核验

`POST /api/service/user/check/{env}`，AppID 来自 `x-app-id`，Body 仅含 `open_id`。
根据游戏配置 `serviceAdapters.user_check` 选择 Go 适配器；游戏接口 URL 从大区 `userCheckUrl` 获取。
成功 `{"code":0,"message":"ok","data":true}`；用户不存在 `{"code":1021003,"message":"用户信息不存在","data":null}`。
游戏端与平台端请求详见 [游戏接口 HTML](index.html#g-start)、[平台接口 HTML](index.html#p-start)。平台核验接口签名样本与环境回调能力仍需联调确认。

## 7. SDK Service 角色查询

`POST /api/service/role/query/{env}`，AppID 来自 `x-app-id`，Body 可包含 `open_id`、`role_id`、`original_server_id`、`current_server_id`、`extension`，至少提供一项身份查询条件。
游戏接口 URL 从大区 `roleQueryUrl` 获取；未指定 `serviceAdapters.role_query` 时使用标准适配器。
成功返回 `{"code":0,"message":"ok","data":[...]}`，无角色返回空数组。
该服务要求全局和渠道环境开关同时开启，签名使用 Header 四项加 Body 顶层非空值。

## 8. SDK Service 区服查询

`POST /api/service/server/query/{env}`，AppID 来自 `x-app-id`，Body 支持 `original_server_id`、`current_server_id`、`page`、`page_size`、`zonekey`。其中 `page` 必填且从 1 开始，`page_size` 默认 1000、最大 1000；数字字段必须为 JSON 整数。
游戏区服地址配置在大区 `serverQueryUrl`；未配置 `serviceAdapters.server_query` 时使用标准适配器。
成功响应 `{"code":0,"message":"ok","data":{"total":0,"list":[]}}`；要求全局与渠道环境的 `server_query` 开关均启用。
签名规则为 Header 四项与 Body 顶层所有非空值，数值转换为字符串后排序计算 SHA-1；默认 `page_size` 若平台未提交，不参与平台入站签名，但转发游戏时显式携带并重新签名。

## 9. SDK Service 封禁与解禁

`POST /api/service/role/control/{env}`，AppID 由 `x-app-id` 指定。平台使用 `appSecret` 对 Header 字段和 Body 顶层所有非空值签名，中台使用 `gameSecret` 重新签名转发到 `region.config.roleControlUrl`。默认适配器为 `standard`，特殊游戏可在 `game.config.serviceAdapters.role_control` 指定已注册实现。

请求字段：`type`（`close` 封号、`unClose` 解封、`lock` 禁言、`unLock` 解禁言）、`server_id`、`open_id`、`role_id`、`role_name`、`ip`、`duration`、`describe`、`extension`。封号和禁言必须显式提供非负整数 `duration`；`0` 表示永久。至少提供一个身份目标字段（open_id/role_id/role_name/ip）。

成功响应为 `{"code":0,"message":"ok","data":null}`。游戏返回失败或网络错误不能当作成功，超时可能已执行，不自动重试。需同时开启全局和渠道环境的 `role_control` 开关；存量数据库先手动执行 `add_sdk_role_control.sql`。

## 10. 健康检查与响应约定

四个服务均有：

- `GET /health/live`：进程存活检查。
- `GET /health/ready`：依赖就绪检查（数据库不可用或停机时通常为 HTTP 503）。

配置中心与接入端的错误使用 `{"error":"..."}`；SDK Core 使用 `{"code":...,"message":"..."}`。不要假设所有模块共享一套错误外壳。API 的 `config` 字段为 JSON 文本字符串，和 SDK Core 最终输出的结构化 `data` 不同。

**公开范围：** 仅按实际业务需要开放 `/api/core/**`；管理、配置同步、接入查询及回调必须部署在可信网络或有独立鉴权的网关之后。

> 维护约定：本文仅保留路由、内部权限和实现索引。游戏和平台的字段、错误码与验签说明不在本文件重复维护。


## SDK 专属节点配置管理

新增 `GET /api/core/admin/node-configs/{id}` 与 `PUT /api/core/admin/node-configs/{id}`，请求体 `{"config":"{...}"}`；响应包含 `nodeId`、`nodeType`、`config`。SDK Core 持有管理写权限，SDK Service 仅使用只读数据。渠道包、功能开关管理统一归 `/api/core/admin/**`。旧 `/api/config/admin/channel-apps/**` 与 `/api/config/admin/sdk-services/**` 不再提供。

`/api/core/admin/**` 必须由受认证的管理网关或可信内网保护，不得经过面向公众的 SDK 网关开放。
