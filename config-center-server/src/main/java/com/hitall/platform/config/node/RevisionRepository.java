package com.hitall.platform.config.node;

import java.util.List;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Repository;

/** 版本与变更日志持久化；使用调用方事务保证原子性。 */
@Repository
public class RevisionRepository {
    private final JdbcTemplate jdbc;

    public RevisionRepository(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    public RevisionInfo revision() {
        return jdbc.queryForObject("SELECT epoch,revision FROM config_revision WHERE id=1",
                (rs, row) -> new RevisionInfo(rs.getString("epoch"), rs.getLong("revision")));
    }

    public List<ChangeEvent> changes(long after, int limit) {
        return jdbc.query("SELECT revision,node_id,operation,created_at FROM config_change_log " +
                        "WHERE revision>? ORDER BY revision LIMIT ?",
                (rs, row) -> new ChangeEvent(rs.getLong("revision"), rs.getLong("node_id"),
                        rs.getString("operation"), rs.getTimestamp("created_at").toLocalDateTime()),
                after, limit);
    }

    // 写事务先锁定单行版本记录，保证并发变更按顺序分配全局 revision。
    public long lockRevision() {
        return jdbc.queryForObject("SELECT revision FROM config_revision WHERE id=1 FOR UPDATE", Long.class);
    }

    // 版本递增与事件入库在调用方的写事务内完成，避免节点变更与日志脱节。
    public void recordChange(long nodeId, String operation) {
        jdbc.update("UPDATE config_revision SET revision=revision+1 WHERE id=1");
        Long revision = jdbc.queryForObject("SELECT revision FROM config_revision WHERE id=1", Long.class);
        jdbc.update("INSERT INTO config_change_log(revision,node_id,operation,created_at) VALUES(?,?,?,UTC_TIMESTAMP(6))",
                revision, nodeId, operation);
    }


}
