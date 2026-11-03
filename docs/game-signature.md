# SDK Core 游戏侧平台式 SHA-1 签名协议

## 一、协议范围

适用于“游戏 → SDK Core 订单登记”和“SDK Core → 游戏发货通知”；后续游戏侧事件上报可复用同一组件，并指定操作标识与 Body 字段。

第三方平台原生的四头签名依旧独立处理：游戏请求平台时只签 app_secret、appid、timestamp、nonce；平台支付回调按其平台 V1 指定字段验签。**内部新增字段并不发送给第三方平台。**

## 二、请求头

```http
x-app-id: 110006
x-timestamp: 1793700000
x-nonce: 0123456789abcdef
x-signature: <40 位小写 SHA-1>
```

timestamp 单位秒，允许当前时间前后五分钟；nonce 是 8 个安全随机字节的小写 hex（16 字符）。AppID 必须与路由/业务归属一致。

## 三、参与签名的字段

内部两个业务均使用以下固定值，再追加 Body 全部顶层非空值：

| 来源 | 签名值 |
| --- | --- |
| 游戏配置 | 原始 UTF-8 的 `game.config.gameSecret` |
| Header | `x-app-id`、`x-timestamp`、`x-nonce` |
| 服务端业务上下文 | env（test/prod/review）、operation |
| Body | 全部顶层非空值（包括 0、false） |

| 接口 | operation | env |
| --- | --- | --- |
| 订单登记 | `order.register` | URL `/api/core/{env}/{appid}/orders` |
| 发货通知 | `payment.notify` | 原订单保存的环境；游戏服使用自身环境复算 |

Body 字符串按 JSON 解码后的内容取值；数字/布尔值按字符串表示；嵌套对象和数组需压缩成 JSON 文本整体参与。当前充值接口只有字符串和整数，不会遇到复杂 JSON 序列化差异。

**与平台相同：按值排序，不按键名排序。** 空字符串和 null 不参与，0 与 false 必须参与。将所有非空值按字典序升序排序，用 `_` 拼接后计算 SHA-1，结果为小写 40 位 hex：

```text
plain = sort(nonEmpty([gameSecret, appid, timestamp, nonce, env, operation, ...bodyValues])).join("_")
signature = lower_hex(SHA1(UTF8(plain)))
```

例如 `gameSecret=测试-密钥!`，环境 test、AppID 110006、时间戳 1793700000、nonce 0123456789abcdef，Body 为 `{"game_order_id":"G100","amount":600,"attach":""}`，操作 `order.register`：

```text
0123456789abcdef_110006_1793700000_600_G100_order.register_test_测试-密钥!
```

签名值：

```text
0e18d1fd65d8cfdff5c63a51aaa5385e5f9fedb5
```

## 四、Java 参考实现

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
            String env, String operation, Collection<String> bodyValues) throws Exception {
        List<String> values = new ArrayList<>();
        values.add(gameSecret);
        values.add(appId);
        values.add(timestamp);
        values.add(nonce);
        values.add(env);
        values.add(operation);
        values.addAll(bodyValues);
        values.removeIf(value -> value == null || value.isEmpty());
        values.sort(String::compareTo);
        String plain = String.join("_", values);
        byte[] hash = MessageDigest.getInstance("SHA-1")
                .digest(plain.getBytes(StandardCharsets.UTF_8));
        return HexFormat.of().formatHex(hash);
    }
}
```

订单登记：

```java
String sign = GameSignature.sign(
        gameSecret, "110006", timestamp, nonce, "test", "order.register",
        List.of("G100", "600")
);
```

注意：`attach` 为空时不参与签名；如果 attach 有值，要将该值添加至 bodyValues。传给签名工具的值必须与实际发送 Body 对应。

## 五、安全及兼容说明

- 不再支持旧 V2 HMAC-SHA256/HKDF 协议，也不接受 `X-Game-*`、`X-SDK-*` 老请求头。
- URL、域名、端口、HTTP 方法不进入签名，环境和操作标识进入签名以限制跨业务复用。
- 仅校验 nonce 和时间戳，暂不持久化 nonce 去重；充值依靠订单幂等。后续非幂等操作应单独增加重放保护。
- 平台风格的原始 SHA-1 不是标准 HMAC，按值签名不绑定字段名，存在值交换/分隔符歧义风险，需依赖 HTTPS、权限校验和服务端业务约束。对高风险新接口更推荐 HMAC-SHA256。
- 游戏端保存完整原始订单并在事务内幂等发货。不要在客户端或日志中暴露任何共享密钥。
