-- 将固定五级配置树改为四个祖先 ID；保留既有 V1 校验和历史记录。
-- 部署前应备份数据库，并协调暂停旧版配置中心和客户端写入。
ALTER TABLE config_node
    ADD COLUMN game_id BIGINT NULL,
    ADD COLUMN region_id BIGINT NULL,
    ADD COLUMN server_group_id BIGINT NULL,
    ADD COLUMN channel_id BIGINT NULL;

-- 由近及远逐级回填；通过 CHECK 强制每种类型的路径完整。
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
    ADD CONSTRAINT chk_config_node_ancestry CHECK (
        (node_type = 'game' AND game_id IS NULL AND region_id IS NULL AND server_group_id IS NULL AND channel_id IS NULL)
        OR (node_type = 'region' AND game_id > 0 AND region_id IS NULL AND server_group_id IS NULL AND channel_id IS NULL)
        OR (node_type = 'server_group' AND game_id > 0 AND region_id > 0 AND server_group_id IS NULL AND channel_id IS NULL)
        OR (node_type = 'channel' AND game_id > 0 AND region_id > 0 AND server_group_id > 0 AND channel_id IS NULL)
        OR (node_type = 'sdk_app' AND game_id > 0 AND region_id > 0 AND server_group_id > 0 AND channel_id > 0)
    );

ALTER TABLE config_node
    DROP INDEX uq_config_node_sibling,
    DROP INDEX idx_config_node_parent,
    DROP CHECK chk_config_node_parent,
    DROP COLUMN parent_id;

-- COALESCE 总是得到直属父节点 ID（游戏为 0）。普通 UNIQUE 包含 NULL 时无法保证同级唯一。
CREATE UNIQUE INDEX uq_config_node_sibling
    ON config_node (node_type, code,
        ((COALESCE(channel_id, server_group_id, region_id, game_id, 0))));

CREATE INDEX idx_config_node_game_type ON config_node (game_id, node_type);
CREATE INDEX idx_config_node_region_type ON config_node (region_id, node_type);
CREATE INDEX idx_config_node_group_type ON config_node (server_group_id, node_type);
CREATE INDEX idx_config_node_channel_type ON config_node (channel_id, node_type);

-- 让已接入客户端识别结构变化并主动拉取新格式全量快照；revision 保持递增。
UPDATE config_revision SET epoch = UUID() WHERE id = 1;
