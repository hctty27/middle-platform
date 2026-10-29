package com.hitall.platform.client.sync;

import com.hitall.platform.client.config.ConfigClientProperties;
import com.hitall.platform.client.remote.ConfigCenterRemoteClient;
import com.hitall.platform.client.repository.LocalConfigRepository;

import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.Set;
import java.util.stream.Collectors;

import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.transaction.PlatformTransactionManager;
import org.springframework.transaction.support.TransactionTemplate;

/**
 * 拉取配置中心数据并更新本地副本。
 * 网络请求不持有数据库事务；每批节点和同步游标在短事务中原子写入。
 */
public class ClientSynchronizer {
    private static final int PAGE_SIZE = 200;
    private static final int MAX_RETRIES = 8;

    private final LocalConfigRepository local;
    private final ConfigCenterRemoteClient remote;
    private final TransactionTemplate transactions;

    public ClientSynchronizer(JdbcTemplate jdbc, ConfigClientProperties properties,
                              PlatformTransactionManager transactionManager) {
        this(new LocalConfigRepository(jdbc), new ConfigCenterRemoteClient(properties), transactionManager);
    }

    public ClientSynchronizer(LocalConfigRepository local, ConfigCenterRemoteClient remote,
                              PlatformTransactionManager transactionManager) {
        this.local = local;
        this.remote = remote;
        this.transactions = new TransactionTemplate(transactionManager);
    }

    /**
     * 同一 JVM 内序列化同步任务；多个 JVM 通过同步游标行锁和版本比较协调。
     * 远程读取发生在事务外，防止网络延迟拉长 MySQL 行锁持有时间。
     */
    public synchronized void synchronize() {
        int retries = 0;
        while (true) {
            State state = local.readState();
            Revision head = remote.revision();

            // 首次接入、epoch 改变或游标超前时，只能重新加载一致性快照。
            if (requiresSnapshot(state, head)) {
                if (restoreSnapshot(state)) {
                    retries = 0;
                } else {
                    retries = retry(retries);
                }
                continue;
            }

            if (state.revision() == head.revision()) return;

            Event[] events = remote.changes(state.revision(), PAGE_SIZE);
            if (events == null || events.length == 0 || !contiguous(state.revision(), events)) {
                if (restoreSnapshot(state)) {
                    retries = 0;
                } else {
                    retries = retry(retries);
                }
                continue;
            }

            Set<Long> ids = new LinkedHashSet<>();
            for (Event event : events) ids.add(event.nodeId());

            Node[] nodes = remote.batchGet(new ArrayList<>(ids));
            Map<Long, Node> latest = Arrays.stream(nodes)
                    .collect(Collectors.toMap(Node::id, node -> node));

            // 避免数据集世代在批量拉取期间切换，导致两个 epoch 的数据混用。
            if (!Objects.equals(head.epoch(), remote.revision().epoch())) {
                retries = retry(retries);
                continue;
            }

            long targetRevision = events[events.length - 1].revision();
            boolean applied = Boolean.TRUE.equals(transactions.execute(status -> {
                State current = local.lockedState();
                if (!current.equals(state)) return false;
                for (Long id : ids) {
                    Node node = latest.get(id);
                    if (node == null) local.delete(id);
                    else local.upsert(node);
                }
                local.moveCursor(state.epoch(), targetRevision);
                return true;
            }));

            if (applied) {
                retries = 0;
            } else {
                retries = retry(retries);
            }
        }
    }

    private boolean restoreSnapshot(State expected) {
        // 快照通过配置中心的一致性读取接口取得，下载时不持有本地事务。
        Snapshot snapshot = remote.snapshot();
        if (snapshot == null || snapshot.epoch() == null || snapshot.nodes() == null) {
            throw new IllegalStateException("Invalid remote snapshot");
        }
        return Boolean.TRUE.equals(transactions.execute(status -> {
            if (!local.lockedState().equals(expected)) return false;
            local.replaceSnapshot(snapshot);
            return true;
        }));
    }

    private static boolean requiresSnapshot(State state, Revision head) {
        return state.epoch() == null || !state.epoch().equals(head.epoch())
                || state.revision() > head.revision();
    }

    private static boolean contiguous(long after, Event[] events) {
        long expected = after;
        for (Event event : events) {
            if (event == null || event.revision() != ++expected) return false;
        }
        return true;
    }

    private static int retry(int previous) {
        if (previous >= MAX_RETRIES) {
            throw new IllegalStateException("Config sync exceeded concurrent update retry limit");
        }
        return previous + 1;
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
