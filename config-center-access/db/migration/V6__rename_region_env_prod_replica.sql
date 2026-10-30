-- 接入副本 V6：停接入服务、备份副本并完成 V5 后手动执行。
-- 扩展约束、转换历史值、再收紧约束；保留原节点和同步游标。
ALTER TABLE config_node
    DROP CHECK chk_replica_node_env_type,
    ADD CONSTRAINT chk_replica_node_env_type CHECK (
        (node_type='region' AND (env_type IS NULL OR env_type IN ('test','review','production','prod')))
        OR (node_type<>'region' AND env_type IS NULL)
    );

UPDATE config_node SET env_type='prod' WHERE node_type='region' AND env_type='production';

ALTER TABLE config_node
    DROP CHECK chk_replica_node_env_type,
    ADD CONSTRAINT chk_replica_node_env_type CHECK (
        (node_type='region' AND (env_type IS NULL OR env_type IN ('test','review','prod')))
        OR (node_type<>'region' AND env_type IS NULL)
    );
