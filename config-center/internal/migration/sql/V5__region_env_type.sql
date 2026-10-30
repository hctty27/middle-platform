-- 保留历史大区的 NULL 类型，等待人工明确归类；不自动推断为正式服。
ALTER TABLE config_node
    ADD COLUMN env_type VARCHAR(16) COLLATE utf8mb4_bin NULL COMMENT 'test/review/production; NULL=legacy unclassified',
    ADD CONSTRAINT chk_config_node_env_type CHECK (
        (node_type = 'region' AND (env_type IS NULL OR env_type IN ('test','review','production')))
        OR (node_type <> 'region' AND env_type IS NULL)
    );

-- 新增同步字段时更新数据世代，客户端自动拉取完整快照。
UPDATE config_revision SET epoch = UUID() WHERE id = 1;
