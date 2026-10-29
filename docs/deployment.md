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
gradle :config-center-server:bootRun
```

## 启动示例

```bash
export DEMO_DB_URL='jdbc:mysql://127.0.0.1:3306/config_demo?connectionTimeZone=UTC'
export DEMO_DB_USER=root
export DEMO_DB_PASSWORD=local-dev-only
export CONFIG_CENTER_URL=http://127.0.0.1:8080
export CONFIG_WEBHOOK_SECRET='dev-signing-secret'
gradle :config-client-demo:bootRun
```

创建订阅者（仅允许 localhost 回调地址，生产需要配置 `config.webhook.allowed-hosts`）：

```bash
curl -X POST http://127.0.0.1:8080/api/config/admin/subscribers \
  -H 'Content-Type: application/json' \
  -d '{"code":"demo","callbackUrl":"http://127.0.0.1:8081/internal/config/callback","secret":"dev-signing-secret"}'
```

查看接入副本：`http://127.0.0.1:8081/demo/nodes`。

## 管理界面

```bash
cd config-center-admin
npm install
npm run dev
```

仅供可信网络访问。生产部署将 `/api/config/admin/**`（以及兼容期 `/api/v1/admin/**`）限制到受信任的网关来源，不允许匿名公网访问。由于没有登录/权限系统，网关网络 ACL 即为管理接口的唯一访问控制边界。生产的 Webhook 需要 HTTPS、受控 DNS/目标地址、密钥管理与监控。

## 客户端远程请求超时

Starter 默认连接超时为 **2000ms**、单次请求读取超时为 **15000ms**，可按内网状况覆盖：

```yaml
config:
  client:
    connect-timeout-ms: 2000
    read-timeout-ms: 15000
```

或者使用 Demo 对应的环境变量 `CONFIG_CLIENT_CONNECT_TIMEOUT_MS`、`CONFIG_CLIENT_READ_TIMEOUT_MS`。
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

接入服务的 `CONFIG_CENTER_URL` 只配置服务根 URL，例如 `http://config-center:8080`，Starter 内部自动拼接 `/api/config/...`。旧版客户端在兼容期仍可使用原 `/api/v1/...` 路由；迁移时需要单独保留 `/api/v1/` 到配置中心的转发规则，否则旧客户端可能返回 404。管理接口不含登录模块，必须通过可信网络、网关 ACL 等限制访问。
