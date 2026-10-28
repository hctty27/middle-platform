package com.hitall.platform.config.node;

import java.time.LocalDateTime;

public record ConfigNode(
        long id,
        long parentId,
        String nodeType,
        String code,
        String name,
        int status,
        String config,
        String remark,
        LocalDateTime createdAt,
        LocalDateTime updatedAt
) {}
