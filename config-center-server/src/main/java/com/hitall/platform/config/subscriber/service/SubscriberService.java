package com.hitall.platform.config.subscriber.service;

import java.net.URI;
import java.util.Arrays;
import java.util.List;
import com.hitall.platform.config.subscriber.dto.CreateSubscriber;
import com.hitall.platform.config.subscriber.dto.SubscriberView;
import com.hitall.platform.config.subscriber.repository.SubscriberRepository;
import java.util.Set;
import java.util.stream.Collectors;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.http.HttpStatus;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.stereotype.Service;
import org.springframework.web.server.ResponseStatusException;

/**
 * 接入方订阅配置管理接口。
 * 创建订阅者时校验回调目标，并初始化该订阅者的投递状态。
 */
@Service
public class SubscriberService {
    private final SubscriberRepository repository;
    private final Set<String> allowedHosts;

    public SubscriberService(SubscriberRepository repository,
            @Value("${config.webhook.allowed-hosts:localhost,127.0.0.1}") String hosts) {
        this.repository = repository;
        this.allowedHosts = Arrays.stream(hosts.split(","))
                .map(host -> host.trim().toLowerCase()).collect(Collectors.toSet());
    }

    public List<SubscriberView> list() {
        return repository.list();
    }

    // 回调仅允许白名单主机；非本机地址必须使用 HTTPS，避免任意回调目标。
    @Transactional
    public void create(CreateSubscriber request) {
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
        repository.create(request);
    }

}

