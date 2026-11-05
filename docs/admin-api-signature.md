# 管理接口签名接入说明

管理接口由单个可信第三方服务端调用，不提供浏览器端长期密钥。配置中心与 SDK Core 使用相同的管理密钥、相同的签名算法。

## 适用范围

- 配置中心：`/api/config/admin` 及其子路径。
- SDK Core：`/api/core/admin` 及其子路径。
- GET、POST、PUT、DELETE 均须签名。
- `/api/config/sync/**`、`/api/config/nodes/**`、`/api/core/config/**`、订单、支付回调、`/api/service/**` 和健康检查不受本管理签名中间件影响；它们各自保持原有访问约束。

## 配置

在 `config-center/configs/config.yaml` 与 `sdk-core/configs/config.yaml` 添加：

```yaml
adminAuth:
  secret: ""
```

正式环境两个进程应填写**相同的至少 32 字节随机密钥**，或设置环境变量 `ADMIN_AUTH_SECRET` 覆盖 YAML。推荐优先通过 Supervisor 单进程环境变量下发密钥；不要把生产密钥提交到仓库，也不要交给浏览器。

未配置、长度不足 32 字节时，所有管理请求返回 HTTP 503（拒绝访问），其他接口不受影响。

## 请求 Header

```http
GET /api/config/admin/nodes?gameId=1 HTTP/1.1
X-Timestamp: 1793865600
X-Nonce: 0123456789abcdef
X-Signature: <40 位小写十六进制摘要>
```

- `X-Timestamp`：Unix 秒，服务器允许前后 5 分钟。
- `X-Nonce`：每次请求重新生成的 8 字节安全随机数，16 位小写十六进制。
- `X-Signature`：40 位小写十六进制 SHA-1 摘要，不发送明文 Secret。

## 管理签名 V1

复用现有 `shared/signature.SignValues`：去掉空字符串，字符串**按值字典序升序**排序，用 `_` 拼接，对拼接后的 UTF-8 数据做 SHA-1，输出小写十六进制。

参与签名的值（注意固定前缀和冒号）：

```text
admin-v1
{secret}
method:{UPPERCASE_HTTP_METHOD}
path:{ESCAPED_PATH}
query:{CANONICAL_QUERY}
body:{LOWER_HEX_SHA256_OF_EXACT_BODY_BYTES}
timestamp:{UNIX_SECONDS}
nonce:{16_HEX_NONCE}
```

具体约定：

1. `path` 是 Go 服务最终收到的转义路径（`URL.EscapedPath()`），**没有 Nginx 的 `/sdk` 前缀**。
2. `query` 是 Go `url.Values.Encode()` 的结果：按查询键排序，遵循 application/x-www-form-urlencoded 编码，重复键的值按实际请求中的先后顺序。无查询参数时为 `query:`。
3. `body` 是 HTTP 传输中实际请求体**原始字节**的 SHA-256 小写 hex，不是业务 JSON 对象的字段排序摘要。无请求体使用空字节串的 SHA-256。请求体最大 1 MiB。
4. `X-Timestamp` 和 `X-Nonce` 都纳入签名；每次重试必须重新生成 nonce 和签名。
5. `secret` 是一个参与排序的原始字符串值，不添加 `secret:` 前缀。算法是**平台兼容 SHA-1 排序签名，不是 HMAC**。

## Java 17 参考签名

```java
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.HexFormat;
import java.util.List;
import java.util.stream.Stream;

public final class AdminSigner {
    private AdminSigner() {}

    public static String sign(String secret, String method, String path,
                              String canonicalQuery, byte[] body,
                              String timestamp, String nonce) throws Exception {
        String bodyHash = hash("SHA-256", body);
        List<String> values = Stream.of(
                "admin-v1", secret, "method:" + method.toUpperCase(),
                "path:" + path, "query:" + canonicalQuery,
                "body:" + bodyHash, "timestamp:" + timestamp, "nonce:" + nonce
        ).filter(v -> !v.isEmpty()).sorted().toList();
        return hash("SHA-1", String.join("_", values).getBytes(StandardCharsets.UTF_8));
    }

    private static String hash(String algorithm, byte[] bytes) throws Exception {
        return HexFormat.of().formatHex(MessageDigest.getInstance(algorithm).digest(bytes));
    }
}
```

调用示例（`canonicalQuery`、`path` 必须与 HTTP 实际发送值一致）：

```java
String timestamp = Long.toString(System.currentTimeMillis() / 1000);
byte[] random = new byte[8];
new java.security.SecureRandom().nextBytes(random);
String nonce = HexFormat.of().formatHex(random);
byte[] body = "{\"enabled\":true}".getBytes(StandardCharsets.UTF_8);
String signature = AdminSigner.sign(secret, "PUT", "/api/core/admin/services/1",
        "", body, timestamp, nonce);
// 发送同一份 body 字节，并设置 X-Timestamp/X-Nonce/X-Signature
```

## 状态码与安全边界

- `401`：签名缺失、过期、格式错误或校验失败。
- `409`：相同 nonce 在当前进程中已使用，或本进程 nonce 缓存达到容量上限。
- `413`：管理请求体超过 1 MiB。
- `503`：管理密钥未配置或不足 32 字节。

**防重放边界**：当前实现使用每个进程独立的过期 nonce 缓存，防止同实例内的重复请求；**不能提供跨实例全局去重**。例如 sdk-core-1 和 sdk-core-2 各自会接受同一条合法请求一次。需要严格的跨实例“至多一次”时，应增加共享的原子 nonce 存储（例如 MySQL 唯一键或 Redis），并为高风险写入接口做业务幂等。

SHA-1 排序签名仅用于兼容既有中台协议，**并非现代推荐的新协议算法**。必须使用 HTTPS，使用高熵密钥，限制管理接口可访问的源地址/网络范围，并在后续允许升级时优先考虑 HMAC-SHA256。
