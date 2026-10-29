package com.hitall.platform.config.webhook;

import java.util.List;
import jakarta.validation.Valid;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.*;

/** 接入方订阅管理接口，业务和持久化委托给服务层。 */
@RestController
@RequestMapping("/api/v1/admin/subscribers")
public class SubscriberController {
    private final SubscriberService service;

    public SubscriberController(SubscriberService service) {
        this.service = service;
    }

    @GetMapping
    public List<SubscriberService.SubscriberView> list() {
        return service.list();
    }

    @PostMapping
    @ResponseStatus(HttpStatus.CREATED)
    public void create(@Valid @RequestBody SubscriberService.CreateSubscriber request) {
        service.create(request);
    }
}
