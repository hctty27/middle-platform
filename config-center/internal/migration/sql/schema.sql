-- V11 基线：仅用于全新、空的 config_center 数据库。
-- 不包含历史归档表、已废弃的渠道应用绑定表和过渡字段。
-- config_schema_migrations 由 Go 迁移器创建，禁止手工对已有数据库执行本文件。

CREATE TABLE config_revision (
    id TINYINT NOT NULL PRIMARY KEY,
    revision BIGINT NOT NULL,
    epoch CHAR(36) NOT NULL,
    CONSTRAINT chk_config_revision_singleton CHECK (id = 1)
) ENGINE=InnoDB COMMENT='全局配置同步世代与版本游标';

INSERT INTO config_revision (id, revision, epoch) VALUES (1, 0, UUID());

CREATE TABLE config_node (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    game_id BIGINT NULL,
    region_id BIGINT NULL,
    node_type VARCHAR(20) NOT NULL,
    env_type VARCHAR(16) COLLATE utf8mb4_bin NULL COMMENT 'test/review/prod; NULL=legacy unclassified',
    code VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
    name VARCHAR(128) NOT NULL,
    status TINYINT NOT NULL DEFAULT 1,
    config JSON NOT NULL,
    remark VARCHAR(500) NULL,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    UNIQUE KEY uq_config_node_sibling (node_type, code, ((COALESCE(region_id, game_id, 0)))),
    KEY idx_config_node_game_type (game_id, node_type),
    KEY idx_config_node_region_type (region_id, node_type),
    CONSTRAINT chk_config_node_type CHECK (node_type IN ('game', 'region', 'server_group')),
    CONSTRAINT chk_config_node_ancestry CHECK (
        (node_type = 'game' AND game_id IS NULL AND region_id IS NULL)
        OR (node_type = 'region' AND COALESCE(game_id, 0) > 0 AND region_id IS NULL)
        OR (node_type = 'server_group' AND COALESCE(game_id, 0) > 0 AND COALESCE(region_id, 0) > 0)
    ),
    CONSTRAINT chk_config_node_status CHECK (status IN (0, 1)),
    CONSTRAINT chk_config_node_env_type CHECK (
        (node_type = 'region' AND (env_type IS NULL OR env_type IN ('test', 'review', 'prod')))
        OR (node_type <> 'region' AND env_type IS NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
  COMMENT='游戏、大区、区服组三级配置节点';

CREATE TABLE config_node_change_log (
    revision BIGINT NOT NULL PRIMARY KEY,
    node_id BIGINT NOT NULL,
    operation VARCHAR(10) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    KEY idx_change_node (node_id),
    CONSTRAINT chk_change_operation CHECK (operation IN ('UPSERT', 'DELETE'))
) ENGINE=InnoDB COMMENT='三级配置节点变更日志';

CREATE TABLE config_subscriber (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    code VARCHAR(128) COLLATE utf8mb4_bin NOT NULL UNIQUE,
    callback_url VARCHAR(1024) NOT NULL,
    webhook_secret VARCHAR(255) NOT NULL,
    enabled TINYINT NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='配置订阅者与回调接入信息';

CREATE TABLE config_subscriber_delivery_state (
    subscriber_id BIGINT NOT NULL PRIMARY KEY,
    acknowledged_revision BIGINT NOT NULL DEFAULT 0,
    lease_owner VARCHAR(64) NULL,
    lease_until DATETIME(6) NULL,
    next_attempt_at DATETIME(6) NOT NULL,
    failure_count INT NOT NULL DEFAULT 0,
    last_error VARCHAR(500) NULL,
    updated_at DATETIME(6) NOT NULL,
    acknowledged_epoch CHAR(36) NULL,
    FOREIGN KEY (subscriber_id) REFERENCES config_subscriber(id) ON DELETE CASCADE
) ENGINE=InnoDB COMMENT='订阅者回调投递状态、租约和失败重试';

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
    prod_server_group_id BIGINT NULL COMMENT '正式服区服组',
    test_server_group_id BIGINT NULL COMMENT '测试服区服组',
    review_server_group_id BIGINT NULL COMMENT '提审服区服组',
    UNIQUE KEY uq_channel_app_code (code),
    KEY idx_channel_app_game (game_id),
    CONSTRAINT chk_channel_app_status CHECK (status IN (0, 1)),
    CONSTRAINT fk_app_prod_group FOREIGN KEY (prod_server_group_id) REFERENCES config_node(id) ON DELETE RESTRICT,
    CONSTRAINT fk_app_test_group FOREIGN KEY (test_server_group_id) REFERENCES config_node(id) ON DELETE RESTRICT,
    CONSTRAINT fk_app_review_group FOREIGN KEY (review_server_group_id) REFERENCES config_node(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
  COMMENT='游戏级渠道应用公共配置';