package com.hitall.platform.config.webhook.service;

import com.hitall.platform.config.webhook.repository.DeliveryRepository;

import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.time.Instant;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;

/**
 * 定时发送配置变更通知。
 * Webhook 只携带 epoch 和 revision，接入方收到通知后主动拉取配置。
 */
@Component
public class WebhookDispatcher {
    private final DeliveryRepository repository;
    private final HttpClient client;
    private final Duration timeout;

    public WebhookDispatcher(DeliveryRepository repository,
            @Value("${config.webhook.request-timeout-seconds:5}") int seconds) {
        this.repository = repository;
        this.timeout = Duration.ofSeconds(seconds);
        this.client = HttpClient.newBuilder().connectTimeout(timeout)
                .followRedirects(HttpClient.Redirect.NEVER).build();
    }

    // 每个实例均可执行轮询；实际投递由数据库租约协调，失败留待下次重试。
    @Scheduled(fixedDelayString = "${config.webhook.poll-delay-ms:3000}")
    public void dispatch() {
        repository.claim().ifPresent(delivery -> {
            try {
                send(delivery);
                repository.delivered(delivery);
            } catch (Exception ex) {
                repository.failed(delivery, ex.getClass().getSimpleName() + ": " + ex.getMessage());
            }
        });
    }

    // 签名内容为 timestamp.epoch.revision，接收端使用共享密钥校验请求。
    private void send(DeliveryRepository.Delivery delivery) throws Exception {
        long timestamp = Instant.now().getEpochSecond();
        String body = "{\"epoch\":\"" + delivery.epoch() + "\",\"revision\":" + delivery.revision() + "}";
        String message = timestamp + "." + delivery.epoch() + "." + delivery.revision();
        Mac mac = Mac.getInstance("HmacSHA256");
        mac.init(new SecretKeySpec(delivery.secret().getBytes(StandardCharsets.UTF_8), "HmacSHA256"));
        String signature = java.util.HexFormat.of().formatHex(mac.doFinal(message.getBytes(StandardCharsets.UTF_8)));
        HttpRequest request = HttpRequest.newBuilder(URI.create(delivery.url()))
                .timeout(timeout)
                .header("Content-Type", "application/json")
                .header("X-Config-Timestamp", Long.toString(timestamp))
                .header("X-Config-Signature", "sha256=" + signature)
                .POST(HttpRequest.BodyPublishers.ofString(body)).build();
        HttpResponse<Void> response = client.send(request, HttpResponse.BodyHandlers.discarding());
        // 只有 2xx 才视为成功，其他 HTTP 状态交由投递仓储记录失败并重试。
        if (response.statusCode() / 100 != 2) {
            throw new IllegalStateException("HTTP " + response.statusCode());
        }
    }
}
