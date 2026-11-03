# 游戏侧统一签名（Java / Go）

为兼容现有游戏项目，订单登记和发货通知统一使用 `sortJoinSign(data) + gameSecret` 后计算 **MD5 大写**。此协议仅用于游戏与 SDK Core 通信。第三方平台的原生 SHA-1 规则保持独立。

## 1. 请求头

```http
x-app-id: 110006
x-env: test
x-timestamp: 1793700000
x-nonce: 0123456789abcdef
x-signature: <32 字符大写 MD5>
```

游戏登记 URL 为 `POST /api/core/{env}/{appid}/orders`；发货通知的 URL 为大区配置的 `payNotifyUrl`。回调成功时仍需返回 HTTP 200，响应体为收到的 `x-nonce` 原样字符串。

## 2. 签名参数

把 JSON Body **所有顶层标量字段**放进一张 Map（不包含 `sign`），再添加下列公共字段：

| 键 | 值 |
| --- | --- |
| `action` | 登记固定 `order.register`；发货通知固定 `payment.notify` |
| `appid` | 渠道 AppID，必须与 `x-app-id` 一致 |
| `env` | prod / test / review |
| `timestamp` | Unix 秒时间戳，取自 `x-timestamp` |
| `nonce` | 8 字节随机数的小写十六进制值 |

按**键名**升序排序；忽略 null、空字符串与 `sign` 字段；每个值按 Java 的 `toString().trim()` 处理，然后连接为 `key=value&key=value`。最后直接拼接 `gameSecret`，对 UTF-8 编码求 MD5，输出大写十六进制。

游戏密钥来自 `game.config.gameSecret`，以**原始字符串**使用，不做 Base64 解码或 HKDF 派生。

## 3. Java 参考实现

```java
import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import java.util.Map;
import org.springframework.util.DigestUtils;

public final class GameSign {
    private GameSign() {}

    public static String sign(Map<String, Object> data, String secret) {
        String raw = sortJoinSign(data) + secret;
        return DigestUtils.md5DigestAsHex(
                raw.getBytes(StandardCharsets.UTF_8)).toUpperCase(java.util.Locale.ROOT);
    }

    public static String sortJoinSign(Map<String, Object> data) {
        String[] keys = data.keySet().toArray(new String[0]);
        Arrays.sort(keys);
        StringBuilder result = new StringBuilder();
        for (String key : keys) {
            Object value = data.get(key);
            if (value == null || "".equals(value) || "sign".equals(key)) continue;
            if (result.length() > 0) result.append('&');
            result.append(key).append('=').append(value.toString().trim());
        }
        return result.toString();
    }
}
```

示例登记请求体：

```json
{"game_order_id":"G100","amount":600,"attach":"roleId=1"}
```

请求参数追加公共字段后，使用 `gameSecret=secret`、`timestamp=1793700000`、`nonce=0123456789abcdef`，规范化字符串为：

```text
action=order.register&amount=600&appid=110006&attach=roleId=1&env=test&game_order_id=G100&nonce=0123456789abcdef&timestamp=1793700000
```

最终签名：

```text
B4AB77CDEC0116260192CEAC17D88D1D
```

## 4. 重要约束

- JSON Body 中不得出现 `action`、`appid`、`env`、`timestamp`、`nonce`、`sign` 等保留字段。
- JSON 对象、数组不能直接作为签名值；未来有嵌套数据的接口应先定义稳定的字符串形式。
- `x-timestamp` 允许前后 5 分钟；未建立 nonce 持久化去重，不能视为一次性签名。
- 如果字段值包含 `&` 或 `=`，这种文本拼接方式可能产生语义歧义；新高风险接口优先考虑标准 HMAC。
- 密钥只能放服务端；金额、订单身份和发货必须以服务端数据为准，并进行幂等校验。
