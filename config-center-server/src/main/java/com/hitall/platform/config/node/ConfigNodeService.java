package com.hitall.platform.config.node;

import java.sql.PreparedStatement;
import java.sql.Statement;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

import org.springframework.dao.DuplicateKeyException;
import org.springframework.http.HttpStatus;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.jdbc.core.RowMapper;
import org.springframework.jdbc.support.GeneratedKeyHolder;
import org.springframework.jdbc.support.KeyHolder;
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

    public ConfigNodeService(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    public List<ConfigNode> list() {
        return jdbc.query("SELECT * FROM config_node ORDER BY id", NODE_MAPPER);
    }

    /**
     * 管理端导航仅加载前三层的轻量节点，不读取 JSON 配置。
     */
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

    public record NodeSummary(
            long id, long parentId, String nodeType, String code, String name, int status, boolean leaf
    ) {}

    public record NodePage(List<NodeSummary> items, long total, int page, int size) {}

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

    /**
     * 创建配置节点，校验层级关系后写入节点，并记录一次 UPSERT 变更。
     */
    @Transactional
    public ConfigNode create(CreateNodeRequest input) {
        validateFields(input.name(), input.status(), input.config(), input.remark());
        if (input.code() == null || input.code().isBlank() || input.code().length() > 128) {
            throw badRequest("Invalid code");
        }
        lockRevision();
        Ancestry ancestry = ancestryFromParent(input.parentId(), input.nodeType());
        KeyHolder key = new GeneratedKeyHolder();
        try {
            jdbc.update(connection -> {
                PreparedStatement statement = connection.prepareStatement(
                        "INSERT INTO config_node(game_id,region_id,server_group_id,channel_id,node_type,code,name,status,config,remark,created_at,updated_at) " +
                        "VALUES (?,?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))",
                        Statement.RETURN_GENERATED_KEYS);
                statement.setObject(1, ancestry.gameId());
                statement.setObject(2, ancestry.regionId());
                statement.setObject(3, ancestry.serverGroupId());
                statement.setObject(4, ancestry.channelId());
                statement.setString(5, input.nodeType());
                statement.setString(6, input.code());
                statement.setString(7, input.name());
                statement.setInt(8, input.status());
                statement.setString(9, input.config());
                statement.setString(10, input.remark());
                return statement;
            }, key);
            } catch (DuplicateKeyException exception) {
            throw new ResponseStatusException(HttpStatus.CONFLICT, "Duplicate sibling code", exception);
        }
        long id = key.getKey().longValue();
        recordChange(id, "UPSERT");
        return get(id);
    }

    /**
     * 更新节点的可编辑属性；不允许通过该接口改变层级和节点编码。
     */
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

    /**
     * 删除叶子节点并记录 DELETE 事件；存在子节点时拒绝删除。
     */
    @Transactional
    public void delete(long id) {
        lockRevision();
        get(id);
        ChildScope scope = childScope(id);
        Long children = jdbc.queryForObject(
                "SELECT COUNT(*) FROM config_node n WHERE " + scope.condition(),
                Long.class, scope.params().toArray());
        if (children != null && children > 0) {
            throw new ResponseStatusException(HttpStatus.CONFLICT, "Delete children first");
        }
        jdbc.update("DELETE FROM config_node WHERE id=?", id);
        recordChange(id, "DELETE");
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
        return jdbc.queryForObject("SELECT epoch,revision FROM config_revision WHERE id=1",
                (rs, row) -> new RevisionInfo(rs.getString("epoch"), rs.getLong("revision")));
    }

    /**
     * 按版本顺序分页返回变更事件；调用方以 revision 为游标连续拉取。
     */
    public List<ChangeEvent> changes(long after, int limit) {
        if (after < 0 || limit < 1 || limit > 500) throw badRequest("Invalid cursor or limit");
        return jdbc.query("SELECT revision,node_id,operation,created_at FROM config_change_log " +
                        "WHERE revision>? ORDER BY revision LIMIT ?",
                (rs, row) -> new ChangeEvent(rs.getLong("revision"), rs.getLong("node_id"),
                        rs.getString("operation"), rs.getTimestamp("created_at").toLocalDateTime()),
                after, limit);
    }

    private record Ancestry(Long gameId, Long regionId, Long serverGroupId, Long channelId) {}

    // 父节点只作为创建接口参数；数据库保存的是由服务端推导的完整祖先路径。
    private Ancestry ancestryFromParent(long parentId, String nodeType) {
        if ("game".equals(nodeType)) {
            if (parentId != 0) throw badRequest("Game parentId must be 0");
            return new Ancestry(null, null, null, null);
        }
        String expected = PARENTS.get(nodeType);
        if (expected == null || parentId <= 0) throw badRequest("Invalid nodeType/parentId");
        List<ConfigNode> parents = jdbc.query("SELECT * FROM config_node WHERE id=?", NODE_MAPPER, parentId);
        if (parents.isEmpty() || !expected.equals(parents.getFirst().nodeType())) {
            throw badRequest("Invalid parent node type");
        }
        ConfigNode parent = parents.getFirst();
        return switch (nodeType) {
            case "region" -> new Ancestry(parent.id(), null, null, null);
            case "server_group" -> new Ancestry(parent.gameId(), parent.id(), null, null);
            case "channel" -> new Ancestry(parent.gameId(), parent.regionId(), parent.id(), null);
            case "sdk_app" -> new Ancestry(parent.gameId(), parent.regionId(), parent.serverGroupId(), parent.id());
            default -> throw badRequest("Invalid node type");
        };
    }

    private void validateFields(String name, Integer status, String json, String remark) {
        if (name == null || name.isBlank() || name.length() > 128) throw badRequest("Invalid name");
        if (status == null || (status != 0 && status != 1)) throw badRequest("Status must be 0 or 1");
        if (remark != null && remark.length() > 500) throw badRequest("Remark too long");
        if (json == null || json.isBlank()) throw badRequest("Config must be JSON");
        Integer isValid = jdbc.queryForObject("SELECT JSON_VALID(?)", Integer.class, json);
        if (isValid == null || isValid != 1) throw badRequest("Invalid JSON");
    }

    // 写事务先锁定单行版本记录，保证并发变更按顺序分配全局 revision。
    private long lockRevision() {
        return jdbc.queryForObject("SELECT revision FROM config_revision WHERE id=1 FOR UPDATE", Long.class);
    }

    // 版本递增与事件入库在调用方的写事务内完成，避免节点变更与日志脱节。
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
