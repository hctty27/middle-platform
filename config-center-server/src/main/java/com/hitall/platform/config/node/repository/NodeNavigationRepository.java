package com.hitall.platform.config.node.repository;

import java.sql.ResultSet;
import java.sql.SQLException;
import java.util.ArrayList;
import java.util.List;

import org.springframework.http.HttpStatus;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Repository;
import org.springframework.web.server.ResponseStatusException;

/**
 * 搜索与路径定位：直接按四个祖先 ID 关联，避免递归查询父节点。
 */
@Repository
public class NodeNavigationRepository {
    private static final String PATH_QUERY = """
            SELECT n.id AS n_id, n.node_type AS n_type, n.code AS n_code,
                   n.name AS n_name, n.status AS n_status,
                   g.id AS g_id, g.node_type AS g_type, g.code AS g_code,
                   g.name AS g_name, g.status AS g_status,
                   r.id AS r_id, r.node_type AS r_type, r.code AS r_code,
                   r.name AS r_name, r.status AS r_status,
                   sg.id AS sg_id, sg.node_type AS sg_type, sg.code AS sg_code,
                   sg.name AS sg_name, sg.status AS sg_status,
                   c.id AS c_id, c.node_type AS c_type, c.code AS c_code,
                   c.name AS c_name, c.status AS c_status
            FROM config_node n
            LEFT JOIN config_node g ON g.id = n.game_id
            LEFT JOIN config_node r ON r.id = n.region_id
            LEFT JOIN config_node sg ON sg.id = n.server_group_id
            LEFT JOIN config_node c ON c.id = n.channel_id
            """;

    private final JdbcTemplate jdbc;

    public NodeNavigationRepository(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    public List<NodeMatch> search(String keyword,
                                  int limit) {
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

    public NodeMatch path(long id) {
        return jdbc.query(PATH_QUERY + " WHERE n.id = ?",
                        (rs, row) -> readMatch(rs), id)
                .stream().findFirst()
                .orElseThrow(() -> new ResponseStatusException(HttpStatus.NOT_FOUND, "Node not found"));
    }

    private static NodeMatch readMatch(ResultSet rs) throws SQLException {
        List<NodeItem> path = new ArrayList<>();
        for (String prefix : List.of("g", "r", "sg", "c", "n")) {
            long id = rs.getLong(prefix + "_id");
            if (rs.wasNull()) continue;
            String type = rs.getString(prefix + "_type");
            long parentId = path.isEmpty() ? 0L : path.getLast().id();
            path.add(new NodeItem(id, parentId, type, rs.getString(prefix + "_code"),
                    rs.getString(prefix + "_name"), rs.getInt(prefix + "_status"),
                    "server_group".equals(type) || "channel".equals(type) || "sdk_app".equals(type)));
        }
        return new NodeMatch(path.getLast(), path);
    }

    public record NodeItem(long id, long parentId, String nodeType,
                           String code, String name, int status, boolean leaf) {}
    public record NodeMatch(NodeItem node, List<NodeItem> path) {}
}
