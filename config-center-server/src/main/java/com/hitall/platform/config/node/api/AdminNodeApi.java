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

/** 配置管理节点 API，仅暴露统一的无版本路由。 */
@RestController
@RequestMapping("/api/config/admin/nodes")
public class AdminNodeApi {
    private final ConfigNodeService service;

    public AdminNodeApi(ConfigNodeService service) {
        this.service = service;
    }

    @GetMapping("")
    public List<ConfigNode> list() {
        return service.list();
    }

    @GetMapping("/tree")
    public List<NodeSummary> treeChildren(
            @RequestParam(defaultValue = "0") long parentId) {
        return service.treeChildren(parentId);
    }


    // 规范子节点分页路径；保留原有 GET /children?parentId=... 兼容
    @GetMapping("/{id}/children")
    public NodePage childrenById(
            @PathVariable("id") long parentId,
            @RequestParam(defaultValue = "1") int page,
            @RequestParam(defaultValue = "20") int size,
            @RequestParam(defaultValue = "") String keyword) {
        return service.children(parentId, page, size, keyword);
    }

    @GetMapping("/children")
    public NodePage children(
            @RequestParam long parentId,
            @RequestParam(defaultValue = "1") int page,
            @RequestParam(defaultValue = "20") int size,
            @RequestParam(defaultValue = "") String keyword) {
        return service.children(parentId, page, size, keyword);
    }

    @GetMapping("/{id}")
    public ConfigNode detail(@PathVariable long id) {
        return service.get(id);
    }

    @PostMapping("")
    @ResponseStatus(HttpStatus.CREATED)
    public ConfigNode create(@Valid @RequestBody CreateNodeRequest request) {
        return service.create(request);
    }

    @PutMapping("/{id}")
    public ConfigNode update(@PathVariable long id, @Valid @RequestBody UpdateNodeRequest request) {
        return service.update(id, request);
    }

    @DeleteMapping("/{id}")
    @ResponseStatus(HttpStatus.NO_CONTENT)
    public void delete(@PathVariable long id) {
        service.delete(id);
    }

}
