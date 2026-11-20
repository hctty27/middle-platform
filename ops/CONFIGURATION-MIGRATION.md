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
| SDK Core、SDK Service | `database.connMaxLifetime` | 30m |
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
| 所有模块 | `log.format`、`log.output`、`log.slowThreshold` | json、stdout、500ms |

## 清理的日志环境变量

移除 `LOG_FORMAT`、`LOG_OUTPUT`、`LOG_SLOW_THRESHOLD`，以及旧采样开关 `LOG_SUCCESS_SAMPLE_EVERY`、`LOG_SDK_CONFIG_SAMPLE_EVERY`、`LOG_SDK_EVENT_SAMPLE_EVERY`、`LOG_PLATFORM_SUCCESS_SAMPLE_EVERY`。保留 `LOG_LEVEL` 与 `LOG_ACCESS_ENABLED`。

## 仍可调节的关键字段

- 数据库：`database.dsn`、`database.maxOpenConns`（Center / Core / Service）；默认最大开放连接数分别为 20 / 30 / 10。Core / Service 可调 `database.maxIdleConns`（默认 10 / 5），大于最大开放数时自动收缩；Center 空闲连接数固定为 min(10, maxOpenConns)。连接生命周期统一 30m
- Core：`adminAuth.secret`、`orderNotify.workers`、`orderNotify.hostConcurrency`、`configCache.ttl`、`configCache.maxEntries`、`configCache.maxInflight`
- Service：`game.timeout`、`configCache.ttl`、`configCache.maxEntries`、`configCache.maxInflight`、`platform.eventReportURL`、`platform.subscribeMessageURL`、`platform.userCheckURL`、`platform.maxIdlePerHost`、`platform.eventMaxConns`
- 日志：`log.level`、`log.accessEnabled`
- 保留原有 `server.address` / 监听环境变量及配置中心、接入端的业务必需字段。Config Center Access 的 `sync.*` 仍然支持自定义，但示例省略默认值。

### 示例 YAML

参考各服务 `configs/config.yaml`。由于 YAML 解析启用了 `KnownFields(true)`，被删除的字段只要残留就会报错，**即使写的是旧默认值也不例外**。

## 数据库连接池默认值调整（后续版本）

- Config Center 现在支持 `database.maxOpenConns`，默认 20。省略时沿用 20，不需要为旧版 YAML 增加字段；如果要调节，需更新服务器上的独立配置文件。
- SDK Core 默认 `database.maxOpenConns` 从 20 调整为 30，但**服务器 YAML 中若显式写着 20，依然会使用 20**。要采用 30，需将生产 `sdk-core.yaml` 对应字段改为 30 或删除该字段使用新版默认值。
- SDK Service 默认仍为 10；三个模块固定的连接最大生命周期均为 30 分钟。
- 上述为连接数上限，不代表启动时会主动建立对应数量的 MySQL 连接。部署后按数据库 `max_connections`、连接等待、慢查询和实例数量核验整体预算。

## 恢复可调性能参数和默认缓存 TTL 为 5s

- SDK Core、SDK Service 重新允许从 YAML 配置 `database.maxIdleConns`、`configCache.maxEntries`、`configCache.maxInflight`，SDK Service 另恢复 `platform.maxIdlePerHost`、`platform.eventMaxConns`。默认分别与原代码固定值一致（Core 空闲池 10 / Service 5、缓存容量 4096、并发回源 8、平台空闲 32、事件连接不限），升级这些项本身不要求数据库迁移。
- **两个 SDK 服务在 YAML 未显式填写 `configCache.ttl` 时，默认均为 5s。** 原来 Core 默认为 1s，Service 默认为 0s；这是有意识的行为变化。生产独立 YAML 若仍显式设置 `ttl: 1s` / `ttl: 0s`，会继续覆盖新版默认值。请在每台服务器上调整为 `ttl: 5s` 或删除该字段才能使用新的默认值。
- **缓存安全告警：** SDK Service 的路由、验签密钥、服务停用和用户核验开关会缓存成功结果；修改后至少考虑 TTL 和在途请求的延迟生效。需要立即停用/撤销的业务可显式将 `ttl` 改为 `0s` 并重启相关服务实例，或补充主动失效机制，不能认为 5s 是严格的最大撤销时限。
- 旧版本若尚不识别恢复的 YAML 字段，请先部署支持这些字段的新二进制，再向独立 YAML 增加可调参数；保留旧字段的恢复版本同样必须遵守严格 YAML 字段校验。


## SDK 管理接口独立服务迁移（2026-11-20）

- SDK Core 不再接受 `adminAuth`；从 `config/sdk-core.yaml` 移除该段。
- 新建 `config/sdk-admin.yaml`，配置 `database.dsn`、`database.maxOpenConns`、`database.maxIdleConns`、`adminAuth.secret`、日志级别。与 `config-center` 共用管理密钥。
- 原 `/api/core/admin/**` 已删除；改用 `/api/sdk/admin/**`，前端和客户端必须使用新路径重新计算请求 HMAC。
- 发布前手动安装 Supervisor、Nginx 及 upstream 运行配置；先启用 sdk-admin 的 Ready，再切换 Vue 前端和删除旧管理入口。
