# 纯 Go 本地部署与调试

## 准备环境

安装 Go 1.27.3、Node.js 22、MySQL 8.0+。

```bash
cd deploy
docker compose up -d
```

创建两个独立数据库：

```sql
CREATE DATABASE IF NOT EXISTS config_center CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE DATABASE IF NOT EXISTS config_access CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
```

## 配置中心

```bash
cd config-center
export CONFIG_DB_DSN='root:password@tcp(127.0.0.1:3306)/config_center?parseTime=true&loc=UTC'
export CONFIG_GO_ADDR=':8080'
export CONFIG_GO_ENABLE_WRITES=true
export CONFIG_GO_ENABLE_WEBHOOK=true
export WEBHOOK_ALLOWED_HOSTS='localhost,127.0.0.1'
go run ./cmd/config-center
```

新库启动时自动执行 Go 内置的 V1–V4 迁移。也支持独立命令 `go run ./cmd/migrate`。

**已存在的历史数据库：** V4 会自动校验并接入；V1–V3 必须先停写、备份，人工确认后执行：

```bash
go run ./cmd/migrate -allow-legacy-upgrade
```

不要直接删除 `config_schema_migrations` 或篡改 `dirty` 标记。MySQL DDL 非事务性，发生失败时应先确认部分完成的 DDL 并从备份恢复。

## Go 接入端

```bash
cd config-center-access
export ACCESS_DB_DSN='root:password@tcp(127.0.0.1:3306)/config_access?parseTime=true&loc=UTC'
export CONFIG_CENTER_URL='http://127.0.0.1:8080'
export CONFIG_ACCESS_ADDR=':8081'
export CONFIG_WEBHOOK_SECRET='replace-with-a-strong-secret'
go run ./cmd/config-center-access
```

新建副本库自动初始化四级表；旧副本库需要确认已完成历史 V3 结构升级。若未升级，参照 `config-center-access/db/migration/` 的 SQL 先备份并手工执行缺少的迁移，不会再启动其它语言的服务。

注册订阅者：

```bash
curl -X POST http://127.0.0.1:8080/api/config/admin/subscribers \
  -H 'Content-Type: application/json' \
  -d '{"code":"access","callbackUrl":"http://127.0.0.1:8081/internal/config/callback","secret":"replace-with-a-strong-secret"}'
```

校验：

- `http://127.0.0.1:8080/health/ready`：配置中心就绪。
- `http://127.0.0.1:8081/health/ready`：接入端就绪。
- `http://127.0.0.1:8081/api/access/nodes`：本地配置节点。
- `http://127.0.0.1:8081/api/access/revision`：本地同步游标。

## Vue 管理端

```bash
cd config-center-admin
npm ci
npm run dev
```

生产通过 Nginx 将 `/api/config/**` 转发到 Go 配置中心，无需变更 URL 前缀。

```nginx
location ^~ /api/config/ {
    proxy_pass http://config_center;
}
```

**必须严格限制 `/api/config/admin/**` 的网络访问。** 当前管理端没有账号权限系统。Webhook 回调使用 HMAC-SHA256 验签，允许目标域名须通过 `WEBHOOK_ALLOWED_HOSTS` 精确配置，生产使用 HTTPS。

## 接入端连接超时

```bash
export CONFIG_CLIENT_CONNECT_TIMEOUT_MS=2000
export CONFIG_CLIENT_READ_TIMEOUT_MS=15000
export CONFIG_RECONCILE_INTERVAL=5m
```

单次请求失败不推进本地游标，由后续回调或定时任务恢复同步。
