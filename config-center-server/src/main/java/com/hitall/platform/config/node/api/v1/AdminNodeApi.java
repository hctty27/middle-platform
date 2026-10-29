package com.hitall.platform.config.node.api.v1;

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

/** V1 管理端节点 API；兼容历史 /api/v1/admin/nodes 路由。 */
@RestController
@RequestMapping({"/api/config/v1/admin/nodes", "/api/v1/admin/nodes"})
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


    // v1 规范路径；兼容旧版的 GET /children?parentId=...
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
