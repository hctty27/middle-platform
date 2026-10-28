CREATE TABLE config_node (
    id BIGINT NOT NULL PRIMARY KEY,
    parent_id BIGINT NOT NULL,
    node_type VARCHAR(20) NOT NULL,
    code VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
    name VARCHAR(128) NOT NULL,
    status TINYINT NOT NULL,
    config JSON NOT NULL,
    remark VARCHAR(500),
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    KEY idx_config_node_parent (parent_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE config_sync_state (
    id TINYINT NOT NULL PRIMARY KEY,
    epoch CHAR(36),
    revision BIGINT NOT NULL,
    updated_at DATETIME(6) NOT NULL
) ENGINE=InnoDB;
INSERT INTO config_sync_state(id,epoch,revision,updated_at) VALUES(1,NULL,0,UTC_TIMESTAMP(6));
