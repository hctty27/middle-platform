package com.hitall.platform.config.webhook;

import java.net.URI;
import java.util.Arrays;
import java.util.List;
import java.util.Set;
import java.util.stream.Collectors;
import jakarta.validation.Valid;
import jakarta.validation.constraints.NotBlank;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.http.HttpStatus;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Component;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.server.ResponseStatusException;

/**
 * 接入方订阅配置管理接口。
 * 创建订阅者时校验回调目标，并初始化该订阅者的投递状态。
 */
@RestController
@RequestMapping("/api/v1/admin/subscribers")
public class SubscriberController {
    private final JdbcTemplate jdbc;
    private final Set<String> allowedHosts;

    public SubscriberController(JdbcTemplate jdbc,
            @Value("${config.webhook.allowed-hosts:localhost,127.0.0.1}") String hosts) {
        this.jdbc = jdbc;
        this.allowedHosts = Arrays.stream(hosts.split(","))
                .map(host -> host.trim().toLowerCase()).collect(Collectors.toSet());
    }

    @GetMapping
    public List<SubscriberView> list() {
        return jdbc.query("""
                SELECT s.id,s.code,s.callback_url,s.enabled,d.acknowledged_revision,
                       d.failure_count,d.last_error
                FROM config_subscriber s JOIN config_delivery_state d ON d.subscriber_id=s.id
                ORDER BY s.id
                """,
                (rs, row) -> new SubscriberView(rs.getLong(1), rs.getString(2),
                        rs.getString(3), rs.getBoolean(4), rs.getLong(5),
                        rs.getInt(6), rs.getString(7)));
    }

    // 回调仅允许白名单主机；非本机地址必须使用 HTTPS，避免任意回调目标。
    @PostMapping
    @ResponseStatus(HttpStatus.CREATED)
    @Transactional
    public void create(@Valid @RequestBody CreateSubscriber request) {
        URI uri;
        try {
            uri = URI.create(request.callbackUrl());
        } catch (IllegalArgumentException ex) {
            throw new ResponseStatusException(HttpStatus.BAD_REQUEST, "Invalid callback URI");
        }
        String host = uri.getHost() == null ? "" : uri.getHost().toLowerCase();
        String scheme = uri.getScheme() == null ? "" : uri.getScheme();
        if (!allowedHosts.contains(host) || uri.getUserInfo() != null ||
                !(scheme.equalsIgnoreCase("https") ||
                        (scheme.equalsIgnoreCase("http") &&
                                Set.of("localhost", "127.0.0.1").contains(host)))) {
            throw new ResponseStatusException(HttpStatus.BAD_REQUEST, "Callback host not allowlisted or HTTPS required");
        }
        jdbc.update("INSERT INTO config_subscriber(code,callback_url,webhook_secret,enabled,created_at,updated_at) " +
                        "VALUES(?,?,?,1,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))",
                request.code(), request.callbackUrl(), request.secret());
        Long id = jdbc.queryForObject("SELECT LAST_INSERT_ID()", Long.class);
        // 为新订阅者建立独立游标，后续由投递任务判断是否有未通知版本。
        jdbc.update("INSERT INTO config_delivery_state(subscriber_id,next_attempt_at,updated_at) " +
                "VALUES(?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", id);
    }

    public record CreateSubscriber(@NotBlank String code,
                                   @NotBlank String callbackUrl,
                                   @NotBlank String secret) {}
    public record SubscriberView(long id, String code, String callbackUrl,
                                 boolean enabled, long acknowledgedRevision,
                                 int failures, String lastError) {}
}
