# SDK Core 游戏侧平台式 SHA-1 签名协议

## 一、协议范围

游戏 → SDK Core 的订单登记、SDK Core → 游戏的发货通知，使用与第三方平台相同的**值排序、下划线拼接、SHA-1 小写**算法。游戏侧两个方向均签请求 Body 的所有顶层非空值。

平台原生入站上报通常仅签密钥、AppID、时间戳、Nonce（不签 Body）；平台 → SDK Core 的支付回调继续采用平台指定的独立字段规则，不改动。

## 二、请求头

```http
x-app-id: 110006
x-timestamp: 1793700000
x-nonce: 0123456789abcdef
x-signature: <40 位小写 SHA-1>
```

时间戳为 Unix 秒，容许前后五分钟；Nonce 是 8 字节随机数的小写 hex，长度为 16。AppID 必须与路由或游戏服务配置匹配。

## 三、参与签名的值

| 来源 | 值 |
| --- | --- |
| 游戏主密钥 | `game.config.gameSecret` 原始 UTF-8 字符串 |
| Header | `x-app-id`、`x-timestamp`、`x-nonce` |
| Body | 全部顶层非空值（含数字 `0`、布尔 `false`） |

**不签 `env`、`operation`，也不签参数名称、URL、HTTP 方法、域名、端口。**

Body 中的空字符串与 `null` 跳过；对象和数组需转换为紧凑 JSON 字符串整体参与。参与签名的所有字符串按**值**字典序升序排序，使用下划线 `_` 拼接后计算 SHA-1，输出 40 位小写十六进制。

```text
plain = sort(nonEmpty([gameSecret, appid, timestamp, nonce, ...bodyValues])).join("_")
signature = lower_hex(SHA1(UTF8(plain)))
```

示例：密钥 `测试-密钥!`，appid=110006，timestamp=1793700000，nonce=0123456789abcdef，Body 为 `{"game_order_id":"G100","amount":600,"attach":""}`，得到：

```text
0123456789abcdef_110006_1793700000_600_G100_测试-密钥!
```

对应签名：

```text
a8c30a586539b1d9cd7c5429421e3e88b6a02f6e
```

## 四、Java 接入示例

```java
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.Collection;
import java.util.HexFormat;
import java.util.List;

public final class GameSignature {
    private GameSignature() {}

    public static String sign(
            String gameSecret, String appId, String timestamp, String nonce,
            Collection<String> bodyValues) throws Exception {
        List<String> values = new ArrayList<>();
        values.add(gameSecret);
        values.add(appId);
        values.add(timestamp);
        values.add(nonce);
        values.addAll(bodyValues);
        values.removeIf(value -> value == null || value.isEmpty());
        values.sort(String::compareTo);
        byte[] hash = MessageDigest.getInstance("SHA-1")
                .digest(String.join("_", values).getBytes(StandardCharsets.UTF_8));
        return HexFormat.of().formatHex(hash);
    }
}
```

订单登记：

```java
String signature = GameSignature.sign(
        gameSecret, "110006", timestamp, nonce, List.of("G100", "600")
);
```

`attach` 非空时将其作为额外业务值添加；发送前保证入参和 JSON Body 内容一致。发货通知也复用相同工具，传入 `order_no`、`game_order_id`、`amount`、非空 `attach`、`paid_at`。

## 五、业务校验与注意事项

- `env` 虽不签，订单登记仍按 URL 的 `{env}` 检查 AppID 绑定、服务状态与大区配置，重复订单需匹配原环境；游戏服处理发货通知时须独立校验业务环境及订单归属。
- 不签 `operation` 意味着签名自身不能限制跨接口复用；接收接口要严格校验请求结构、业务权限和订单幂等。
- 时间戳与 nonce 格式校验不意味着 nonce 防重放，目前未建立已用 nonce 去重存储。
- 原生 SHA-1 加共享密钥不是标准 HMAC；按值排序不绑定字段名，还存在字段交换或分隔符歧义。务必在受控服务端使用高强度密钥、HTTPS 和业务校验。
- 不兼容 V2 HMAC/HKDF，也不再使用旧 `X-Game-*`、`X-SDK-*` 请求头；升级 SDK Core 时需同步更新游戏端与管理端。
