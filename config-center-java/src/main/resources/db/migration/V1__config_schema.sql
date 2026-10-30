-- 权威配置节点表：game → region → server_group → channel → sdk_app。
-- 同级节点 code 唯一；父节点类型合法性由写服务进一步校验。
CREATE TABLE config_node (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    parent_id BIGINT NOT NULL,
    node_type VARCHAR(20) NOT NULL,
    code VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
    name VARCHAR(128) NOT NULL,
    status TINYINT NOT NULL DEFAULT 1,
    config JSON NOT NULL,
    remark VARCHAR(500),
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    UNIQUE KEY uq_config_node_sibling (parent_id, node_type, code),
    KEY idx_config_node_parent (parent_id),
    CONSTRAINT chk_config_node_type CHECK (node_type IN ('game', 'region', 'server_group', 'channel', 'sdk_app')),
    CONSTRAINT chk_config_node_parent CHECK ((node_type = 'game' AND parent_id = 0) OR (node_type <> 'game' AND parent_id > 0)),
    CONSTRAINT chk_config_node_status CHECK (status IN (0, 1))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 全局版本记录：固定只有 id=1 的一行，revision 每次变更递增。
-- epoch 标识数据世代，用于识别数据库重建或版本体系变化。
CREATE TABLE config_revision (
    id TINYINT NOT NULL PRIMARY KEY,
    revision BIGINT NOT NULL,
    epoch CHAR(36) NOT NULL,
    CONSTRAINT chk_config_revision_singleton CHECK (id = 1)
) ENGINE=InnoDB;

INSERT INTO config_revision (id, revision, epoch) VALUES (1, 0, UUID());

-- 增量事件日志：只记录 revision、node_id 和操作类型，不重复保存完整节点数据。
CREATE TABLE config_change_log (
    revision BIGINT NOT NULL PRIMARY KEY,
    node_id BIGINT NOT NULL,
    operation VARCHAR(10) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    KEY idx_change_node (node_id),
    CONSTRAINT chk_change_operation CHECK (operation IN ('UPSERT', 'DELETE'))
) ENGINE=InnoDB;

-- 接入方订阅信息：保存回调地址、共享密钥及启用状态。
CREATE TABLE config_subscriber (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    code VARCHAR(128) COLLATE utf8mb4_bin NOT NULL UNIQUE,
    callback_url VARCHAR(1024) NOT NULL,
    webhook_secret VARCHAR(255) NOT NULL,
    enabled TINYINT NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 每个接入方独立的投递游标、重试时间和租约状态，用于多实例协调。
CREATE TABLE config_delivery_state (
    subscriber_id BIGINT NOT NULL PRIMARY KEY,
    acknowledged_revision BIGINT NOT NULL DEFAULT 0,
    lease_owner VARCHAR(64) NULL,
    lease_until DATETIME(6) NULL,
    next_attempt_at DATETIME(6) NOT NULL,
    failure_count INT NOT NULL DEFAULT 0,
    last_error VARCHAR(500) NULL,
    updated_at DATETIME(6) NOT NULL,
    FOREIGN KEY (subscriber_id) REFERENCES config_subscriber(id) ON DELETE CASCADE
) ENGINE=InnoDB;
