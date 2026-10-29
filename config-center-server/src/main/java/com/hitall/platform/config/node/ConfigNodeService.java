package com.hitall.platform.config.node;

import java.util.List;
import java.util.Map;

import org.springframework.dao.DuplicateKeyException;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Isolation;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.server.ResponseStatusException;

/**
 * 配置节点的读写与版本管理。
 * 配置变更与全局版本、变更日志在同一事务中提交，供接入方增量同步。
 */
@Service
public class ConfigNodeService {
    // 各节点类型的合法父类型；game 为根节点，其 parentId 必须为 0。
    private static final Map<String, String> PARENTS = Map.of(
            "region", "game",
            "server_group", "region",
            "channel", "server_group",
            "sdk_app", "channel"
    );

    private final ConfigNodeQueryRepository queries;
    private final ConfigNodeWriteRepository writes;
    private final RevisionRepository revisions;

    public ConfigNodeService(ConfigNodeQueryRepository queries, ConfigNodeWriteRepository writes,
                             RevisionRepository revisions) {
        this.queries = queries;
        this.writes = writes;
        this.revisions = revisions;
    }

    public List<ConfigNode> list() {
        return queries.list();
    }

    public List<NodeSummary> treeChildren(long parentId) {
        return queries.treeChildren(parentId);
    }

    public NodePage children(long parentId, int page, int size, String keyword) {
        return queries.children(parentId, page, size, keyword);
    }

    public ConfigNode get(long id) {
        return queries.get(id);
    }

    public List<ConfigNode> batchGet(List<Long> ids) {
        return queries.batchGet(ids);
    }

    /**
     * 创建配置节点，校验层级关系后写入节点，并记录一次 UPSERT 变更。
     */
    @Transactional
    public ConfigNode create(CreateNodeRequest input) {
        validateFields(input.name(), input.status(), input.config(), input.remark());
        if (input.code() == null || input.code().isBlank() || input.code().length() > 128) {
            throw badRequest("Invalid code");
        }
        revisions.lockRevision();
        NodeAncestry ancestry = ancestryFromParent(input.parentId(), input.nodeType());
        long id;
        try {
            id = writes.insert(input, ancestry);
        } catch (DuplicateKeyException exception) {
            throw new ResponseStatusException(HttpStatus.CONFLICT, "Duplicate sibling code", exception);
        }
        revisions.recordChange(id, "UPSERT");
        return get(id);
    }

    /**
     * 更新节点的可编辑属性；不允许通过该接口改变层级和节点编码。
     */
    @Transactional
    public ConfigNode update(long id, UpdateNodeRequest input) {
        validateFields(input.name(), input.status(), input.config(), input.remark());
        revisions.lockRevision();
        ConfigNode current = get(id);
        if (writes.unchanged(id, input)) return current;

        writes.update(id, input);
        revisions.recordChange(id, "UPSERT");
        return get(id);
    }

    /**
     * 删除叶子节点并记录 DELETE 事件；存在子节点时拒绝删除。
     */
    @Transactional
    public void delete(long id) {
        revisions.lockRevision();
        get(id);
        if (queries.hasChildren(id)) {
            throw new ResponseStatusException(HttpStatus.CONFLICT, "Delete children first");
        }
        writes.delete(id);
        revisions.recordChange(id, "DELETE");
    }

    /**
     * 读取同一事务视图下的版本号和全部节点，用于客户端全量初始化或恢复。
     */
    @Transactional(readOnly = true, isolation = Isolation.REPEATABLE_READ)
    public ConfigSnapshot snapshot() {
        RevisionInfo info = revision();
        return new ConfigSnapshot(info.epoch(), info.revision(), list());
    }

    public RevisionInfo revision() {
        return revisions.revision();
    }

    public List<ChangeEvent> changes(long after, int limit) {
        if (after < 0 || limit < 1 || limit > 500) throw badRequest("Invalid cursor or limit");
        return revisions.changes(after, limit);
    }

    // 父节点只作为创建接口参数；数据库保存的是由服务端推导的完整祖先路径。
    private NodeAncestry ancestryFromParent(long parentId, String nodeType) {
        if ("game".equals(nodeType)) {
            if (parentId != 0) throw badRequest("Game parentId must be 0");
            return new NodeAncestry(null, null, null, null);
        }
        String expected = PARENTS.get(nodeType);
        if (expected == null || parentId <= 0) throw badRequest("Invalid nodeType/parentId");
        List<ConfigNode> parents = queries.find(parentId).stream().toList();
        if (parents.isEmpty() || !expected.equals(parents.getFirst().nodeType())) {
            throw badRequest("Invalid parent node type");
        }
        ConfigNode parent = parents.getFirst();
        return switch (nodeType) {
            case "region" -> new NodeAncestry(parent.id(), null, null, null);
            case "server_group" -> new NodeAncestry(parent.gameId(), parent.id(), null, null);
            case "channel" -> new NodeAncestry(parent.gameId(), parent.regionId(), parent.id(), null);
            case "sdk_app" -> new NodeAncestry(parent.gameId(), parent.regionId(), parent.serverGroupId(), parent.id());
            default -> throw badRequest("Invalid node type");
        };
    }

    private void validateFields(String name, Integer status, String json, String remark) {
        if (name == null || name.isBlank() || name.length() > 128) throw badRequest("Invalid name");
        if (status == null || (status != 0 && status != 1)) throw badRequest("Status must be 0 or 1");
        if (remark != null && remark.length() > 500) throw badRequest("Remark too long");
        if (json == null || json.isBlank()) throw badRequest("Config must be JSON");
        if (!writes.validJson(json)) throw badRequest("Invalid JSON");
    }

    private static ResponseStatusException badRequest(String message) {
        return new ResponseStatusException(HttpStatus.BAD_REQUEST, message);
    }

    public record ConfigSnapshot(String epoch, long revision, List<ConfigNode> nodes) {}
}
