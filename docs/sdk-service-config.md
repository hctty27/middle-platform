# SDK 服务开关配置

## 设计范围

仅增加 SDK 服务定义和按渠道包、环境的功能配置，不实现事件上报、角色查询等具体业务接口，不影响已有订单登记、支付回调与发货补偿。

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

## 事件上报预留配置

只有实现事件上报业务后，下列配置才有运行时效果：

```json
{
  "defaultMode": "direct",
  "rules": {
    "login": "direct",
    "role_create": "durable",
    "chat": "direct"
  }
}
```

`direct` 代表不持久化、直接转发；`durable` 代表持久化后异步转发。当前仅存储 JSON 配置，不建事件表、不启动事件 Worker。

## 上线顺序

1. 备份数据库，手动执行 SQL 并核对新增表、索引。
2. 更新配置中心，确认启动校验通过，GET 管理接口正常。
3. 更新管理前端，配置各环境开关。
4. 未来接入 SDK Core 业务路由时再调用 `LookupSDKService` 执行控制。

`sdk_service_channel.config` 不应存放客户端可读的密钥。需要审计全局开关变更时，应在后续管理审计模块中增加操作审计。
