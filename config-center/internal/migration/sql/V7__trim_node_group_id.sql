-- V7：配置树只包含 game → region → server_group，节点不再需要 server_group_id。
-- 迁移前备份数据库、暂停旧版写入实例；绑定表中的 server_group_id 保留。
ALTER TABLE config_node
    DROP CHECK chk_config_node_ancestry,
    DROP INDEX uq_config_node_sibling,
    DROP INDEX idx_config_node_group_type,
    DROP COLUMN server_group_id,
    ADD CONSTRAINT chk_config_node_ancestry CHECK (
        (node_type='game' AND game_id IS NULL AND region_id IS NULL)
        OR (node_type='region' AND COALESCE(game_id,0)>0 AND region_id IS NULL)
        OR (node_type='server_group' AND COALESCE(game_id,0)>0 AND COALESCE(region_id,0)>0)
    ),
    ADD UNIQUE INDEX uq_config_node_sibling
        (node_type,code,((COALESCE(region_id,game_id,0))));

-- 结构变化切换数据世代，保留 revision。
UPDATE config_revision SET epoch=UUID() WHERE id=1;
