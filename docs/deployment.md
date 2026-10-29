# 本地部署与调试

## 单 MySQL

```bash
cd deploy
docker compose up -d
```

开发环境默认 root 密码仅供本机测试。生产中应使用独立账号和强密码、数据库备份。

## 启动服务

```bash
export CONFIG_DB_URL='jdbc:mysql://127.0.0.1:3306/config_center?connectionTimeZone=UTC'
export CONFIG_DB_USER=root
export CONFIG_DB_PASSWORD=local-dev-only
gradle :config-center:bootRun
```

## 启动接入服务

```bash
export ACCESS_DB_URL='jdbc:mysql://127.0.0.1:3306/config_access?connectionTimeZone=UTC'
export ACCESS_DB_USER=root
export ACCESS_DB_PASSWORD=local-dev-only
export CONFIG_CENTER_URL=http://127.0.0.1:8080
export CONFIG_WEBHOOK_SECRET='dev-signing-secret'
gradle :config-center-access:bootRun
```

创建订阅者（仅允许 localhost 回调地址，生产需要配置 `config.webhook.allowed-hosts`）：

```bash
curl -X POST http://127.0.0.1:8080/api/config/admin/subscribers \
  -H 'Content-Type: application/json' \
  -d '{"code":"access","callbackUrl":"http://127.0.0.1:8081/internal/config/callback","secret":"dev-signing-secret"}'
```

查看接入副本：`http://127.0.0.1:8081/api/access/nodes`；查看当前游标：`http://127.0.0.1:8081/api/access/revision`。

## 管理界面

```bash
cd config-center-admin
npm install
npm run dev
```

仅供可信网络访问。生产部署将 `/api/config/admin/**`限制到受信任的网关来源，不允许匿名公网访问。由于没有登录/权限系统，网关网络 ACL 即为管理接口的唯一访问控制边界。生产的 Webhook 需要 HTTPS、受控 DNS/目标地址、密钥管理与监控。

## 客户端远程请求超时

接入服务 默认连接超时为 **2000ms**、单次请求读取超时为 **15000ms**，可按内网状况覆盖：

```yaml
config:
  client:
    connect-timeout-ms: 2000
    read-timeout-ms: 15000
```

或者使用 接入工程 对应的环境变量 `CONFIG_CLIENT_CONNECT_TIMEOUT_MS`、`CONFIG_CLIENT_READ_TIMEOUT_MS`。
两个值必须在 1–120000ms 范围内，设置为 0 不允许（避免无期限等待或立即超时）。
配置中心不可达或请求超时后，同步任务会失败且不会推进本地 revision；
定时校对将再次尝试，Webhook 投递也会按现有策略重试。
注意服务端 Webhook 默认请求超时为 5 秒，完整的接入端同步（尤其大数据量首次快照）可能超过该时限，导致重复通知；重复通知须保持幂等，生产环境按实际耗时配置该超时或进一步采用异步触发方案。


## 统一无版本接口前缀与 Nginx

生产环境统一使用 `/api/config/**` 访问配置中心。Nginx 仅按服务前缀转发，`proxy_pass` 不带 URI 后缀，完整保留请求路径：

```nginx
location ^~ /api/config/ {
    proxy_pass http://config_center;
}
```

接入服务的 `CONFIG_CENTER_URL` 只配置服务根 URL，例如 `http://config-center:8080`，接入服务 内部自动拼接 `/api/config/...`。旧版版本化接口已删除：上线前必须升级所有接入工程的 接入服务、重新构建 接入工程 与管理前端，并同时调整 Nginx 配置；不再配置旧版接口前缀的转发规则。未升级的旧调用方将收到 404。管理接口不含登录模块，必须通过可信网络、网关 ACL 等限制访问。

## 模块合并说明

`config-client-starter` 与 `config-client-demo` 已整合到 `config-center-access`，仅输出独立可运行 Spring Boot 应用，不再作为 Starter 引入。内部 Webhook `/internal/config/callback`、配置中心 `/api/config/**`、`config.client.*` 参数、Flyway V1/V2 迁移保持不变。已有本地副本数据库无需重新创建，迁移时沿用原 JDBC URL 即可；新建环境默认示例数据库为 `config_access`。旧 `/demo/**` 查询路由不再提供。

## V4 四级配置树上线（非兼容数据库迁移）

原有 `game → region → server_group → channel → sdk_app` 改为 `game → region → server_group → channel_app`。配置中心 Flyway V4 与接入工程 Flyway V3 必须同步升级，旧实例不可在新表结构下继续读写。

1. **停止写入与通知**：停止旧配置中心、管理端及接入实例，确认无活跃旧版应用后再迁移。备份权威 MySQL 和接入副本数据库，并在测试环境演练恢复。
2. **预检 Code 冲突**：不同旧渠道下的 SDK 应用提升为同一区服组直属节点后，`code` 不能重复。执行下列查询，必须返回空结果：

```sql
SELECT server_group_id, code, COUNT(*) AS cnt
FROM config_node
WHERE node_type='sdk_app'
GROUP BY server_group_id, code
HAVING COUNT(*) > 1;
```

3. **检查渠道配置**：原渠道节点的 `config`、`remark`、`status` 不会自动合并进新渠道应用。V4 自动备份所有旧渠道和 SDK 应用（含 `channel_id` 对应关系）到 `config_node_v4_legacy_archive`；如业务依赖渠道层配置，先人工决定新归属。
4. **部署配置中心**：首次启动会执行 V4，完成类型重命名、删除旧渠道节点和 `channel_id`、重建约束/索引并更新 `epoch`。必须确认 `flyway_schema_history` 已有 V4 的成功记录。
5. **部署接入工程与前端**：接入端 V3 清空旧副本并重置游标；新服务启动后自动重建四级快照。升级前端，核对区服组的渠道应用列表、全局搜索路径及增量同步。
6. **恢复服务**：确认多个配置中心实例和所有接入实例的 `epoch` 相同、应用数量一致后再恢复写入。不要让旧实例混入集群。

**注意**：MySQL DDL 存在隐式提交，迁移失败后数据库可能已部分升级；不要跳过 Flyway 失败记录或盲目重试，应根据备份和实际 DDL 状态修复后重试。代码层不提供旧 `channel` / `sdk_app` 类型兼容。
