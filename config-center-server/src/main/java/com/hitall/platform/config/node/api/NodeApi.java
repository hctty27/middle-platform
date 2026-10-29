package com.hitall.platform.config.node.api;

import com.hitall.platform.config.node.model.ConfigNode;
import com.hitall.platform.config.node.service.ConfigNodeService;
import java.util.List;
import org.springframework.web.bind.annotation.*;

/** V1 客户端节点查询 API；无管理写接口。 */
@RestController
@RequestMapping({"/api/config/v1/nodes", "/api/v1/config/nodes"})
public class NodeApi {
    private final ConfigNodeService service;

    public NodeApi(ConfigNodeService service) {
        this.service = service;
    }

    @GetMapping("/{id}")
    public ConfigNode get(@PathVariable long id) {
        return service.get(id);
    }

    // 增量同步按变更 ID 批量读取当前节点；已删除节点不会出现在结果中。
    @PostMapping("/batch-get")
    public List<ConfigNode> batchGet(@RequestBody BatchGetRequest request) {
        return service.batchGet(request.ids());
    }

    public record BatchGetRequest(List<Long> ids) {}
}
