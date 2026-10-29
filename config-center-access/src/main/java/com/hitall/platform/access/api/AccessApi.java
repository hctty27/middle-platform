package com.hitall.platform.access.api;

import java.util.List;
import java.util.Map;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.web.bind.annotation.*;

/**
 * 接入服务对外展示本地配置副本及同步进度。
 * 以下接口读取本地数据库，不会直接调用配置中心。
 */
@RestController
@RequestMapping("/api/access")
public class AccessApi {
    private final JdbcTemplate jdbc;

    public AccessApi(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    @GetMapping("/nodes")
    public List<Map<String, Object>> nodes() {
        return jdbc.queryForList("SELECT id,game_id,region_id,server_group_id,node_type,code,name,status FROM config_node ORDER BY id");
    }

    @GetMapping("/revision")
    public Map<String, Object> revision() {
        return jdbc.queryForMap("SELECT epoch,revision,updated_at FROM config_sync_state WHERE id=1");
    }
}
