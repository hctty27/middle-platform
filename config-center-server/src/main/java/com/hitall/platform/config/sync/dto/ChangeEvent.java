package com.hitall.platform.config.sync.dto;

import java.time.LocalDateTime;

public record ChangeEvent(long revision, long nodeId, String operation, LocalDateTime createdAt) {}
