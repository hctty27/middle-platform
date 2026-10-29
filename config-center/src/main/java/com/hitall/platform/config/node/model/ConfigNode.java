package com.hitall.platform.config.node.model;

import java.time.LocalDateTime;
import com.fasterxml.jackson.annotation.JsonProperty;

/**
 * 固定五级配置节点，只存储祖先 ID；parentId 是兼容前端的计算属性。
 */
public record ConfigNode(
        long id,
        String nodeType,
        Long gameId,
        Long regionId,
        Long serverGroupId,
        Long channelId,
        String code,
        String name,
        int status,
        String config,
        String remark,
        LocalDateTime createdAt,
        LocalDateTime updatedAt
) {
    @JsonProperty("parentId")
    public long parentId() {
        return parentId(nodeType, gameId, regionId, serverGroupId, channelId);
    }

    public static long parentId(String nodeType, Long gameId, Long regionId,
                                Long serverGroupId, Long channelId) {
        return switch (nodeType) {
            case "game" -> 0L;
            case "region" -> gameId;
            case "server_group" -> regionId;
            case "channel" -> serverGroupId;
            case "sdk_app" -> channelId;
            default -> throw new IllegalArgumentException("Unknown node type: " + nodeType);
        };
    }
}
