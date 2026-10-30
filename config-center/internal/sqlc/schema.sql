-- sqlc analysis snapshot of Go migration V5 (sqlc only; V6 channel tables are accessed via database/sql). Do not use this file for migrations.
-- Source migrations live in config-center/internal/migration/sql.
CREATE TABLE config_node (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    game_id BIGINT NULL,
    region_id BIGINT NULL,
    server_group_id BIGINT NULL,
    node_type VARCHAR(20) NOT NULL,
    env_type VARCHAR(16) NULL,
    code VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL,
    status TINYINT NOT NULL,
    config JSON NOT NULL,
    remark VARCHAR(500),
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL
);

CREATE TABLE config_revision (
    id TINYINT NOT NULL PRIMARY KEY,
    revision BIGINT NOT NULL,
    epoch CHAR(36) NOT NULL
);

CREATE TABLE config_change_log (
    revision BIGINT NOT NULL PRIMARY KEY,
    node_id BIGINT NOT NULL,
    operation VARCHAR(10) NOT NULL,
    created_at DATETIME(6) NOT NULL
);

CREATE TABLE config_subscriber (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    code VARCHAR(128) NOT NULL,
    callback_url VARCHAR(1024) NOT NULL,
    webhook_secret VARCHAR(255) NOT NULL,
    enabled TINYINT NOT NULL,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL
);

CREATE TABLE config_delivery_state (
    subscriber_id BIGINT NOT NULL PRIMARY KEY,
    acknowledged_revision BIGINT NOT NULL,
    acknowledged_epoch CHAR(36),
    lease_owner VARCHAR(64),
    lease_until DATETIME(6),
    next_attempt_at DATETIME(6) NOT NULL,
    failure_count INT NOT NULL,
    last_error VARCHAR(500),
    updated_at DATETIME(6) NOT NULL
);
