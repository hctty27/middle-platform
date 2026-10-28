package com.hctty.platform.client;

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

public class ClientSynchronizer {
    private final JdbcTemplate jdbc;
    private final RestClient remote;

    public ClientSynchronizer(JdbcTemplate jdbc, ConfigClientProperties properties) {
        this.jdbc = jdbc;
        this.remote = RestClient.builder().baseUrl(properties.getBaseUrl()).build();
    }

    @Transactional
    public synchronized void synchronize() {
        State state = jdbc.queryForObject(
                "SELECT epoch,revision FROM config_sync_state WHERE id=1 FOR UPDATE",
                (rs, row) -> new State(rs.getString(1), rs.getLong(2)));
        Revision head = remote.get().uri("/api/v1/sync/revision").retrieve().body(Revision.class);
        if (head == null) throw new IllegalStateException("Missing remote revision");

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

        long cursor = state.revision();
        while (true) {
            long after = cursor;
            Event[] events = remote.get().uri(builder -> builder.path("/api/v1/sync/changes")
                    .queryParam("afterRevision", after).queryParam("limit", 200).build())
                    .retrieve().body(Event[].class);
            if (events == null || events.length == 0) break;
            Set<Long> ids = new LinkedHashSet<>();
            for (Event event : events) {
                if (event.revision() != ++cursor) {
                    throw new IllegalStateException("Missing revision after " + (cursor - 1));
                }
                ids.add(event.nodeId());
            }
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
            moveCursor(state.epoch(), cursor);
            if (events.length < 200) break;
        }
        if (cursor < head.revision()) {
            throw new IllegalStateException("Change history missing, full rebuild required");
        }
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
