# Go 服务统一日志规范

`config-center`、`config-center-access`、`sdk-core` 共享 `shared/logging` 模块，基于标准库 `log/slog`，不依赖额外日志框架。

## YAML 配置

```yaml
log:
  level: info
  format: json
  output: stdout
  accessEnabled: true
  slowThreshold: 500ms
```

- `level`：`debug` / `info` / `warn` / `error`。
- `format`：`json` 或 `text`。
- `output`：`stdout` 或 `stderr`；不在 Go 进程中负责写日志文件、轮转。
- `accessEnabled`：开关正常 HTTP 访问日志；即使关闭，错误请求与慢请求仍然记录。
- `slowThreshold`：大于零的 Go duration（如 `500ms`、`2s`）。
- 配置可分别通过 `LOG_LEVEL`、`LOG_FORMAT`、`LOG_OUTPUT`、`LOG_ACCESS_ENABLED`、`LOG_SLOW_THRESHOLD` 环境变量覆盖；不合法值会拒绝启动。
- `INSTANCE_ID`：可选集群实例名；默认采用系统主机名。

## HTTP 请求日志

- 响应头返回 `X-Request-ID`。调用方可以传入同名请求头以串联跨服务调用；不合法 ID 会重新生成。
- 每条请求日志包含 `service`、`instance`、`request_id`、`method`、`path`、`status`、`duration_ms`。
- 2xx/3xx 为 INFO；4xx 或慢请求为 WARN；5xx 为 ERROR。
- 成功且非慢请求的 `/health/live` 和 `/health/ready` 不产生日常访问日志。
- 不记录 URL 查询参数、请求体、请求头中的 Authorization、Cookie、签名或响应内容。
- 处理函数 panic 时记录 panic 类型，并尽可能返回 HTTP 500；不记录 panic 原值。
- 在 HTTP Handler 内部可用 `logging.FromContext(r.Context())` 让业务日志携带请求 ID。

## 生产部署

- Docker 使用日志驱动管理 stdout/stderr 的轮转和保留；systemd 使用 journald。
- 随后如需多实例检索，可选择 Loki + Grafana，按 `service`、`instance` 过滤，搜索 `request_id` 定位请求。
- 不要把高基数的 `request_id` 当作 Loki 标签。

## 开发测试

```bash
(cd shared/logging && go test ./...)
(cd config-center && go test ./...)
(cd config-center-access && go test ./...)
(cd sdk-core && go test ./...)
```

三个服务使用本仓库内的 `../shared/logging` replace；必须连同 `shared/logging` 一起构建和部署，不能只复制单个服务目录。
