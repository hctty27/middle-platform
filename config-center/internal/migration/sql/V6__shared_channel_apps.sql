-- V6：渠道应用归属游戏，区服组只负责绑定。上线前备份并同时升级配置中心与接入服务。
-- 为避免环境中的相同渠道配置被错误合并，历史 JSON 全量保存在绑定覆盖层，公共 JSON 初始为 {}。
CREATE TABLE config_channel_app (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    game_id BIGINT NOT NULL,
    code VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
    name VARCHAR(128) NOT NULL,
    status TINYINT NOT NULL DEFAULT 1,
    config JSON NOT NULL,
    remark VARCHAR(500) NULL,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    UNIQUE KEY uq_channel_app_game_code (game_id,code),
    KEY idx_channel_app_game (game_id),
    CONSTRAINT chk_channel_app_status CHECK (status IN (0,1))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE config_channel_app_binding (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    channel_app_id BIGINT NOT NULL,
    server_group_id BIGINT NOT NULL,
    status TINYINT NOT NULL DEFAULT 1,
    config JSON NOT NULL,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    UNIQUE KEY uq_channel_app_group (channel_app_id,server_group_id),
    KEY idx_channel_binding_group (server_group_id),
    CONSTRAINT fk_channel_binding_app FOREIGN KEY (channel_app_id) REFERENCES config_channel_app(id) ON DELETE RESTRICT,
    CONSTRAINT chk_channel_binding_status CHECK (status IN (0,1))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 历史数据保留原始 id 和配置，方便迁移后人工核对。
CREATE TABLE config_node_v6_channel_app_archive AS
    SELECT * FROM config_node WHERE node_type='channel_app';
ALTER TABLE config_node_v6_channel_app_archive ADD PRIMARY KEY (id);

-- 每个游戏下相同 code 收敛为一个应用，不跨游戏合并。
INSERT INTO config_channel_app (id,game_id,code,name,status,config,remark,created_at,updated_at)
SELECT first_node.id,first_node.game_id,first_node.code,first_node.name,1,JSON_OBJECT(),
       first_node.remark,first_node.created_at,first_node.updated_at
FROM config_node first_node
JOIN (
    SELECT game_id,code,MIN(id) AS first_id
    FROM config_node WHERE node_type='channel_app'
    GROUP BY game_id,code
) grouped ON grouped.first_id=first_node.id;

-- 把旧渠道配置原样放到区服组覆盖层，保证迁移后的生效结果不变。
INSERT INTO config_channel_app_binding (id,channel_app_id,server_group_id,status,config,created_at,updated_at)
SELECT old.id,app.id,old.server_group_id,old.status,old.config,old.created_at,old.updated_at
FROM config_node old
JOIN config_channel_app app ON app.game_id=old.game_id AND app.code=old.code
WHERE old.node_type='channel_app';

DELETE FROM config_node WHERE node_type='channel_app';
ALTER TABLE config_node
    DROP CHECK chk_config_node_type,
    DROP CHECK chk_config_node_ancestry,
    ADD CONSTRAINT chk_config_node_type CHECK (node_type IN ('game','region','server_group')),
    ADD CONSTRAINT chk_config_node_ancestry CHECK (
        (node_type='game' AND game_id IS NULL AND region_id IS NULL AND server_group_id IS NULL)
        OR (node_type='region' AND COALESCE(game_id,0)>0 AND region_id IS NULL AND server_group_id IS NULL)
        OR (node_type='server_group' AND COALESCE(game_id,0)>0 AND COALESCE(region_id,0)>0 AND server_group_id IS NULL)
    );

-- 强制所有接入端以 V6 协议重新获取全量快照。
UPDATE config_revision SET epoch=UUID() WHERE id=1;
