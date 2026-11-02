# 本地开发与联调

本文以 **Windows PowerShell** 为例；三个 Go 服务使用各自的 `configs/config.yaml` 作为公开模板，复制为未提交的 `configs/config-dev.yaml` 并填写凭据。不要将包含真实密码的 YAML 提交 Git。

## 环境

- Go 1.27.3、MySQL 8.0+；运行 Vue 管理端另需 Node.js 22 / npm。
- 本地默认端口：`config-center:8080`、`config-center-access:8081`、`sdk-core:8082`。
- 权威数据库和接入副本必须分开：`config_center` 与 `config_access`。

使用 MySQL 管理工具执行：

```sql
CREATE DATABASE IF NOT EXISTS config_center
  CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE DATABASE IF NOT EXISTS config_access
  CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
```

## 1. 权威配置中心

打开第一个 PowerShell 终端：

```powershell
cd config-center
Copy-Item configs/config.yaml configs/config-dev.yaml
```

修改 `configs/config-dev.yaml` 中的 `database.dsn`，例如 `root:your-password@tcp(127.0.0.1:3306)/config_center?parseTime=true&loc=UTC`。本地需要在管理后台修改节点时配置 `admin.writesEnabled: true`；需要 Webhook 联调时还要设置 `webhook.enabled: true`，`webhook.allowedHosts` 包含回调实际主机名/IP。

对**空库先迁移，再启动**：

```powershell
go run ./cmd/migrate -config configs/config-dev.yaml
go run ./cmd/config-center -config configs/config-dev.yaml
```

成功后检查 `http://127.0.0.1:8080/health/ready`。若提示 `database migration state missing`，应先核实 DSN 指向的库是否为空，执行独立迁移；不得让启动程序自动建表或删除迁移状态表。

## 2. 接入端（可选）

新终端进入 `config-center-access`：

```powershell
cd config-center-access
Copy-Item configs/config.yaml configs/config-dev.yaml
# 修改 YAML 中 database.dsn / configCenter.url / webhook.secret
go run ./cmd/config-center-access -config configs/config-dev.yaml
```

`database.dsn` 指向 `config_access`，`configCenter.url` 通常是 `http://127.0.0.1:8080`，`webhook.secret` 必须是足够长的随机密钥。首次启动**空副本库**可自动建表和拉取全量节点，随后增量同步；旧副本库参见 [数据库文档](database.md)。

在配置中心注册订阅者（示例密钥需与接入端一致，且配置中心写入与 Webhook 已启用）：

```powershell
$body = @{
    code = "access-local"
    callbackUrl = "http://127.0.0.1:8081/internal/config/callback"
    secret = "replace-with-a-strong-local-secret"
} | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:8080/api/config/admin/subscribers" -ContentType "application/json" -Body $body
```

检查 `http://127.0.0.1:8081/api/access/revision`。回调签名验证成功只代表已接受异步同步，真正的节点更新以副本游标和节点结果为准。

## 3. SDK Core

新终端进入 `sdk-core`：

```powershell
cd sdk-core
Copy-Item configs/config.yaml configs/config-dev.yaml
# 将 database.dsn 设置为 config_center 的只读连接。
# 如果需要启用订单，将 orderDatabase.dsn 指向同一 config_center（可用独立订单账号），
# 先运行 config-center/cmd/migrate 更新至 V12；留空则不注册订单接口。
go run ./cmd/sdk-core -config configs/config-dev.yaml
```

SDK Core **直接查询权威库**，不连接 `config_access`，也不执行迁移；需要先在管理端创建已启用游戏、大区、区服组和渠道应用，并给对应环境绑定区服组，再调用：

```powershell
Invoke-RestMethod "http://127.0.0.1:8082/api/core/prod/your-appid"
```

`your-appid` 必须替换为实际渠道应用 Code；未配置时返回 404 属预期行为。接口映射见 [API 文档](api.md) 与 [SDK Core 说明](../sdk-core/README.md)。

## 4. Vue 管理端

```powershell
cd config-center-admin
npm ci
npm run dev
```

按 Vite 控制台显示的地址打开管理端。需要其他局域网设备访问时可运行 `npm run dev -- --host 0.0.0.0`，并配置相应防火墙与后端访问策略；切勿将无认证的管理端直接开放公网。

## 5. 检查与排障

| 现象 | 检查项 |
| --- | --- |
| `database.dsn ... is required` | 指定 YAML 中是否填 DSN；也可使用对应环境变量 |
| `migration state missing` | 连接的是否为空库、是否执行过 `cmd/migrate` |
| `obsolete replica schema` | 副本版本过旧；迁移到 V6 或备份后使用新空库 |
| SDK Core 返回 404 | AppID、`env`、三个绑定字段、节点启用状态 |
| Webhook 不生效 | 订阅者密钥、回调 URL、`allowedHosts`、反向代理访问和同步游标 |
| 前端接口失败 | Vite 代理、配置中心端口以及是否启用管理写入 |

公共配置优先级以各工程的 `configs/config.yaml` 与 `internal/settings` 实现为准，通常由默认值、配置文件和环境变量叠加。线上发布请转到 [生产运维](production.md)。
