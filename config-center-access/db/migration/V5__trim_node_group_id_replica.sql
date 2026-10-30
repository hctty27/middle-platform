-- 接入端副本 V5：停接入端并备份后手工执行；必须先完成 V3、V4。
-- 仅移除 config_node 冗余列，绑定表中的 server_group_id 必须保留。
ALTER TABLE config_node
    DROP CHECK chk_replica_node_ancestry,
    DROP INDEX idx_replica_group_type,
    DROP COLUMN server_group_id,
    ADD CONSTRAINT chk_replica_node_ancestry CHECK (
        (node_type='game' AND game_id IS NULL AND region_id IS NULL)
        OR (node_type='region' AND COALESCE(game_id,0)>0 AND region_id IS NULL)
        OR (node_type='server_group' AND COALESCE(game_id,0)>0 AND COALESCE(region_id,0)>0)
    );
