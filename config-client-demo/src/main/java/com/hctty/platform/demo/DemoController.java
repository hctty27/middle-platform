package com.hctty.platform.demo;

import java.util.List;
import java.util.Map;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/demo")
public class DemoController {
    private final JdbcTemplate jdbc;

    public DemoController(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    @GetMapping("/nodes")
    public List<Map<String, Object>> nodes() {
        return jdbc.queryForList("SELECT id,parent_id,node_type,code,name,status FROM config_node ORDER BY id");
    }

    @GetMapping("/revision")
    public Map<String, Object> revision() {
        return jdbc.queryForMap("SELECT epoch,revision,updated_at FROM config_sync_state WHERE id=1");
    }
}
