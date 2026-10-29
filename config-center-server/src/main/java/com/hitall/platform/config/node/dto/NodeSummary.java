package com.hitall.platform.config.node.dto;

/** 配置管理导航使用的轻量节点信息。 */
public record NodeSummary(
            long id, long parentId, String nodeType, String code, String name, int status, boolean leaf
    ) {}
