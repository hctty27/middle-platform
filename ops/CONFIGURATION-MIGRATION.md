# 运行配置精简迁移说明

此改动只精简 SDK Core / SDK Service 对外可调节的技术参数及统一日志项，不删除缓存、异步通知、出站连接池或慢请求告警功能。生产配置文件是独立文件，不会随打包自动替换。

## 发布前操作（每台服务器）

1. 备份 `/data/middle-platform/config/` 下所有生产 YAML 和 Supervisor 中显式设置的环境变量。
2. 按照下表移除 **已有** 的废弃字段。保留真实数据库 DSN、管理 HMAC 密钥及平台 URL；绝对不要用仓库中的空模板覆盖生产秘密。
3. 配置文件可以先更新：旧版本在未显式配置这些参数时已使用相同默认值，因此先移除与默认值相同的配置不改变行为。如线上旧值曾被特意调整，需要先核对与此次固定值的差异；有差异则应单独验证再升级。
4. 全部实例 YAML 更新完成后，再使用现有滚动发布流程更新二进制。确认 `/health/ready`、异常/慢请求日志、数据库连接等待及异步回调正常。
5. 如果需要回退旧版本二进制，精简后的 YAML 仍能被旧版本按默认值读取。只有先前使用非默认值的功能可能需要恢复备份配置。

## 删除的 YAML 字段

| 模块 | 不再允许配置的字段 | 固定默认值 |
| --- | --- | --- |
| SDK Core、SDK Service | `database.maxIdleConns` | Core 10 / Service 5，上限随 `maxOpenConns` 向下收缩 |
| SDK Core、SDK Service | `database.connMaxLifetime` | 30m |
| SDK Core、SDK Service | `configCache.maxEntries` | 4096 |
| SDK Core、SDK Service | `configCache.maxInflight` | 8 |
| SDK Core、SDK Service | `configCache.loadTimeout` | 5s |
| SDK Core | `server.requestTimeout` | 5s |
| SDK Core | `orderNotify.idleInterval` | 500ms |
| SDK Core | `orderNotify.disableKeepAlives` | false |
| SDK Core | `orderNotify.maxIdleConns` | 32 |
| SDK Core | `orderNotify.maxIdleConnsPerHost` | 4 |
| SDK Core | `orderNotify.maxConnsPerHost` | 4 |
| SDK Core | `orderNotify.idleConnTimeout` | 30s |
| SDK Service | `server.requestTimeout` | 自动计算 `max(game.timeout, 5s) + 3s` |
| SDK Service | `platform.isolateConnections` | true |
| SDK Service | `platform.maxIdlePerHost` | 32 |
| SDK Service | `platform.eventMaxConns` | 0 |
| 所有模块 | `log.format`、`log.output`、`log.slowThreshold` | json、stdout、500ms |

## 清理的日志环境变量

移除 `LOG_FORMAT`、`LOG_OUTPUT`、`LOG_SLOW_THRESHOLD`，以及旧采样开关 `LOG_SUCCESS_SAMPLE_EVERY`、`LOG_SDK_CONFIG_SAMPLE_EVERY`、`LOG_SDK_EVENT_SAMPLE_EVERY`、`LOG_PLATFORM_SUCCESS_SAMPLE_EVERY`。保留 `LOG_LEVEL` 与 `LOG_ACCESS_ENABLED`。

## 仍可调节的关键字段

- 数据库：`database.dsn`、`database.maxOpenConns`（Core / Service）；最大空闲连接数使用固定默认值与最大开放连接数的较小者
- Core：`adminAuth.secret`、`orderNotify.workers`、`orderNotify.hostConcurrency`、`configCache.ttl`
- Service：`game.timeout`、`configCache.ttl`、`platform.eventReportURL`、`platform.subscribeMessageURL`、`platform.userCheckURL`
- 日志：`log.level`、`log.accessEnabled`
- 保留原有 `server.address` / 监听环境变量及配置中心、接入端的业务必需字段。Config Center Access 的 `sync.*` 仍然支持自定义，但示例省略默认值。

### 示例 YAML

参考各服务 `configs/config.yaml`。由于 YAML 解析启用了 `KnownFields(true)`，被删除的字段只要残留就会报错，**即使写的是旧默认值也不例外**。
