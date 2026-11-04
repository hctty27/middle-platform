# HTTP API 契约

以下路径直接对应当前三个服务的路由。服务默认分别监听 `:8080`、`:8081`、`:8082`，生产由 Nginx 按前缀转发；所有示例主机仅用于本地调试。

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
| GET / POST | `/api/config/admin/channel-apps` | 按 `gameId` 查询 / 创建渠道应用 |
| PUT / DELETE | `/api/config/admin/channel-apps/{id}` | 修改 / 删除渠道应用 |
| GET | `/api/config/admin/channel-apps/server-groups?gameId=...&envType=...&keyword=...` | 绑定区服组候选项 |
| GET | `/api/config/admin/server-groups/{id}/channel-apps` | 查询绑定到区服组的应用 |
| GET | `/api/config/nodes/{id}` | 同步侧单节点查询 |
| POST | `/api/config/nodes/batch-get` | 批量查询节点，JSON 请求 `{"ids":[1,2]}`，最多 500 个 |
| GET | `/api/config/sync/revision` | 当前 `epoch` 与 `revision` |
| GET | `/api/config/sync/changes?afterRevision=0&limit=200` | 变更记录，`limit` 最大 500 |
| GET | `/api/config/sync/snapshot` | 一致性三级节点快照 |

管理写路由只有在 `admin.writesEnabled=true` 时才注册；关闭时可能返回 404，而非业务级“无权限”。管理 API 无内建登录与 RBAC，必须从网关侧限制访问。

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
| POST | `/api/core/events/{env}` | 渠道包事件上报（AppID 在 x-app-id Header） |

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

## 4. 渠道包事件上报（SDK Core）

```http
POST /api/core/events/{env}
Content-Type: application/json
x-app-id: 110006
x-timestamp: <Unix 秒>
x-nonce: <16 位小写十六进制>
x-signature: <40 位小写 SHA-1>
```

```json
{"event":"login","ext":{"roleId":"10001"}}
```

只支持已绑定环境的渠道包。Core 从 Header 读取 AppID，使用所属游戏密钥验签；只有全局和渠道环境的 `event_report` 服务开关同时开启才同步转发至平台。平台请求使用渠道包 `appSecret` 重新签名。当前不保存或自动重试事件。详见 [事件上报接入说明](event-report-integration.md)。

## 5. 健康检查与响应约定

三个服务均有：

- `GET /health/live`：进程存活检查。
- `GET /health/ready`：依赖就绪检查（数据库不可用或停机时通常为 HTTP 503）。

配置中心与接入端的错误使用 `{"error":"..."}`；SDK Core 使用 `{"code":...,"message":"..."}`。不要假设所有模块共享一套错误外壳。API 的 `config` 字段为 JSON 文本字符串，和 SDK Core 最终输出的结构化 `data` 不同。

**公开范围：** 仅按实际业务需要开放 `/api/core/**`；管理、配置同步、接入查询及回调必须部署在可信网络或有独立鉴权的网关之后。
