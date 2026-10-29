package com.hitall.platform.config.node.repository;

import com.hitall.platform.config.node.dto.NodePage;
import com.hitall.platform.config.node.dto.NodeSummary;
import com.hitall.platform.config.node.model.ConfigNode;

import java.util.ArrayList;
import java.util.List;
import java.util.Optional;
import org.springframework.http.HttpStatus;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.jdbc.core.RowMapper;
import org.springframework.stereotype.Repository;
import org.springframework.web.server.ResponseStatusException;

/** 对配置节点进行查询、分页及路径范围解析。 */
@Repository
public class ConfigNodeQueryRepository {
    private static final RowMapper<ConfigNode> NODE_MAPPER = (rs, row) -> new ConfigNode(
            rs.getLong("id"),
            rs.getString("node_type"),
            rs.getObject("game_id", Long.class),
            rs.getObject("region_id", Long.class),
            rs.getObject("server_group_id", Long.class),
            rs.getObject("channel_id", Long.class),
            rs.getString("code"),
            rs.getString("name"),
            rs.getInt("status"),
            rs.getString("config"),
            rs.getString("remark"),
            rs.getTimestamp("created_at").toLocalDateTime(),
            rs.getTimestamp("updated_at").toLocalDateTime()
    );

    private static final RowMapper<NodeSummary> SUMMARY_MAPPER = (rs, row) -> {
        String type = rs.getString("node_type");
        return new NodeSummary(
                rs.getLong("id"),
                ConfigNode.parentId(type, rs.getObject("game_id", Long.class),
                        rs.getObject("region_id", Long.class),
                        rs.getObject("server_group_id", Long.class),
                        rs.getObject("channel_id", Long.class)),
                type, rs.getString("code"), rs.getString("name"),
                rs.getInt("status"), "server_group".equals(type)
        );
    };

    private static final String SUMMARY_COLUMNS =
            "n.id,n.node_type,n.game_id,n.region_id,n.server_group_id,n.channel_id,n.code,n.name,n.status";


    private final JdbcTemplate jdbc;

    public ConfigNodeQueryRepository(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    public List<ConfigNode> list() {
        return jdbc.query("SELECT * FROM config_node ORDER BY id", NODE_MAPPER);
    }

    /**
     * 根据节点类型解析直属子项的固定层级条件，不再读取 parent_id 列。
     */
    private ChildScope childScope(long parentId) {
        validateParentId(parentId);
        if (parentId == 0) return new ChildScope("n.node_type='game'", List.of());
        List<String> types = jdbc.query("SELECT node_type FROM config_node WHERE id=?",
                (rs, row) -> rs.getString(1), parentId);
        if (types.isEmpty()) throw new ResponseStatusException(HttpStatus.NOT_FOUND, "Parent not found");
        return switch (types.getFirst()) {
            case "game" -> new ChildScope("n.node_type='region' AND n.game_id=?", List.of(parentId));
            case "region" -> new ChildScope("n.node_type='server_group' AND n.region_id=?", List.of(parentId));
            case "server_group" -> new ChildScope("n.node_type='channel' AND n.server_group_id=?", List.of(parentId));
            case "channel" -> new ChildScope("n.node_type='sdk_app' AND n.channel_id=?", List.of(parentId));
            case "sdk_app" -> new ChildScope("1=0", List.of());
            default -> throw badRequest("Invalid parent node type");
        };
    }

    private record ChildScope(String condition, List<Object> params) {}

    /**
     * 管理端导航仅加载前三层的轻量节点，不读取 JSON 配置。
     */
    public List<NodeSummary> treeChildren(long parentId) {
        ChildScope scope = childScope(parentId);
        return jdbc.query("SELECT " + SUMMARY_COLUMNS +
                " FROM config_node n WHERE " + scope.condition() +
                " AND n.node_type IN ('game','region','server_group') ORDER BY n.id",
                SUMMARY_MAPPER, scope.params().toArray());
    }

    /**
     * 仅查询直属子项，支持关键词过滤及服务端分页。
     */
    public NodePage children(long parentId, int page, int size, String keyword) {
        if (page < 1 || size < 1 || size > 100) throw badRequest("Invalid pagination");
        String word = keyword == null ? "" : keyword.trim();
        if (word.length() > 128) throw badRequest("Keyword too long");

        ChildScope scope = childScope(parentId);
        String condition = " FROM config_node n WHERE " + scope.condition();
        List<Object> params = new ArrayList<>(scope.params());
        if (!word.isEmpty()) {
            condition += " AND (n.name LIKE ? ESCAPE '!' OR n.code LIKE ? ESCAPE '!')";
            String pattern = "%" + word.replace("!", "!!").replace("%", "!%").replace("_", "!_") + "%";
            params.add(pattern);
            params.add(pattern);
        }

        Long total = jdbc.queryForObject("SELECT COUNT(*)" + condition, Long.class, params.toArray());
        List<Object> pageParams = new ArrayList<>(params);
        pageParams.add(size);
        pageParams.add(((long) page - 1) * size);
        List<NodeSummary> items = jdbc.query("SELECT " + SUMMARY_COLUMNS + condition +
                        " ORDER BY n.id LIMIT ? OFFSET ?", SUMMARY_MAPPER, pageParams.toArray());
        return new NodePage(items, total == null ? 0L : total, page, size);
    }

    private static void validateParentId(long parentId) {
        if (parentId < 0) throw badRequest("Invalid parentId");
    }





    public ConfigNode get(long id) {
        return jdbc.query("SELECT * FROM config_node WHERE id=?", NODE_MAPPER, id)
                .stream().findFirst()
                .orElseThrow(() -> new ResponseStatusException(HttpStatus.NOT_FOUND, "Node not found"));
    }

    public List<ConfigNode> batchGet(List<Long> ids) {
        if (ids == null || ids.isEmpty()) return List.of();
        if (ids.size() > 500) throw badRequest("Maximum 500 IDs");
        String placeholders = String.join(",", java.util.Collections.nCopies(ids.size(), "?"));
        return jdbc.query("SELECT * FROM config_node WHERE id IN (" + placeholders + ") ORDER BY id",
                NODE_MAPPER, ids.toArray());
    }


    public Optional<ConfigNode> find(long id) {
        return jdbc.query("SELECT * FROM config_node WHERE id=?", NODE_MAPPER, id).stream().findFirst();
    }

    public boolean hasChildren(long id) {
        ChildScope scope = childScope(id);
        Long count = jdbc.queryForObject("SELECT COUNT(*) FROM config_node n WHERE " + scope.condition(),
                Long.class, scope.params().toArray());
        return count != null && count > 0;
    }

    private static ResponseStatusException badRequest(String message) {
        return new ResponseStatusException(HttpStatus.BAD_REQUEST, message);
    }
}
