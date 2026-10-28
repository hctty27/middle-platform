package com.hctty.platform.config.node;

import java.sql.PreparedStatement;
import java.sql.Statement;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.util.List;
import java.util.Map;

import org.springframework.dao.DataAccessException;
import org.springframework.http.HttpStatus;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.jdbc.core.RowMapper;
import org.springframework.jdbc.support.GeneratedKeyHolder;
import org.springframework.jdbc.support.KeyHolder;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Isolation;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.server.ResponseStatusException;

@Service
public class ConfigNodeService {
    private static final Map<String, String> PARENTS = Map.of(
            "region", "game",
            "server_group", "region",
            "channel", "server_group",
            "sdk_app", "channel"
    );

    private static final RowMapper<ConfigNode> NODE_MAPPER = (rs, row) -> new ConfigNode(
            rs.getLong("id"),
            rs.getLong("parent_id"),
            rs.getString("node_type"),
            rs.getString("code"),
            rs.getString("name"),
            rs.getInt("status"),
            rs.getString("config"),
            rs.getString("remark"),
            rs.getTimestamp("created_at").toLocalDateTime(),
            rs.getTimestamp("updated_at").toLocalDateTime()
    );

    private final JdbcTemplate jdbc;

    public ConfigNodeService(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    public List<ConfigNode> list() {
        return jdbc.query("SELECT * FROM config_node ORDER BY id", NODE_MAPPER);
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

    @Transactional
    public ConfigNode create(CreateNodeRequest input) {
        validateFields(input.name(), input.status(), input.config(), input.remark());
        if (input.code() == null || input.code().isBlank() || input.code().length() > 128) {
            throw badRequest("Invalid code");
        }
        lockRevision();
        validateParent(input.parentId(), input.nodeType());
        KeyHolder key = new GeneratedKeyHolder();
        jdbc.update(connection -> {
            PreparedStatement statement = connection.prepareStatement(
                    "INSERT INTO config_node(parent_id,node_type,code,name,status,config,remark,created_at,updated_at) " +
                    "VALUES (?,?,?,?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))",
                    Statement.RETURN_GENERATED_KEYS);
            statement.setLong(1, input.parentId());
            statement.setString(2, input.nodeType());
            statement.setString(3, input.code());
            statement.setString(4, input.name());
            statement.setInt(5, input.status());
            statement.setString(6, input.config());
            statement.setString(7, input.remark());
            return statement;
        }, key);
        long id = key.getKey().longValue();
        recordChange(id, "UPSERT");
        return get(id);
    }

    @Transactional
    public ConfigNode update(long id, UpdateNodeRequest input) {
        validateFields(input.name(), input.status(), input.config(), input.remark());
        lockRevision();
        get(id);
        jdbc.update("UPDATE config_node SET name=?,status=?,config=?,remark=?,updated_at=UTC_TIMESTAMP(6) WHERE id=?",
                input.name(), input.status(), input.config(), input.remark(), id);
        recordChange(id, "UPSERT");
        return get(id);
    }

    @Transactional
    public void delete(long id) {
        lockRevision();
        get(id);
        Long children = jdbc.queryForObject(
                "SELECT COUNT(*) FROM config_node WHERE parent_id=?", Long.class, id);
        if (children != null && children > 0) {
            throw new ResponseStatusException(HttpStatus.CONFLICT, "Delete children first");
        }
        jdbc.update("DELETE FROM config_node WHERE id=?", id);
        recordChange(id, "DELETE");
    }

    @Transactional(readOnly = true, isolation = Isolation.REPEATABLE_READ)
    public ConfigSnapshot snapshot() {
        RevisionInfo info = revision();
        return new ConfigSnapshot(info.epoch(), info.revision(), list());
    }

    public RevisionInfo revision() {
        return jdbc.queryForObject("SELECT epoch,revision FROM config_revision WHERE id=1",
                (rs, row) -> new RevisionInfo(rs.getString("epoch"), rs.getLong("revision")));
    }

    public List<ChangeEvent> changes(long after, int limit) {
        if (after < 0 || limit < 1 || limit > 500) throw badRequest("Invalid cursor or limit");
        return jdbc.query("SELECT revision,node_id,operation,created_at FROM config_change_log " +
                        "WHERE revision>? ORDER BY revision LIMIT ?",
                (rs, row) -> new ChangeEvent(rs.getLong("revision"), rs.getLong("node_id"),
                        rs.getString("operation"), rs.getTimestamp("created_at").toLocalDateTime()),
                after, limit);
    }

    private void validateParent(long parentId, String nodeType) {
        if ("game".equals(nodeType)) {
            if (parentId != 0) throw badRequest("Game parentId must be 0");
            return;
        }
        String expected = PARENTS.get(nodeType);
        if (expected == null || parentId <= 0) throw badRequest("Invalid nodeType/parentId");
        List<String> parents = jdbc.query("SELECT node_type FROM config_node WHERE id=?",
                (rs, row) -> rs.getString(1), parentId);
        if (parents.isEmpty() || !expected.equals(parents.getFirst())) {
            throw badRequest("Invalid parent node type");
        }
    }

    private void validateFields(String name, Integer status, String json, String remark) {
        if (name == null || name.isBlank() || name.length() > 128) throw badRequest("Invalid name");
        if (status == null || (status != 0 && status != 1)) throw badRequest("Status must be 0 or 1");
        if (remark != null && remark.length() > 500) throw badRequest("Remark too long");
        if (json == null || json.isBlank()) throw badRequest("Config must be JSON");
        Integer isValid = jdbc.queryForObject("SELECT JSON_VALID(?)", Integer.class, json);
        if (isValid == null || isValid != 1) throw badRequest("Invalid JSON");
    }

    private long lockRevision() {
        return jdbc.queryForObject("SELECT revision FROM config_revision WHERE id=1 FOR UPDATE", Long.class);
    }

    private void recordChange(long nodeId, String operation) {
        jdbc.update("UPDATE config_revision SET revision=revision+1 WHERE id=1");
        Long revision = jdbc.queryForObject("SELECT revision FROM config_revision WHERE id=1", Long.class);
        jdbc.update("INSERT INTO config_change_log(revision,node_id,operation,created_at) VALUES(?,?,?,UTC_TIMESTAMP(6))",
                revision, nodeId, operation);
    }

    private static ResponseStatusException badRequest(String message) {
        return new ResponseStatusException(HttpStatus.BAD_REQUEST, message);
    }

    public record ConfigSnapshot(String epoch, long revision, List<ConfigNode> nodes) {}
}
