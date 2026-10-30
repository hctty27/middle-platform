-- V10：正式服协议值从 production 统一为 prod；历史迁移 SQL 不修改。
-- 升级前停止旧版写入服务、备份权威库；MySQL DDL 不是整体事务。
-- 临时允许新旧值，以便在 CHECK 约束下完成已有数据转换。
ALTER TABLE config_node
    DROP CHECK chk_config_node_env_type,
    ADD CONSTRAINT chk_config_node_env_type CHECK (
        (node_type = 'region' AND (env_type IS NULL OR env_type IN ('test','review','production','prod')))
        OR (node_type <> 'region' AND env_type IS NULL)
    );

UPDATE config_node SET env_type='prod' WHERE node_type='region' AND env_type='production';

ALTER TABLE config_node
    DROP CHECK chk_config_node_env_type,
    ADD CONSTRAINT chk_config_node_env_type CHECK (
        (node_type = 'region' AND (env_type IS NULL OR env_type IN ('test','review','prod')))
        OR (node_type <> 'region' AND env_type IS NULL)
    ),
    MODIFY COLUMN env_type VARCHAR(16) COLLATE utf8mb4_bin NULL
        COMMENT 'test/review/prod; NULL=legacy unclassified';

-- 迫使接入端按新协议重建快照，不改变 revision。
UPDATE config_revision SET epoch=UUID() WHERE id=1;
