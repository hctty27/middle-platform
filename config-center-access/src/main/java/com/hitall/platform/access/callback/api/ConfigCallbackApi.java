package com.hitall.platform.access.callback.api;

import com.hitall.platform.access.config.ConfigClientProperties;
import com.hitall.platform.access.sync.ConfigSyncTrigger;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.time.Instant;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.server.ResponseStatusException;

/**
 * 接收配置中心的 Webhook 通知。
 * 先校验时间戳与 HMAC 签名，再触发接入方主动同步配置。
 */
@RestController
public class ConfigCallbackApi {
    private final ConfigSyncTrigger trigger;
    private final ConfigClientProperties properties;

    public ConfigCallbackApi(ConfigSyncTrigger trigger,
                                    ConfigClientProperties properties) {
        this.trigger = trigger;
        this.properties = properties;
    }

    // 通知只作为同步触发信号，不直接信任请求体中的配置数据。
    @PostMapping("/internal/config/callback")
    @ResponseStatus(HttpStatus.ACCEPTED)
    public void notify(@RequestHeader("X-Config-Timestamp") String timestamp,
                       @RequestHeader("X-Config-Signature") String signature,
                       @RequestBody Notification notification) {
        verify(timestamp, signature, notification);
        trigger.requestSync();
    }

    // 拒绝时间偏差超过 5 分钟的请求，并使用常量时间比较验证签名。
    private void verify(String timestamp, String signature, Notification notification) {
        try {
            long sentAt = Long.parseLong(timestamp);
            if (Math.abs(Instant.now().getEpochSecond() - sentAt) > 300) throw new IllegalArgumentException();
            String message = timestamp + "." + notification.epoch() + "." + notification.revision();
            Mac mac = Mac.getInstance("HmacSHA256");
            mac.init(new SecretKeySpec(properties.getWebhookSecret().getBytes(StandardCharsets.UTF_8), "HmacSHA256"));
            String expected = "sha256=" + java.util.HexFormat.of()
                    .formatHex(mac.doFinal(message.getBytes(StandardCharsets.UTF_8)));
            if (!MessageDigest.isEqual(expected.getBytes(StandardCharsets.UTF_8),
                                       signature.getBytes(StandardCharsets.UTF_8))) {
                throw new IllegalArgumentException();
            }
        } catch (Exception ex) {
            throw new ResponseStatusException(HttpStatus.UNAUTHORIZED, "Invalid callback signature");
        }
    }

    public record Notification(String epoch, long revision) {}
}
