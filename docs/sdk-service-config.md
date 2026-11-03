# SDK 服务开关配置

## 设计范围

提供 SDK 全局和渠道环境开关。当前已接入渠道包事件上报；角色查询等其他业务仍按需实现。不影响订单登记、支付回调与发货补偿。

- `sdk_service`：服务目录，唯一编码 `code`，全局总开关 `enabled`。
- `sdk_service_channel`：通过 `channel_app_id` 关联 `config_channel_app.id`，按 `prod/test/review` 保存 `enabled` 和独立 `config` JSON 对象。
- 未创建渠道服务记录时，默认关闭；全局禁用时，无论渠道开关状态如何，服务不可用。
- `SDK Core` 的服务判断还要求渠道包、所属游戏、大区、区服组处于启用状态且环境绑定有效。
- 已确认支付订单的补偿不受服务开关控制。当前不把 `payment` 注册为可关闭服务。

## 手动执行 SQL

现有 `config_center` 数据库先备份，再由管理员手动执行：

```bash
mysql -u <user> -p config_center < config-center/db/sql/add_sdk_services.sql
```

新数据库使用 `config-center/db/sql/initialize.sql`，其中已包含两张表和内置服务目录。两个 SQL 是二选一，不可同时执行。运行时绝不自动执行 DDL；配置中心启动前会校验新增表结构。

## 管理 API

所有接口位于配置中心管理路由，必须由网关访问控制保护，禁止未经认证向公网开放。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/config/admin/sdk-services` | 查询全局服务目录 |
| PUT | `/api/config/admin/sdk-services/{id}` | 修改全局开关，Body `{"enabled":true}` |
| GET | `/api/config/admin/channel-apps/{id}/services?env=test` | 查询渠道包某环境服务配置 |
| PUT | `/api/config/admin/channel-apps/{id}/services/{serviceId}?env=test` | 修改渠道包功能，Body `{"enabled":true,"config":{}}` |

全局服务目录通过初始化 SQL 维护，不提供在管理页面随意创建未知代码的能力；数据库新增服务编码并不意味着 Go 自动支持业务路由。

## 页面

- 左侧导航的「服务管理」：全局目录和总开关。
- 「渠道应用」详情的「服务配置」：按测试、正式、提审环境维护对应开关与 JSON 扩展配置。

## 渠道包事件上报

事件上报入口为 `POST /api/core/{env}/events`，通过请求头 `x-app-id` 识别渠道包。游戏使用所属游戏 `gameSecret` 验签，Core 使用渠道包 `appSecret` 重新生成平台签名，向固定的 `/report/event` 同步转发。

**只有两层开关**：`sdk_service.code=event_report` 全局开启，且 `sdk_service_channel` 对应渠道包及环境的记录 `enabled=1` 时才转发；未设置记录时默认关闭。

事件上报目前不读取 `sdk_service_channel.config` 的任何规则；页面不展示事件级配置，保存开关时配置为 `{}`。不保存事件、不启动异步队列或自动补偿，由游戏服根据结果决定是否重试。

详细请求参数与响应见 [渠道事件上报接入](event-report-integration.md)。

## 上线顺序

1. 备份数据库，手动执行 SQL 并核对新增表、索引。
2. 更新配置中心，确认启动校验通过，GET 管理接口正常。
3. 更新管理前端，配置各环境开关。
4. 在 SDK Core 的 YAML 中填写平台事件地址并更新 SDK Core；通过测试环境渠道进行联调。

`sdk_service_channel.config` 不应存放客户端可读的密钥。需要审计全局开关变更时，应在后续管理审计模块中增加操作审计。
