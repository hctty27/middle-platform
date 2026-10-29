package com.hitall.platform.client;

import java.util.List;
import org.springframework.web.client.RestClient;
import com.hitall.platform.client.ClientSynchronizer.Event;
import com.hitall.platform.client.ClientSynchronizer.Node;
import com.hitall.platform.client.ClientSynchronizer.Ids;
import com.hitall.platform.client.ClientSynchronizer.Revision;
import com.hitall.platform.client.ClientSynchronizer.Snapshot;

/** 封装配置中心的版本、增量、节点批量查询和快照接口。 */
public class ConfigCenterRemoteClient {
    private final RestClient client;

    public ConfigCenterRemoteClient(ConfigClientProperties properties) {
        client = RestClient.builder().baseUrl(properties.getBaseUrl()).build();
    }

    public Revision revision() {
        Revision value = client.get().uri("/api/v1/sync/revision").retrieve().body(Revision.class);
        if (value == null) throw new IllegalStateException("Missing remote revision");
        return value;
    }

    public Event[] changes(long after, int limit) {
        Event[] value = client.get().uri(builder -> builder.path("/api/v1/sync/changes")
                .queryParam("afterRevision", after).queryParam("limit", limit).build())
                .retrieve().body(Event[].class);
        return value == null ? new Event[0] : value;
    }

    public Node[] batchGet(List<Long> ids) {
        Node[] value = client.post().uri("/api/v1/config/nodes/batch-get")
                .body(new Ids(ids)).retrieve().body(Node[].class);
        if (value == null) throw new IllegalStateException("Missing nodes");
        return value;
    }

    public Snapshot snapshot() {
        Snapshot value = client.get().uri("/api/v1/sync/snapshot").retrieve().body(Snapshot.class);
        if (value == null) throw new IllegalStateException("Missing snapshot");
        return value;
    }
}
