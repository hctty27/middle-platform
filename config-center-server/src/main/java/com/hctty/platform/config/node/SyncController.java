package com.hctty.platform.config.node;

import java.util.List;
import org.springframework.web.bind.annotation.*;

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

    @GetMapping("/changes")
    public List<ChangeEvent> changes(@RequestParam(defaultValue = "0") long afterRevision,
                                     @RequestParam(defaultValue = "200") int limit) {
        return service.changes(afterRevision, limit);
    }

    @GetMapping("/snapshot")
    public ConfigNodeService.ConfigSnapshot snapshot() {
        return service.snapshot();
    }
}
