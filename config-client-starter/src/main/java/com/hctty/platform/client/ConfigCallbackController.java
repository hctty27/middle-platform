package com.hctty.platform.client;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.time.Instant;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.server.ResponseStatusException;

@RestController
public class ConfigCallbackController {
    private final ClientSynchronizer synchronizer;
    private final ConfigClientProperties properties;

    public ConfigCallbackController(ClientSynchronizer synchronizer,
                                    ConfigClientProperties properties) {
        this.synchronizer = synchronizer;
        this.properties = properties;
    }

    @PostMapping("/internal/config/callback")
    public void notify(@RequestHeader("X-Config-Timestamp") String timestamp,
                       @RequestHeader("X-Config-Signature") String signature,
                       @RequestBody Notification notification) {
        verify(timestamp, signature, notification);
        synchronizer.synchronize();
    }

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
