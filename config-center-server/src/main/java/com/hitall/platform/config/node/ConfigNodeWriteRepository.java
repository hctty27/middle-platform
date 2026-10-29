package com.hitall.platform.config.node;

import java.sql.PreparedStatement;
import java.sql.Statement;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.jdbc.support.GeneratedKeyHolder;
import org.springframework.jdbc.support.KeyHolder;
import org.springframework.stereotype.Repository;

/** 节点写入和内容等价性查询，不承担业务校验与事务编排。 */
@Repository
public class ConfigNodeWriteRepository {
    private final JdbcTemplate jdbc;

    public ConfigNodeWriteRepository(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    public long insert(CreateNodeRequest input, NodeAncestry ancestry) {
        KeyHolder key = new GeneratedKeyHolder();
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
        return key.getKey().longValue();
    }

    public boolean unchanged(long id, UpdateNodeRequest input) {
        Boolean unchanged = jdbc.queryForObject("""
                SELECT (name <=> ?) AND (status <=> ?) AND (remark <=> ?)
                       AND (config = CAST(? AS JSON))
                  FROM config_node WHERE id=?
                """, Boolean.class, input.name(), input.status(), input.remark(), input.config(), id);
        return Boolean.TRUE.equals(unchanged);
    }

    public void update(long id, UpdateNodeRequest input) {
        jdbc.update("UPDATE config_node SET name=?,status=?,config=?,remark=?,updated_at=UTC_TIMESTAMP(6) WHERE id=?",
                input.name(), input.status(), input.config(), input.remark(), id);
    }

    public void delete(long id) {
        jdbc.update("DELETE FROM config_node WHERE id=?", id);
    }

    public boolean validJson(String json) {
        Integer isValid = jdbc.queryForObject("SELECT JSON_VALID(?)", Integer.class, json);
        return isValid != null && isValid == 1;
    }
}
