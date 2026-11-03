# 模拟游戏充值通知服务

该工具仅用于本地联调 SDK Core 发货通知，不实现 HMAC 验签、业务发货或订单持久化；**禁止在正式环境使用**。

## 启动

在 `sdk-core` 目录运行：

```powershell
go run ./tools/mock-notify
```

默认监听 `127.0.0.1:9000`，接收 `POST /pay/notify`，并在成功模式返回 HTTP 200、响应 Body 为收到的 `X-SDK-Nonce`（不附带换行）。

可选参数：

```powershell
# 调整监听 IP 和端口
go run ./tools/mock-notify -addr 127.0.0.1:9001

# 返回 HTTP 500，用来测试 SDK Core 自动重试
go run ./tools/mock-notify -mode fail

# 检查进程是否在运行
curl.exe -i http://127.0.0.1:9000/health/live
```

## 配置

在 **测试大区**的 `region.config` 中填写：

```json
{
  "payNotifyUrl": "http://127.0.0.1:9000/pay/notify"
}
```

注意：`127.0.0.1` 指 **SDK Core 所在机器**。如果 SDK Core 与模拟服务位于不同主机，必须使用 SDK Core 可以访问的地址，并在可信网络内调整监听 IP；不要把未验签的模拟服务暴露在公网。

## 联调流程

1. 启动模拟服务，并通过 `/health/live` 检查。
2. 在充值模拟管理页面登记**新订单**，使用测试环境。
3. 模拟支付成功回调，检查平台回调返回 HTTP 200。
4. 观察本工具控制台的通知日志；成功模式下 SDK Core 最终应将 `notify_status` 更新为 1。

检查数据库状态：

```sql
SELECT game_order_id, game_notify_url, payment_status, notify_status,
       notify_attempts, last_notify_error, next_notify_at
FROM game_order
WHERE appid = 'h0200'
ORDER BY id DESC
LIMIT 10;
```

`payment_status=1` 表示支付确认；`notify_status=1` 表示模拟服务已确认通知。

**已有订单的发货通知地址不会随 `region.config` 改变**，因为 SDK Core 在首次登记时保存了 URL 快照。修改 `payNotifyUrl` 后应使用新的订单号登记。旧订单失败后仍按照原地址进行补偿重试；测试环境使用完毕注意清理或隔离测试数据。

运行测试：

```powershell
go test ./tools/mock-notify
```
