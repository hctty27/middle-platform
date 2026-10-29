package com.hitall.platform.access.repository;

import com.hitall.platform.access.sync.ClientSynchronizer;

import org.springframework.jdbc.core.JdbcTemplate;
import com.hitall.platform.access.sync.ClientSynchronizer.Node;
import com.hitall.platform.access.sync.ClientSynchronizer.Snapshot;
import com.hitall.platform.access.sync.ClientSynchronizer.State;

/** 本地配置节点副本和同步游标的 JDBC 操作。写事务由同步编排层显式控制。 */
public class LocalConfigRepository {
    private final JdbcTemplate jdbc;

    public LocalConfigRepository(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    /** 不加行锁读取已提交的同步游标，网络请求前调用。 */
    public State readState() {
        return jdbc.queryForObject("SELECT epoch,revision FROM config_sync_state WHERE id=1",
                (rs, row) -> new State(rs.getString(1), rs.getLong(2)));
    }

    /** 仅在短写事务内调用，配合游标一致性检查。 */
    public State lockedState() {
        return jdbc.queryForObject("SELECT epoch,revision FROM config_sync_state WHERE id=1 FOR UPDATE",
                (rs, row) -> new State(rs.getString(1), rs.getLong(2)));
    }

    public State replaceSnapshot(Snapshot snapshot) {
        jdbc.update("DELETE FROM config_node");
        for (Node node : snapshot.nodes()) upsert(node);
        moveCursor(snapshot.epoch(), snapshot.revision());
        return new State(snapshot.epoch(), snapshot.revision());
    }

    public void delete(long id) {
        jdbc.update("DELETE FROM config_node WHERE id=?", id);
    }

    public void moveCursor(String epoch, long revision) {
        jdbc.update("UPDATE config_sync_state SET epoch=?,revision=?,updated_at=UTC_TIMESTAMP(6) WHERE id=1",
                epoch, revision);
    }

    public void upsert(Node node) {
        jdbc.update("""
                INSERT INTO config_node(id,game_id,region_id,server_group_id,node_type,code,name,status,config,remark,created_at,updated_at)
                VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
                ON DUPLICATE KEY UPDATE game_id=VALUES(game_id),region_id=VALUES(region_id),
                    server_group_id=VALUES(server_group_id),
                    node_type=VALUES(node_type),code=VALUES(code),name=VALUES(name),
                    status=VALUES(status),config=VALUES(config),remark=VALUES(remark),
                    created_at=VALUES(created_at),updated_at=VALUES(updated_at)
                """, node.id(), node.gameId(), node.regionId(), node.serverGroupId(),
                node.nodeType(), node.code(), node.name(), node.status(), node.config(), node.remark(),
                node.createdAt(), node.updatedAt());
    }


}
