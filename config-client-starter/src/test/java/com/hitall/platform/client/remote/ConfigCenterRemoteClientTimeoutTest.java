package com.hitall.platform.client.remote;

import com.hitall.platform.client.config.ConfigClientProperties;

import java.io.IOException;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

import com.sun.net.httpserver.HttpServer;
import org.junit.jupiter.api.Test;
import org.springframework.web.client.ResourceAccessException;

import static org.junit.jupiter.api.Assertions.*;

class ConfigCenterRemoteClientTimeoutTest {
    @Test
    void rejectsNonPositiveOrUnboundedTimeouts() {
        ConfigClientProperties settings = new ConfigClientProperties();
        settings.setConnectTimeoutMs(0);
        assertThrows(IllegalArgumentException.class, () -> new ConfigCenterRemoteClient(settings));

        settings.setConnectTimeoutMs(2000);
        settings.setReadTimeoutMs(120_001);
        assertThrows(IllegalArgumentException.class, () -> new ConfigCenterRemoteClient(settings));
    }

    @Test
    void timesOutOnSlowRemoteWithoutReturningAnEmptyRevision() throws Exception {
        HttpServer server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        CountDownLatch requestStarted = new CountDownLatch(1);
        server.createContext("/api/config/v1/sync/revision", exchange -> {
            requestStarted.countDown();
            try {
                Thread.sleep(750);
                byte[] bytes = "{\"epoch\":\"a\",\"revision\":1}".getBytes(StandardCharsets.UTF_8);
                exchange.sendResponseHeaders(200, bytes.length);
                exchange.getResponseBody().write(bytes);
            } catch (InterruptedException ex) {
                Thread.currentThread().interrupt();
            } catch (IOException ignored) {
                // The client may already have timed out and closed the connection.
            } finally {
                exchange.close();
            }
        });
        server.start();
        try {
            ConfigClientProperties settings = new ConfigClientProperties();
            settings.setBaseUrl("http://127.0.0.1:" + server.getAddress().getPort());
            settings.setConnectTimeoutMs(1000);
            settings.setReadTimeoutMs(150);
            ConfigCenterRemoteClient remote = new ConfigCenterRemoteClient(settings);
            assertThrows(ResourceAccessException.class, remote::revision);
            assertTrue(requestStarted.await(2, TimeUnit.SECONDS));
        } finally {
            server.stop(0);
        }
    }

    @Test
    void rejectsEmptyRevisionResponse() throws Exception {
        HttpServer server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/api/config/v1/sync/revision", exchange -> {
            exchange.sendResponseHeaders(204, -1);
            exchange.close();
        });
        server.start();
        try {
            ConfigClientProperties settings = new ConfigClientProperties();
            settings.setBaseUrl("http://127.0.0.1:" + server.getAddress().getPort());
            ConfigCenterRemoteClient remote = new ConfigCenterRemoteClient(settings);
            assertThrows(IllegalStateException.class, remote::revision);
        } finally {
            server.stop(0);
        }
    }
}
