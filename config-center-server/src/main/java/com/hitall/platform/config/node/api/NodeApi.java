package com.hitall.platform.config.node.api;

import com.hitall.platform.config.node.dto.CreateNodeRequest;
import com.hitall.platform.config.node.dto.NodePage;
import com.hitall.platform.config.node.dto.NodeSummary;
import com.hitall.platform.config.node.dto.UpdateNodeRequest;
import com.hitall.platform.config.node.model.ConfigNode;
import com.hitall.platform.config.node.service.ConfigNodeService;

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
public class NodeApi {
    private final ConfigNodeService service;

    public NodeApi(ConfigNodeService service) {
        this.service = service;
    }

    @GetMapping("/admin/nodes")
    public List<ConfigNode> list() {
        return service.list();
    }

    @GetMapping("/admin/nodes/tree")
    public List<NodeSummary> treeChildren(
            @RequestParam(defaultValue = "0") long parentId) {
        return service.treeChildren(parentId);
    }

    @GetMapping("/admin/nodes/children")
    public NodePage children(
            @RequestParam long parentId,
            @RequestParam(defaultValue = "1") int page,
            @RequestParam(defaultValue = "20") int size,
            @RequestParam(defaultValue = "") String keyword) {
        return service.children(parentId, page, size, keyword);
    }

    @GetMapping("/admin/nodes/{id}")
    public ConfigNode detail(@PathVariable long id) {
        return service.get(id);
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
