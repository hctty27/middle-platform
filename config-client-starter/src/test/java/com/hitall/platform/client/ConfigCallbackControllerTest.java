package com.hitall.platform.client;

import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.util.HexFormat;

import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

import org.junit.jupiter.api.Test;
import org.springframework.http.HttpStatus;
import org.springframework.web.server.ResponseStatusException;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.Mockito.*;

class ConfigCallbackControllerTest {
    @Test
    void validSignatureSchedulesSyncWithoutExecutingItInController() throws Exception {
        ConfigSyncTrigger trigger = mock(ConfigSyncTrigger.class);
        ConfigClientProperties properties = new ConfigClientProperties();
        properties.setWebhookSecret("integration-test-key");
        ConfigCallbackController controller = new ConfigCallbackController(trigger, properties);

        String timestamp = Long.toString(Instant.now().getEpochSecond());
        var notification = new ConfigCallbackController.Notification("epoch-a", 42);
        String payload = timestamp + ".epoch-a.42";
        Mac mac = Mac.getInstance("HmacSHA256");
        mac.init(new SecretKeySpec("integration-test-key".getBytes(StandardCharsets.UTF_8), "HmacSHA256"));
        String signature = "sha256=" + HexFormat.of().formatHex(
                mac.doFinal(payload.getBytes(StandardCharsets.UTF_8)));

        assertDoesNotThrow(() -> controller.notify(timestamp, signature, notification));
        verify(trigger).requestSync();
    }

    @Test
    void invalidSignatureDoesNotScheduleSync() {
        ConfigSyncTrigger trigger = mock(ConfigSyncTrigger.class);
        ConfigClientProperties properties = new ConfigClientProperties();
        properties.setWebhookSecret("integration-test-key");
        ConfigCallbackController controller = new ConfigCallbackController(trigger, properties);

        var notification = new ConfigCallbackController.Notification("epoch-a", 42);
        ResponseStatusException error = assertThrows(ResponseStatusException.class,
                () -> controller.notify(Long.toString(Instant.now().getEpochSecond()),
                        "sha256=wrong", notification));
        assertEquals(HttpStatus.UNAUTHORIZED, error.getStatusCode());
        verifyNoInteractions(trigger);
    }
}
