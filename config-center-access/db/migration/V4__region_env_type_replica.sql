-- 已有 V3 接入副本添加大区类型，保留节点数据与同步游标。
ALTER TABLE config_node
    ADD COLUMN env_type VARCHAR(16) COLLATE utf8mb4_bin NULL,
    ADD CONSTRAINT chk_replica_node_env_type CHECK (
        (node_type='region' AND (env_type IS NULL OR env_type IN ('test','review','production')))
        OR (node_type<>'region' AND env_type IS NULL)
    );
