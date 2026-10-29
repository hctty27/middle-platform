package com.hitall.platform.client.config;

import org.springframework.boot.context.properties.ConfigurationProperties;

/**
 * 接入服务的配置中心连接参数。
 * baseUrl 指向配置中心，webhookSecret 用于验证回调请求签名。
 */
@ConfigurationProperties(prefix = "config.client")
public class ConfigClientProperties {
    private boolean enabled;
    private String baseUrl = "http://localhost:8080";
    private String webhookSecret = "";
    private int connectTimeoutMs = 2000;
    private int readTimeoutMs = 15000;

    public boolean isEnabled() { return enabled; }
    public void setEnabled(boolean enabled) { this.enabled = enabled; }
    public String getBaseUrl() { return baseUrl; }
    public void setBaseUrl(String baseUrl) { this.baseUrl = baseUrl; }
    public String getWebhookSecret() { return webhookSecret; }
    public void setWebhookSecret(String webhookSecret) { this.webhookSecret = webhookSecret; }
    public int getConnectTimeoutMs() { return connectTimeoutMs; }
    public void setConnectTimeoutMs(int connectTimeoutMs) { this.connectTimeoutMs = connectTimeoutMs; }
    public int getReadTimeoutMs() { return readTimeoutMs; }
    public void setReadTimeoutMs(int readTimeoutMs) { this.readTimeoutMs = readTimeoutMs; }
}
