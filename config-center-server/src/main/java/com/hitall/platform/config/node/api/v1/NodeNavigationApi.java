package com.hitall.platform.config.node.api.v1;

import com.hitall.platform.config.node.repository.NodeNavigationRepository;

import java.util.List;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

/** 配置节点全局搜索与路径查询接口。 */
@RestController
@RequestMapping({"/api/config/v1/admin/nodes", "/api/v1/admin/nodes"})
public class NodeNavigationApi {
    private final NodeNavigationRepository repository;

    public NodeNavigationApi(NodeNavigationRepository repository) {
        this.repository = repository;
    }

    @GetMapping("/search")
    public List<NodeNavigationRepository.NodeMatch> search(@RequestParam String keyword,
            @RequestParam(defaultValue = "30") int limit) {
        return repository.search(keyword, limit);
    }

    @GetMapping("/{id}/path")
    public NodeNavigationRepository.NodeMatch path(@PathVariable long id) {
        return repository.path(id);
    }
}
