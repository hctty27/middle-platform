# 研发中台生产部署（Docker Compose + Nginx + GHCR）

本方案适用于 `config-center`、`sdk-core`、`config-center-access`、`config-center-admin`。
Go 进程、MySQL 数据和管理控制台均应部署在可信内网；只有 SDK 所需的公开 API 可以对公网开放。

> 约束：这里是可上线的第一阶段方案，不等于端到端高可用。单个 Nginx、单个 MySQL 仍是单点。
> 正式服不可直接公开 `/api/config/admin/**`、`/api/config/sync/**` 或 `/internal/config/callback`。

## 1. 架构与项目文件

- `deploy/docker/go.Dockerfile`：同一构建模板生成三个 Go 镜像，构建上下文必须是仓库根目录，以包含 `shared/logging`。
- `deploy/docker/admin.Dockerfile`：Node 构建 Vue，Nginx 只服务静态文件。
- `deploy/compose.prod.yml`：应用节点运行配置中心、两个独立的 SDK Core 进程（`sdk-core-1` / `sdk-core-2`）和管理静态站点；可选独立部署接入副本。
- `deploy/nginx/gateway.conf.example`：部署于独立网关机或云负载均衡后的 Nginx 示例。
- `deploy/deploy.sh`：给**已从网关摘流量**的单个节点部署固定 SHA 标签镜像。
- `.github/workflows/publish-images.yml`：在 main 的 Build 工作流通过之后，构建并推送四个 GHCR 镜像。

建议将 config-center 与 sdk-core 部署在两个应用节点，每个节点运行相同 Compose；数据库使用独立 MySQL 8.x，统一处于低延迟内网。每台应用机默认运行 `sdk-core-1`（宿主机 8082 → 容器 8082）和 `sdk-core-2`（宿主机 8084 → 容器 8082），**共用同一份只读挂载的 `sdk-core.yaml`**。管理端占用宿主机 8083，避免端口冲突。单机起步可将 `BIND_IP=127.0.0.1`，本机 Nginx 转发到 8080、8082、8084、8083。双节点示例中 Nginx 对四个 SDK 实例负载均衡（两台机器各两个），固定 IP 必须按实际环境调整。

`config-center-access` 是接入侧副本，不是配置中心集群的必选组件；需要它时可部署到独立接入节点，使用自己的 `config_access` 数据库和订阅回调配置。不同接入节点若使用不同副本库，必须各自收到通知或执行周期校对，不能只依赖负载均衡向其中一台发送回调。

## 2. 准备数据库和账号

先创建空库并保存备份。正式服应为 config-center 创建最小化权限账号，sdk-core 使用只读账号：

```sql
CREATE DATABASE IF NOT EXISTS config_center
  CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
-- 接入侧确有需要时，在其独立数据库实例上创建 config_access。
```

按实际数据库版本、迁移所需 DDL 权限以及来源主机创建账号与授权，不要使用 root DSN。
SDK 只需 `config_center` 业务查询所需的 SELECT 权限。多台 sdk-core 不会修改这个库。
生产数据库不应通过公网开放 3306；配置中心和 SDK Core 应处于同地域、低延迟网络。

## 3. 准备每台应用节点

安装 Docker Engine 和 Docker Compose 插件，将仓库克隆至 `/opt/middle-platform`，并在每台应用机上创建：

```bash
sudo install -d -m 0750 /etc/middle-platform
sudo cp config-center/configs/config.yaml /etc/middle-platform/config-center.yaml
sudo cp sdk-core/configs/config.yaml /etc/middle-platform/sdk-core.yaml
sudo cp deploy/.env.prod.example /etc/middle-platform/compose.env
```

分别编辑三个文件：

