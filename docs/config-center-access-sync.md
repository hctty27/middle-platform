# 配置中心第三方接入说明

> 适用对象：需要同步游戏配置的第三方系统。  
> 版本：2026-11-03（基于 `middle-platform` 主分支 `ea3c2067`）。  
> 本文仅介绍接入协议，不涉及中台内部部署、数据库或任务调度。

## 1. 接入方式

配置中心采用 **Webhook 通知 + 主动拉取**：

1. 第三方提供一个 HTTP/HTTPS 回调地址。
2. 中台管理员登记回调地址及双方约定的签名密钥。
3. 配置有变更时，中台发送 Webhook 通知。
4. 第三方验签、快速回复 HTTP 2xx，异步拉取配置并更新本地缓存。
5. 建议每 5 分钟主动检查版本，补偿可能丢失的通知。

**推荐最简单的接入方式：收到通知后直接拉取全量快照。** 配置规模较大时再考虑增量同步。

同步范围仅包括 `game`（游戏）、`region`（大区）、`server_group`（区服组），**不包括渠道应用、渠道绑定与充值订单**。

## 2. 接入参数

| 参数 | 由谁提供 | 示例 |
| --- | --- | --- |
| `code` | 第三方 | `game-service-a` |
| `callbackUrl` | 第三方 | `https://game.example.com/internal/config/callback` |
| `secret` | 双方安全交换 | 强随机字符串 |
| `configCenterBaseUrl` | 中台管理员 | `https://config.example.com`（示意） |

第三方通常不需要访问管理 API；管理员负责创建订阅者，并提供可访问的同步接口地址。

## 3. 接收 Webhook

**中台请求：** `POST {callbackUrl}`。

```http
POST /internal/config/callback HTTP/1.1
Content-Type: application/json
X-Config-Timestamp: 1760000000
X-Config-Signature: sha256=<64位小写十六进制签名>

{"epoch":"550e8400-e29b-41d4-a716-446655440000","revision":12}
```

| 字段 | 含义 |
| --- | --- |
| `epoch` | 配置数据集标识；改变时应重新全量同步 |
| `revision` | 配置版本号，同一 `epoch` 下单调递增 |
| `X-Config-Timestamp` | Unix 秒时间戳 |
| `X-Config-Signature` | HMAC-SHA256 签名 |

**验签规则：**

```text
待签名字符串 = timestamp + "." + epoch + "." + revision
签名 = "sha256=" + hex_lower(HMAC_SHA256(secret, 待签名字符串))
```

校验签名和时间戳（允许前后 5 分钟）。不合法返回 HTTP 401；合法通知尽快回复 HTTP 200/202。

> HTTP 2xx 表示通知已接收，**不代表配置已经同步完成**。应在后台执行拉取，避免占用 Webhook 请求。

通知可能重复，按 `epoch + revision` 判断是否需要更新，不要依赖通知一定只到达一次。

## 4. 拉取配置（推荐全量）

### 4.1 查询版本

```http
GET /api/config/sync/revision
```

```json
{"epoch":"550e8400-e29b-41d4-a716-446655440000","revision":12}
```

### 4.2 拉取全量快照

```http
GET /api/config/sync/snapshot
```

响应示例（节选）：

```json
{
  "epoch": "550e8400-e29b-41d4-a716-446655440000",
  "revision": 12,
  "nodes": [
    {
      "id": 1,
      "nodeType": "game",
      "gameId": null,
      "regionId": null,
      "code": "game_a",
      "name": "示例游戏",
      "status": 1,
      "config": "{\"featureEnabled\":true}"
    }
  ]
}
```

节点还可能包含 `envType`、`remark`、时间等字段。注意 `config` 是 **JSON 字符串**，需要再解析成业务对象。

第三方可自行选择内存缓存、文件或数据库保存配置，无须使用中台的 MySQL 副本。保存完整快照和 `epoch/revision` 时，应确保二者同时生效，不要先推进版本号。

**需要重新拉取的场景：**

- 第一次接入，尚无本地配置。
- 收到新版本的通知。
- 发现 `epoch` 变化。
- 定期检查发现本地 `revision` 落后。

重复的旧通知可以忽略；实际最新版本以配置中心查询结果为准。

## 5. 可选：增量同步

当全量数据较大时，可以使用：

| 接口 | 用途 |
| --- | --- |
| `GET /api/config/sync/changes?afterRevision=12&limit=200` | 按版本获取变更事件 |
| `POST /api/config/nodes/batch-get` | 按 ID 获取节点最新值，Body：`{"ids":[1,2,3]}` |

需要检查 `revision` 是否连续；批量查询中缺失的节点表示已被删除。节点更新和游标保存需保持一致。遇到版本断档、`epoch` 变化或一致性无法确认时，回退全量快照。

## 6. 联调检查

- [ ] 修改游戏、大区或区服组节点后，收到签名 Webhook 并成功拉取配置。
- [ ] 错误密钥或篡改签名时返回 HTTP 401。
- [ ] 重复通知不会造成错误的重复更新。
- [ ] 临时关闭回调后，定期版本检查仍可补齐配置。
- [ ] 修改渠道应用不会触发三级节点同步。

## 7. 注意事项

- 回调地址支持公网和内网 HTTP/HTTPS；正式环境建议 HTTPS。
- 同步 API 可能返回敏感配置，应通过可信网络或受保护网关访问，不要将密钥暴露给浏览器。
- 接入服务器需保持时间同步。
- 只需要按 AppID 查询 SDK 配置时，应使用独立的 `GET /api/core/config/{env}/{appid}`，而不是三级节点同步协议。

**第三方最低接入工作量：一个验签回调接口、一个全量拉取流程、本地配置存储，以及定期版本检查。**
