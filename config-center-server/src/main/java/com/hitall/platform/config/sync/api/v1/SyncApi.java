package com.hitall.platform.config.sync.api.v1;

import com.hitall.platform.config.node.service.ConfigNodeService;
import com.hitall.platform.config.sync.dto.ChangeEvent;
import com.hitall.platform.config.sync.dto.RevisionInfo;

import java.util.List;
import org.springframework.web.bind.annotation.*;

/**
 * 向客户端公开版本游标、增量事件和全量快照。
 */
@RestController
@RequestMapping({"/api/config/v1/sync", "/api/v1/sync"})
public class SyncApi {
    private final ConfigNodeService service;

    public SyncApi(ConfigNodeService service) {
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
