# config-center · 权威配置中心

负责游戏、大区、区服组基础节点及其同步版本、订阅者管理、增量/快照接口和 Webhook 投递。使用共享权威库 `config_center`；SDK 专属节点配置、渠道包及功能开关由 `sdk-core` 管理，不在这里重复维护。

## 本地启动

1. 新建空库时手工执行 `../db/full/config_center.sql`；已有库按 [数据库文档](../docs/guide.html#database) 核对后选择增量 SQL，禁止重新执行全量初始化。
2. 将 `configs/config.yaml` 复制为**完整独立**的 `configs/config-dev.yaml` 并填写 `database.dsn`。启动时只读取指定文件；不传 `-config` 时默认读取当前工作目录下的 `configs/config.yaml`。环境变量仍可覆盖指定字段。
3. 在本目录运行：

```powershell
go run ./cmd/config-center -config configs/config-dev.yaml
```

检查 `GET /health/ready`。服务启动不自动执行 DDL，节点变更推进 `epoch/revision`；Webhook 只发版本通知，接入方根据修订号异步拉取。渠道包不参与三级节点同步。

## 安全与参考

`/api/config/admin/**` 默认启用 `adminAuth` 管理签名。两服务的管理密钥应一致；即使启用签名，生产也必须限制网关访问范围并使用 HTTPS。同步快照和原始节点接口同样不得无防护地公开。

- [统一接口与签名规范](../docs/index.html)
- [架构与内部路由](../docs/guide.html#architecture)
- [数据库与手动 SQL](../docs/guide.html#database)
- [本地开发](../docs/guide.html#development)
- [生产运维](../docs/guide.html#production)

```bash
go test -race ./...
go vet ./...
go build ./cmd/config-center
```
