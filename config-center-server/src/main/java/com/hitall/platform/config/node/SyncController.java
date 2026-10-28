package com.hitall.platform.config.node;

import java.util.List;
import org.springframework.web.bind.annotation.*;

/**
 * 向客户端公开版本游标、增量事件和全量快照。
 */
@RestController
@RequestMapping("/api/v1/sync")
public class SyncController {
    private final ConfigNodeService service;

    public SyncController(ConfigNodeService service) {
        this.service = service;
    }

    @GetMapping("/revision")
    public RevisionInfo revision() {
        return service.revision();
    }

    // 客户端从 afterRevision 之后拉取事件，按版本号升序分页。
    @GetMapping("/changes")
    public List<ChangeEvent> changes(@RequestParam(defaultValue = "0") long afterRevision,
                                     @RequestParam(defaultValue = "200") int limit) {
        return service.changes(afterRevision, limit);
    }

    // 当版本断档或 epoch 改变时，客户端使用快照重建本地副本。
    @GetMapping("/snapshot")
    public ConfigNodeService.ConfigSnapshot snapshot() {
        return service.snapshot();
    }
}
