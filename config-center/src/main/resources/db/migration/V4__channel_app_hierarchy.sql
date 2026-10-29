-- 五级树收敛为四级：game -> region -> server_group -> channel_app。
-- 上线前需停止所有配置中心写入、备份权威数据库；此迁移为非向后兼容变更。
-- Flyway V1/V2/V3 必须保持原样，迁移失败后不要直接重跑部分成功的 DDL。
--
-- 预检：SDK 应用从不同渠道提升到同一区服组时，不允许相同 code。
-- 临时表主键利用与原业务一致的 utf8mb4_bin 区分大小写。
CREATE TEMPORARY TABLE config_v4_sibling_check (
    server_group_id BIGINT NOT NULL,
    code VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
    PRIMARY KEY (server_group_id, code)
) ENGINE=InnoDB;
INSERT INTO config_v4_sibling_check (server_group_id, code)
SELECT server_group_id, code FROM config_node WHERE node_type = 'sdk_app';
DROP TEMPORARY TABLE config_v4_sibling_check;

-- 保留原渠道配置及 SDK 应用所属渠道的完整快照，后续可以人工核对与回填。
-- 此表仅供历史审计，不参与新配置查询、订阅和同步。
CREATE TABLE config_node_v4_legacy_archive (
    id BIGINT NOT NULL PRIMARY KEY,
    game_id BIGINT NULL,
    region_id BIGINT NULL,
    server_group_id BIGINT NULL,
    channel_id BIGINT NULL,
    node_type VARCHAR(20) NOT NULL,
    code VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
    name VARCHAR(128) NOT NULL,
    status TINYINT NOT NULL,
    config JSON NOT NULL,
    remark VARCHAR(500) NULL,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    archived_at DATETIME(6) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO config_node_v4_legacy_archive
    (id,game_id,region_id,server_group_id,channel_id,node_type,code,name,status,config,remark,
     created_at,updated_at,archived_at)
SELECT id,game_id,region_id,server_group_id,channel_id,node_type,code,name,status,config,remark,
       created_at,updated_at,UTC_TIMESTAMP(6)
FROM config_node WHERE node_type IN ('channel','sdk_app');

ALTER TABLE config_node
    DROP CHECK chk_config_node_type,
    DROP CHECK chk_config_node_ancestry,
    DROP INDEX uq_config_node_sibling,
    DROP INDEX idx_config_node_channel_type;

-- 保持 SDK 应用原有主键/业务编码/JSON 配置，只改变类型和直属父节点层级。
UPDATE config_node SET node_type='channel_app' WHERE node_type='sdk_app';
DELETE FROM config_node WHERE node_type='channel';

ALTER TABLE config_node
    DROP COLUMN channel_id,
    ADD CONSTRAINT chk_config_node_type CHECK
        (node_type IN ('game','region','server_group','channel_app')),
    ADD CONSTRAINT chk_config_node_ancestry CHECK (
        (node_type='game' AND game_id IS NULL AND region_id IS NULL AND server_group_id IS NULL)
        OR (node_type='region' AND COALESCE(game_id,0)>0 AND region_id IS NULL AND server_group_id IS NULL)
        OR (node_type='server_group' AND COALESCE(game_id,0)>0 AND COALESCE(region_id,0)>0 AND server_group_id IS NULL)
        OR (node_type='channel_app' AND COALESCE(game_id,0)>0 AND COALESCE(region_id,0)>0 AND COALESCE(server_group_id,0)>0)
    );

CREATE UNIQUE INDEX uq_config_node_sibling ON config_node
    (node_type, code, ((COALESCE(server_group_id, region_id, game_id, 0))));

-- 结构变化必须切换 epoch；revision 不重置，接入实例据此恢复全量快照。
UPDATE config_revision SET epoch=UUID() WHERE id=1;
