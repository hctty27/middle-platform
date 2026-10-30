-- V11：将固定的 prod/test/review 一对一环境绑定内聚到渠道应用。
-- Go 迁移器执行 preflightV11；必须停写备份，历史覆盖配置必须可合并为同一最终 JSON。
-- code 全局唯一（utf8mb4_bin，区分大小写），不再按 game_id+code 唯一。
ALTER TABLE config_channel_app
    ADD COLUMN prod_server_group_id BIGINT NULL COMMENT '正式服区服组',
    ADD COLUMN test_server_group_id BIGINT NULL COMMENT '测试服区服组',
    ADD COLUMN review_server_group_id BIGINT NULL COMMENT '提审服区服组',
    DROP INDEX uq_channel_app_game_code,
    ADD UNIQUE INDEX uq_channel_app_code (code),
    ADD CONSTRAINT fk_app_prod_group FOREIGN KEY (prod_server_group_id) REFERENCES config_node(id) ON DELETE RESTRICT,
    ADD CONSTRAINT fk_app_test_group FOREIGN KEY (test_server_group_id) REFERENCES config_node(id) ON DELETE RESTRICT,
    ADD CONSTRAINT fk_app_review_group FOREIGN KEY (review_server_group_id) REFERENCES config_node(id) ON DELETE RESTRICT;

UPDATE config_channel_app a
LEFT JOIN (
    SELECT b.channel_app_id,
        MAX(CASE WHEN r.env_type='prod' THEN b.server_group_id END) AS prod_id,
        MAX(CASE WHEN r.env_type='test' THEN b.server_group_id END) AS test_id,
        MAX(CASE WHEN r.env_type='review' THEN b.server_group_id END) AS review_id
    FROM config_channel_app_binding b
    JOIN config_node sg ON sg.id=b.server_group_id
    JOIN config_node r ON r.id=sg.region_id
    GROUP BY b.channel_app_id
) mapped ON mapped.channel_app_id=a.id
SET a.prod_server_group_id=mapped.prod_id,
    a.test_server_group_id=mapped.test_id,
    a.review_server_group_id=mapped.review_id;

-- 旧覆盖配置仅在所有环境生效 JSON 相同时上升为唯一公共配置。
UPDATE config_channel_app a
JOIN (
    SELECT channel_app_id,MIN(id) AS first_binding_id
    FROM config_channel_app_binding GROUP BY channel_app_id
) first_binding ON first_binding.channel_app_id=a.id
JOIN config_channel_app_binding b ON b.id=first_binding.first_binding_id
SET a.config=JSON_MERGE_PATCH(a.config,b.config);

DROP TABLE config_channel_app_binding;
