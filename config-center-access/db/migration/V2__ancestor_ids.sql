-- 旧版 Demo 本地副本升级：先按历史父节点回填完整路径，再删除 parent_id。
ALTER TABLE config_node
    ADD COLUMN game_id BIGINT NULL,
    ADD COLUMN region_id BIGINT NULL,
    ADD COLUMN server_group_id BIGINT NULL,
    ADD COLUMN channel_id BIGINT NULL;

UPDATE config_node child JOIN config_node parent
    ON parent.id = child.parent_id AND parent.node_type = 'game'
SET child.game_id = parent.id
WHERE child.node_type = 'region';

UPDATE config_node child JOIN config_node parent
    ON parent.id = child.parent_id AND parent.node_type = 'region'
SET child.game_id = parent.game_id, child.region_id = parent.id
WHERE child.node_type = 'server_group';

UPDATE config_node child JOIN config_node parent
    ON parent.id = child.parent_id AND parent.node_type = 'server_group'
SET child.game_id = parent.game_id, child.region_id = parent.region_id,
    child.server_group_id = parent.id
WHERE child.node_type = 'channel';

UPDATE config_node child JOIN config_node parent
    ON parent.id = child.parent_id AND parent.node_type = 'channel'
SET child.game_id = parent.game_id, child.region_id = parent.region_id,
    child.server_group_id = parent.server_group_id, child.channel_id = parent.id
WHERE child.node_type = 'sdk_app';

ALTER TABLE config_node
    ADD CONSTRAINT chk_replica_node_ancestry CHECK (
        (node_type = 'game' AND game_id IS NULL AND region_id IS NULL AND server_group_id IS NULL AND channel_id IS NULL)
        OR (node_type = 'region' AND COALESCE(game_id, 0) > 0 AND region_id IS NULL AND server_group_id IS NULL AND channel_id IS NULL)
        OR (node_type = 'server_group' AND COALESCE(game_id, 0) > 0 AND COALESCE(region_id, 0) > 0 AND server_group_id IS NULL AND channel_id IS NULL)
        OR (node_type = 'channel' AND COALESCE(game_id, 0) > 0 AND COALESCE(region_id, 0) > 0 AND COALESCE(server_group_id, 0) > 0 AND channel_id IS NULL)
        OR (node_type = 'sdk_app' AND COALESCE(game_id, 0) > 0 AND COALESCE(region_id, 0) > 0 AND COALESCE(server_group_id, 0) > 0 AND COALESCE(channel_id, 0) > 0)
    );

ALTER TABLE config_node
    DROP INDEX idx_config_node_parent,
    DROP COLUMN parent_id;

CREATE INDEX idx_replica_game_type ON config_node (game_id, node_type);
CREATE INDEX idx_replica_region_type ON config_node (region_id, node_type);
CREATE INDEX idx_replica_group_type ON config_node (server_group_id, node_type);
CREATE INDEX idx_replica_channel_type ON config_node (channel_id, node_type);
