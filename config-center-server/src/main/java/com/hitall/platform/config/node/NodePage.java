package com.hitall.platform.config.node;

import java.util.List;

/** 直属子节点分页。 */
public record NodePage(List<NodeSummary> items, long total, int page, int size) {}