- `config-center.yaml`：配置 `database.dsn`、`server.address: ":8080"`；若使用管理台写入，启用 `admin.writesEnabled`；如需要接入 Webhook，启用 `webhook.enabled` 并配置可信的 `allowedHosts`。
- `sdk-core.yaml`：两个 SDK Core 容器共用此文件，使用同一个权威 `config_center` 库、只读账号；`server.address: ":8082"`。两容器的 **MySQL 连接池上限相加**，部署前确认数据库连接容量。
- `compose.env`：填写发布后的 `IMAGE_TAG=sha-<完整40位commit SHA>`、本机 `BIND_IP`、`CONFIG_DIR`。

服务容器以 UID/GID `65532` 运行，需要有权读取 YAML。若使用 rootful Docker 和本机 bind mount，可将两份文件设为 root 所有、GID 65532、模式 0640，并确保目录可遍历：

```bash
sudo chown root:65532 /etc/middle-platform/config-center.yaml /etc/middle-platform/sdk-core.yaml
sudo chmod 0640 /etc/middle-platform/config-center.yaml /etc/middle-platform/sdk-core.yaml
sudo chgrp 65532 /etc/middle-platform
sudo chmod 0750 /etc/middle-platform
sudo chmod 0600 /etc/middle-platform/compose.env
```

上述权限适用于 rootful Docker 的普通 bind mount；若启用了 rootless Docker 或 user namespace 映射，必须按映射后的 UID/GID 调整权限和目录可遍历性。

> 真实配置和 `compose.env` 仅存生产机或安全凭据管理系统，禁止提交 Git；镜像里不打包任何真实凭据。
> 同时配置安全组/防火墙：8080、8081、8082、8083、8084 仅允许网关或可信接入节点访问。

## 4. 镜像发布

`main` 分支 `Build` 工作流成功完成后，`Publish production images` 自动校验 SDK Core 与 Compose，然后推送：

```text
ghcr.io/hctty27/middle-platform-config-center:sha-<40位commit>
ghcr.io/hctty27/middle-platform-config-center-access:sha-<40位commit>
ghcr.io/hctty27/middle-platform-sdk-core:sha-<40位commit>
ghcr.io/hctty27/middle-platform-admin:sha-<40位commit>
```

也可在 GitHub Actions 手动运行发布工作流（只能从 main 手动发布）。推送依赖 `GITHUB_TOKEN` 的 `packages: write` 权限；如 GHCR 包不可匿名拉取，先在部署机 `docker login ghcr.io`，使用具有 `read:packages` 权限的访问令牌，或将包设为 Public。不要将拉取令牌写入仓库。

## 5. 执行数据库迁移（先于应用发布）

生产**先备份、停写、运行一次迁移**，检查 `config_schema_migrations` 和日志，再发布新容器：

```bash
cd /opt/middle-platform
export IMAGE_TAG=sha-<完整40位commit>
docker compose --env-file /etc/middle-platform/compose.env \
  -f deploy/compose.prod.yml run --rm --no-deps \
  --entrypoint /app/migrate config-center -config /app/config.yaml
```

这使用与业务服务同一版本镜像内的独立 Go 迁移命令。
MySQL DDL 可能隐式提交，升级失败不得直接重跑或手改 dirty 标记。
历史迁移（尤其 V6 GTID 和 V11 绑定约束）必须先确认备份、数据冲突和版本兼容性。
如果迁移不是向后兼容的，应安排维护窗口；跨版本回滚应用也不能自动回滚数据库。

## 6. 首次启动与后续发布

首次启动或节点已经被摘流量时运行：

```bash
cd /opt/middle-platform
bash deploy/deploy.sh sha-<完整40位commit>
```

脚本会校验 Compose 配置、拉取并部署四个运行服务（`config-center`、`sdk-core-1`、`sdk-core-2`、`admin`），执行 `up -d --wait` 并显示状态。两个 SDK 容器使用同一镜像标签。仅更新双 SDK 实例时可以使用 `bash deploy/deploy.sh sha-<完整40位commit> sdk-core`，或者单独指定 `sdk-core-1` / `sdk-core-2`。

