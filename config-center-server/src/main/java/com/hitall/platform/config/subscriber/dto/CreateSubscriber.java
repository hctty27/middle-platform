package com.hitall.platform.config.subscriber.dto;

import jakarta.validation.constraints.NotBlank;

/** 新增 Webhook 订阅者请求。 */
public record CreateSubscriber(@NotBlank String code,
                               @NotBlank String callbackUrl,
                               @NotBlank String secret) {}
