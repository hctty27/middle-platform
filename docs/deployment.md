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
curl -X POST http://127.0.0.1:8080/api/v1/admin/subscribers \
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

仅供可信网络访问。生产部署将 `/api/v1/admin/**` 限制到受信任的网关来源，不允许匿名公网访问。由于没有登录/权限系统，网关网络 ACL 即为管理接口的唯一访问控制边界。生产的 Webhook 需要 HTTPS、受控 DNS/目标地址、密钥管理与监控。