只有需要接入端时，先准备独立 `config-center-access.yaml` 和接入副本数据库，
然后使用 `--profile access` 单独启动，且不要对公网暴露 8081：

```bash
export IMAGE_TAG=sha-<完整40位commit>
docker compose --env-file /etc/middle-platform/compose.env \
  -f deploy/compose.prod.yml --profile access up -d --wait config-center-access
```

验证：

```bash
curl -fsS http://<应用内网IP>:8080/health/ready
curl -fsS http://<应用内网IP>:8082/health/ready
curl -fsS http://<应用内网IP>:8084/health/ready
curl -fsS http://<应用内网IP>:8083/healthz
docker compose --env-file /etc/middle-platform/compose.env -f deploy/compose.prod.yml ps
```

**双节点滚动发布**必须逐台进行：

1. 在 Nginx 或上游负载均衡器摘除节点 A（停止向其发送新请求），确认 B 健康且能承载流量。
2. 在 A 上执行 `bash deploy/deploy.sh sha-...`，验证 **8082 与 8084 两个**健康接口、SDK 核心查询和 Webhook 行为。
3. 将 A 重新加入网关并观察错误率、连接数，再摘除 B，重复操作。
4. 同一节点内部也可对 `sdk-core-1`、`sdk-core-2` 逐个更新（前提是 Nginx 已摘除对应端口），以减少单节点中断；跨节点仍需保证其他实例健康。
5. 最后进行业务冒烟测试及配置变更、回调、增量同步校验。

这里的开源 Nginx 使用**被动**故障检测，不会主动查询 `/health/ready`；
不能以为 `docker compose --wait` 自动替网关摘流量。
若要无人值守零停机滚动发布，需要额外实现可验证的网关摘流量/复流量自动化，或引入支持主动健康探测的负载均衡器。发布脚本目前不会远程操作网关，以免误摘全部实例。

回滚镜像时，对单个摘流量节点运行：

```bash
bash deploy/deploy.sh sha-<上一个可用commit>
```

数据库结构迁移**不随镜像回滚**；务必确保前后版本可兼容或使用独立恢复方案。

## 7. Nginx 网关和后台权限

在独立网关机参考 `deploy/nginx/gateway.conf.example`：

1. 替换所有 `10.0.0.11/12`、域名、证书目录及管理员 VPN 网段。
2. TLS 证书必须匹配 `api.example.com` 和 `admin.example.com`（可使用对应 SAN 证书）。
3. 在 Nginx 机生成 htpasswd 凭据，配置可读权限；`admin.example.com` 同时执行 VPN/IP 白名单与 Basic Auth。
4. `api.example.com` 仅公开 `/api/core/`；`/api/config/admin/`、同步 API 和内部回调通过可信网络访问，不走公开域名。
5. Nginx upstream 中包含两台应用机器上各两个 SDK 端口（`8082`、`8084`）；执行 `nginx -t` 通过后再 `systemctl reload nginx`。Nginx 要能访问两台应用节点的内网端口。

目前配置中心管理接口没有自身的账号权限体系，Basic Auth + VPN/IP 限制仅为第一阶段的网关保护措施；未来应补充应用层身份认证、角色和审计。

## 8. 生产运维检查清单

- 镜像固定到 `sha-完整commit`，不要在发布中依赖漂移的 `latest` 标签。
- 将生产权限限制在最小范围；镜像运行的 Go 进程为非 root。
- 至少保证数据库定期备份、可恢复性验证、应用日志轮转和磁盘空间告警。
- 监控 Go `/health/live`、`/health/ready`、MySQL 连接数、请求错误率、Webhook 重试量、接入端 revision。
- 外部入口增加合理超时、限流、TLS 和访问审计；SDK 配置响应不得暴露支付/签名私钥。
- 真正的端到端高可用还需要冗余 Nginx/云负载均衡和 HA MySQL，单纯两个 Go 实例不足以消除所有单点。
