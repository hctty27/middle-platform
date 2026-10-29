package com.hitall.platform.config.node.model;

/** 由合法的父节点层级推导出的祖先 ID，不包含节点自身。 */
public record NodeAncestry(Long gameId, Long regionId, Long serverGroupId, Long channelId) {}
