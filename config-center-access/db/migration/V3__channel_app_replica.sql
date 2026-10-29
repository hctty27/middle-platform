-- 接入侧本地副本可由权威端重建；丢弃旧五级数据并重置游标。
-- 必须与已升级为四级结构的 config-center 配套上线。
DELETE FROM config_node;
UPDATE config_sync_state SET epoch=NULL,revision=0,updated_at=UTC_TIMESTAMP(6) WHERE id=1;

ALTER TABLE config_node
    DROP CHECK chk_replica_node_ancestry,
    DROP INDEX idx_replica_channel_type,
    DROP COLUMN channel_id,
    ADD CONSTRAINT chk_replica_node_ancestry CHECK (
        (node_type='game' AND game_id IS NULL AND region_id IS NULL AND server_group_id IS NULL)
        OR (node_type='region' AND COALESCE(game_id,0)>0 AND region_id IS NULL AND server_group_id IS NULL)
        OR (node_type='server_group' AND COALESCE(game_id,0)>0 AND COALESCE(region_id,0)>0 AND server_group_id IS NULL)
        OR (node_type='channel_app' AND COALESCE(game_id,0)>0 AND COALESCE(region_id,0)>0 AND COALESCE(server_group_id,0)>0)
    );
