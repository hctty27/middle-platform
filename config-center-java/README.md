# Java 配置中心（兼容与迁移）

本目录原名 `config-center`，现重命名为 `config-center-java`。Go 主配置中心已位于仓库根目录下的 `config-center`。

## 保留用途

- Java 25 / Spring Boot 的原始配置中心实现和 Java/Go API 合同对照。
- 权威 MySQL 的 Flyway V1–V4 迁移脚本；**Go 主服务当前不会执行 Flyway**。
- CI 的历史数据升级验证和双实例 Webhook E2E 测试。

## 启动

```bash
export CONFIG_DB_URL='jdbc:mysql://127.0.0.1:3306/config_center?connectionTimeZone=UTC'
export CONFIG_DB_USER=root
export CONFIG_DB_PASSWORD=local-dev-only
export CONFIG_CENTER_PORT=8082
./gradlew :config-center-java:bootRun
```

首次迁移成功后可停止 Java 服务，再启动 `config-center` Go 主服务。配置树、数据库表、同步接口和已有环境变量语义不因目录改名而变化。不能同时在同一监听端口启动两个配置中心。
