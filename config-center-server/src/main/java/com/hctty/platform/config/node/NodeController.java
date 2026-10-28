package com.hctty.platform.config.node;

import java.util.List;

import jakarta.validation.Valid;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api/v1")
public class NodeController {
    private final ConfigNodeService service;

    public NodeController(ConfigNodeService service) {
        this.service = service;
    }

    @GetMapping("/admin/nodes")
    public List<ConfigNode> list() {
        return service.list();
    }

    @PostMapping("/admin/nodes")
    @ResponseStatus(HttpStatus.CREATED)
    public ConfigNode create(@Valid @RequestBody CreateNodeRequest request) {
        return service.create(request);
    }

    @PutMapping("/admin/nodes/{id}")
    public ConfigNode update(@PathVariable long id, @Valid @RequestBody UpdateNodeRequest request) {
        return service.update(id, request);
    }

    @DeleteMapping("/admin/nodes/{id}")
    @ResponseStatus(HttpStatus.NO_CONTENT)
    public void delete(@PathVariable long id) {
        service.delete(id);
    }

    @GetMapping("/config/nodes/{id}")
    public ConfigNode get(@PathVariable long id) {
        return service.get(id);
    }

    @PostMapping("/config/nodes/batch-get")
    public List<ConfigNode> batchGet(@RequestBody BatchGetRequest request) {
        return service.batchGet(request.ids());
    }

    public record BatchGetRequest(List<Long> ids) {}
}
