package com.hitall.platform.config.node.dto;

import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Size;

public record UpdateNodeRequest(
        @NotBlank @Size(max = 128) String name,
        @NotNull Integer status,
        @NotNull String config,
        @Size(max = 500) String remark
) {}
