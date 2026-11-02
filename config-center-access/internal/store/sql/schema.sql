-- 接入端 V6 基线：仅用于全新、空的 config_access 数据库。
-- 接入端只存储 game / region / server_group 与同步游标。
CREATE TABLE config_node (
    id BIGINT NOT NULL PRIMARY KEY,
    game_id BIGINT NULL,
    region_id BIGINT NULL,
    node_type VARCHAR(20) NOT NULL,
    env_type VARCHAR(16) COLLATE utf8mb4_bin NULL,
    code VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
    name VARCHAR(128) NOT NULL,
    status TINYINT NOT NULL,
    config JSON NOT NULL,
    remark VARCHAR(500) NULL,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    KEY idx_replica_game_type (game_id, node_type),
    KEY idx_replica_region_type (region_id, node_type),
    CONSTRAINT chk_replica_node_type CHECK (node_type IN ('game', 'region', 'server_group')),
    CONSTRAINT chk_replica_node_env_type CHECK (
        (node_type = 'region' AND (env_type IS NULL OR env_type IN ('test', 'review', 'prod')))
        OR (node_type <> 'region' AND env_type IS NULL)
    ),
    CONSTRAINT chk_replica_node_ancestry CHECK (
        (node_type = 'game' AND game_id IS NULL AND region_id IS NULL)
        OR (node_type = 'region' AND COALESCE(game_id, 0) > 0 AND region_id IS NULL)
        OR (node_type = 'server_group' AND COALESCE(game_id, 0) > 0 AND COALESCE(region_id, 0) > 0)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE config_sync_state (
    id TINYINT NOT NULL PRIMARY KEY,
    epoch CHAR(36) NULL,
    revision BIGINT NOT NULL,
    updated_at DATETIME(6) NOT NULL
) ENGINE=InnoDB;

INSERT INTO config_sync_state(id, epoch, revision, updated_at)
VALUES (1, NULL, 0, UTC_TIMESTAMP(6));