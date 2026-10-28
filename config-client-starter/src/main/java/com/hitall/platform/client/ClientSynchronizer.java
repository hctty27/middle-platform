package com.hitall.platform.client;

import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.stream.Collectors;

import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.client.RestClient;

/**
 * 将配置中心数据同步到接入服务的本地 MySQL 副本。
 * 优先增量同步；数据世代变化、事件断档时使用全量快照恢复。
 */
public class ClientSynchronizer {
    private final JdbcTemplate jdbc;
    private final RestClient remote;

    public ClientSynchronizer(JdbcTemplate jdbc, ConfigClientProperties properties) {
        this.jdbc = jdbc;
        this.remote = RestClient.builder().baseUrl(properties.getBaseUrl()).build();
    }

    /**
     * 同步远端最新配置及本地版本游标。
     * 本地更新置于事务中；同一实例内同步调用串行执行。
     */
    @Transactional
    public synchronized void synchronize() {
        State state = jdbc.queryForObject(
                "SELECT epoch,revision FROM config_sync_state WHERE id=1 FOR UPDATE",
                (rs, row) -> new State(rs.getString(1), rs.getLong(2)));
        Revision head = remote.get().uri("/api/v1/sync/revision").retrieve().body(Revision.class);
        if (head == null) throw new IllegalStateException("Missing remote revision");

        // 首次接入、epoch 改变或本地版本超前时，旧游标不可复用，必须全量重建。
        if (state.epoch() == null || !state.epoch().equals(head.epoch()) ||
                state.revision() > head.revision()) {
            Snapshot snapshot = remote.get().uri("/api/v1/sync/snapshot")
                    .retrieve().body(Snapshot.class);
            if (snapshot == null) throw new IllegalStateException("Missing snapshot");
            jdbc.update("DELETE FROM config_node");
            for (Node node : snapshot.nodes()) upsert(node);
            moveCursor(snapshot.epoch(), snapshot.revision());
            state = new State(snapshot.epoch(), snapshot.revision());
        }

        // 正常情况按 revision 顺序分页读取事件，每次最多处理 200 条。
        long cursor = state.revision();
        while (true) {
            long after = cursor;
            Event[] events = remote.get().uri(builder -> builder.path("/api/v1/sync/changes")
                    .queryParam("afterRevision", after).queryParam("limit", 200).build())
                    .retrieve().body(Event[].class);
            if (events == null || events.length == 0) break;
            Set<Long> ids = new LinkedHashSet<>();
            for (Event event : events) {
                // 发现版本不连续，说明增量事件无法安全衔接，直接退回快照恢复。
                if (event.revision() != ++cursor) {
                    resetFromSnapshot();
                    return;
                }
                ids.add(event.nodeId());
            }
            // 事件只提供节点 ID；批量获取当前节点，缺失的 ID 按删除处理。
            Node[] nodes = remote.post().uri("/api/v1/config/nodes/batch-get")
                    .body(new Ids(new ArrayList<>(ids))).retrieve().body(Node[].class);
            if (nodes == null) throw new IllegalStateException("Missing nodes");
            Map<Long, Node> current = java.util.Arrays.stream(nodes)
                    .collect(Collectors.toMap(Node::id, node -> node));
            for (Long id : ids) {
                Node node = current.get(id);
                if (node == null) jdbc.update("DELETE FROM config_node WHERE id=?", id);
                else upsert(node);
            }
            // 节点更新和游标推进在同一事务中提交，防止只更新一半状态。
            moveCursor(state.epoch(), cursor);
            if (events.length < 200) break;
        }
        // 若追赶结束仍未达到开始时看到的远端版本，重新读取快照兜底。
        if (cursor < head.revision()) {
            resetFromSnapshot();
        }
    }

    // 全量恢复时先清空本地副本，再写入服务端快照及对应游标。
    private void resetFromSnapshot() {
        Snapshot snapshot = remote.get().uri("/api/v1/sync/snapshot").retrieve().body(Snapshot.class);
        if (snapshot == null) throw new IllegalStateException("Missing snapshot");
        jdbc.update("DELETE FROM config_node");
        for (Node node : snapshot.nodes()) upsert(node);
        moveCursor(snapshot.epoch(), snapshot.revision());
    }

    private void moveCursor(String epoch, long revision) {
        jdbc.update("UPDATE config_sync_state SET epoch=?,revision=?,updated_at=UTC_TIMESTAMP(6) WHERE id=1",
                epoch, revision);
    }

    private void upsert(Node node) {
        jdbc.update("""
                INSERT INTO config_node(id,parent_id,node_type,code,name,status,config,remark,created_at,updated_at)
                VALUES(?,?,?,?,?,?,?,?,?,?)
                ON DUPLICATE KEY UPDATE parent_id=VALUES(parent_id),node_type=VALUES(node_type),
                    code=VALUES(code),name=VALUES(name),status=VALUES(status),config=VALUES(config),
                    remark=VALUES(remark),created_at=VALUES(created_at),updated_at=VALUES(updated_at)
                """, node.id(), node.parentId(), node.nodeType(), node.code(), node.name(),
                node.status(), node.config(), node.remark(), node.createdAt(), node.updatedAt());
    }

    public record State(String epoch, long revision) {}
    public record Revision(String epoch, long revision) {}
    public record Snapshot(String epoch, long revision, List<Node> nodes) {}
    public record Event(long revision, long nodeId, String operation, LocalDateTime createdAt) {}
    public record Ids(List<Long> ids) {}
    public record Node(long id, long parentId, String nodeType, String code, String name,
                       int status, String config, String remark,
                       LocalDateTime createdAt, LocalDateTime updatedAt) {}
}
