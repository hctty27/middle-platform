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

/**
 * 将配置中心数据同步到接入服务的本地 MySQL 副本。
 * 优先增量同步；数据世代变化、事件断档时使用全量快照恢复。
 */
public class ClientSynchronizer {
    private final LocalConfigRepository local;
    private final ConfigCenterRemoteClient remote;

    public ClientSynchronizer(JdbcTemplate jdbc, ConfigClientProperties properties) {
        this(new LocalConfigRepository(jdbc), new ConfigCenterRemoteClient(properties));
    }

    public ClientSynchronizer(LocalConfigRepository local, ConfigCenterRemoteClient remote) {
        this.local = local;
        this.remote = remote;
    }

    /**
     * 同步远端最新配置及本地版本游标。
     * 本地更新置于事务中；同一实例内同步调用串行执行。
     */
    @Transactional
    public synchronized void synchronize() {
        State state = local.lockedState();
        Revision head = remote.revision();

        // 首次接入、epoch 改变或本地版本超前时，旧游标不可复用，必须全量重建。
        if (state.epoch() == null || !state.epoch().equals(head.epoch()) ||
                state.revision() > head.revision()) {
            state = resetFromSnapshot();
        }

        // 正常情况按 revision 顺序分页读取事件，每次最多处理 200 条。
        long cursor = state.revision();
        while (true) {
            Event[] events = remote.changes(cursor, 200);
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
            Node[] nodes = remote.batchGet(new ArrayList<>(ids));
            Map<Long, Node> current = java.util.Arrays.stream(nodes)
                    .collect(Collectors.toMap(Node::id, node -> node));
            for (Long id : ids) {
                Node node = current.get(id);
                if (node == null) local.delete(id);
                else local.upsert(node);
            }
            // 节点更新和游标推进在同一事务中提交，防止只更新一半状态。
            local.moveCursor(state.epoch(), cursor);
            if (events.length < 200) break;
        }
        // 若追赶结束仍未达到开始时看到的远端版本，重新读取快照兜底。
        if (cursor < head.revision()) {
            resetFromSnapshot();
        }
    }

    // 快照拉取与本地副本替换分离，事务语义保持不变。
    private State resetFromSnapshot() {
        return local.replaceSnapshot(remote.snapshot());
    }

    public record State(String epoch, long revision) {}
    public record Revision(String epoch, long revision) {}
    public record Snapshot(String epoch, long revision, List<Node> nodes) {}
    public record Event(long revision, long nodeId, String operation, LocalDateTime createdAt) {}
    public record Ids(List<Long> ids) {}
    @com.fasterxml.jackson.annotation.JsonIgnoreProperties(ignoreUnknown = true)
    public record Node(long id, String nodeType, Long gameId, Long regionId,
                       Long serverGroupId, Long channelId, String code, String name,
                       int status, String config, String remark,
                       LocalDateTime createdAt, LocalDateTime updatedAt) {}
}
