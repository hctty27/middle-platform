package com.hitall.platform.config.node;

import java.sql.ResultSet;
import java.sql.SQLException;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

import org.springframework.http.HttpStatus;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.server.ResponseStatusException;

/**
 * 管理端跨层级搜索与节点路径定位。只读取轻量字段，不返回 JSON 配置。
 */
@RestController
@RequestMapping("/api/v1/admin/nodes")
public class NodeNavigationController {
    private static final String PATH_QUERY = """
            SELECT n.id AS n_id,
            n.parent_id AS n_parent_id,
            n.node_type AS n_node_type,
            n.code AS n_code,
            n.name AS n_name,
            n.status AS n_status,
            p1.id AS p1_id,
            p1.parent_id AS p1_parent_id,
            p1.node_type AS p1_node_type,
            p1.code AS p1_code,
            p1.name AS p1_name,
            p1.status AS p1_status,
            p2.id AS p2_id,
            p2.parent_id AS p2_parent_id,
            p2.node_type AS p2_node_type,
            p2.code AS p2_code,
            p2.name AS p2_name,
            p2.status AS p2_status,
            p3.id AS p3_id,
            p3.parent_id AS p3_parent_id,
            p3.node_type AS p3_node_type,
            p3.code AS p3_code,
            p3.name AS p3_name,
            p3.status AS p3_status,
            p4.id AS p4_id,
            p4.parent_id AS p4_parent_id,
            p4.node_type AS p4_node_type,
            p4.code AS p4_code,
            p4.name AS p4_name,
            p4.status AS p4_status
            FROM config_node n
            LEFT JOIN config_node p1 ON p1.id = n.parent_id
            LEFT JOIN config_node p2 ON p2.id = p1.parent_id
            LEFT JOIN config_node p3 ON p3.id = p2.parent_id
            LEFT JOIN config_node p4 ON p4.id = p3.parent_id
            """;

    private final JdbcTemplate jdbc;

    public NodeNavigationController(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    @GetMapping("/search")
    public List<NodeMatch> search(@RequestParam String keyword,
                                  @RequestParam(defaultValue = "30") int limit) {
        String word = keyword == null ? "" : keyword.trim();
        if (word.isEmpty()) return List.of();
        if (word.length() > 128 || limit < 1 || limit > 50) {
            throw new ResponseStatusException(HttpStatus.BAD_REQUEST, "Invalid search params");
        }
        String pattern = "%" + word.replace("!", "!!").replace("%", "!%")
                .replace("_", "!_") + "%";
        return jdbc.query(PATH_QUERY + """
                WHERE (LOWER(n.name) LIKE LOWER(?) ESCAPE '!'
                    OR LOWER(n.code) LIKE LOWER(?) ESCAPE '!')
                ORDER BY CASE WHEN LOWER(n.code) = LOWER(?) THEN 0
                              WHEN LOWER(n.code) LIKE LOWER(?) ESCAPE '!' THEN 1
                              ELSE 2 END, n.id
                LIMIT ?
                """, (rs, row) -> readMatch(rs), pattern, pattern, word,
                word.replace("!", "!!").replace("%", "!%").replace("_", "!_") + "%", limit);
    }

    @GetMapping("/{id}/path")
    public NodeMatch path(@PathVariable long id) {
        return jdbc.query(PATH_QUERY + " WHERE n.id = ?",
                        (rs, row) -> readMatch(rs), id)
                .stream().findFirst()
                .orElseThrow(() -> new ResponseStatusException(HttpStatus.NOT_FOUND, "Node not found"));
    }

    private static NodeMatch readMatch(ResultSet rs) throws SQLException {
        List<NodeItem> path = new ArrayList<>();
        for (String prefix : List.of("n", "p1", "p2", "p3", "p4")) {
            long id = rs.getLong(prefix + "_id");
            if (rs.wasNull()) break;
            String nodeType = rs.getString(prefix + "_node_type");
            path.add(new NodeItem(
                    id, rs.getLong(prefix + "_parent_id"), nodeType,
                    rs.getString(prefix + "_code"), rs.getString(prefix + "_name"),
                    rs.getInt(prefix + "_status"),
                    "server_group".equals(nodeType) || "channel".equals(nodeType)
                            || "sdk_app".equals(nodeType)));
        }
        Collections.reverse(path);
        return new NodeMatch(path.getLast(), path);
    }

    public record NodeItem(long id, long parentId, String nodeType,
                           String code, String name, int status, boolean leaf) {}
    public record NodeMatch(NodeItem node, List<NodeItem> path) {}
}
