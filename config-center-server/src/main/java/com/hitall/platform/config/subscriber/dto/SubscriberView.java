package com.hitall.platform.config.subscriber.dto;

/** 订阅者配置和投递状态（不暴露签名密钥）。 */
public record SubscriberView(long id, String code, String callbackUrl,
                             boolean enabled, long acknowledgedRevision,
                             int failures, String lastError) {}
