package com.hctty.platform.config.node;

import java.util.List;

import jakarta.validation.Valid;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.*;

/**
 * 配置节点管理与读取接口。
 * 管理端执行增删改，接入端可按 ID 查询节点最新状态。
 */
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

    // 增量同步按变更 ID 批量读取当前节点；已删除节点不会出现在结果中。
    @PostMapping("/config/nodes/batch-get")
    public List<ConfigNode> batchGet(@RequestBody BatchGetRequest request) {
        return service.batchGet(request.ids());
    }

    public record BatchGetRequest(List<Long> ids) {}
}
