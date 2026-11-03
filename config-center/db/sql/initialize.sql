-- 仅用于新建空的 config_center 库；包含当前全部业务表，禁止在已有库执行。
-- 手工运行本文件前请先创建数据库并备份。版本记录不再由服务维护。

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

-- 已有 config_center 库缺少 game_order 时，在备份并确认表不存在后人工执行。
-- 不会修改旧业务表，也不更新已废弃的迁移版本记录。
CREATE TABLE game_order (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '内部主键',
    appid VARCHAR(128) COLLATE utf8mb4_bin NOT NULL COMMENT '应用 Code',
    game_id BIGINT NOT NULL COMMENT '登记时所属游戏 ID，通知补偿固定读取该游戏的主密钥',
    env VARCHAR(16) COLLATE utf8mb4_bin NOT NULL COMMENT 'prod/test/review',
    game_order_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL COMMENT '游戏订单号',
    amount BIGINT UNSIGNED NOT NULL COMMENT '预期实付，人民币分',
    attach TEXT NOT NULL COMMENT '原始透传字符串',
    game_notify_url VARCHAR(2048) NOT NULL COMMENT '大区 payNotifyUrl 快照',
    platform_order_no VARCHAR(128) COLLATE utf8mb4_bin DEFAULT NULL COMMENT '平台订单号',
    paid_at DATETIME(3) DEFAULT NULL COMMENT '支付完成 UTC 时间',
    payment_status TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0未支付 1已支付',
    notify_status TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0待通知 1成功 2失败',
    notify_attempts INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '通知次数',
    last_notify_at DATETIME(3) DEFAULT NULL COMMENT '最近通知开始 UTC 时间',
    last_notify_error VARCHAR(512) NOT NULL DEFAULT '' COMMENT '脱敏错误',
    notify_success_at DATETIME(3) DEFAULT NULL COMMENT '首次成功 UTC 时间',
    next_notify_at DATETIME(3) DEFAULT NULL COMMENT '下次自动补偿 UTC 时间',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_app_game_order (appid,game_order_id),
    UNIQUE KEY uk_platform_order (platform_order_no),
    KEY idx_due_notify (payment_status,notify_status,next_notify_at),
    KEY idx_env_created (env,created_at),
    CONSTRAINT chk_order_env CHECK (env IN ('prod','test','review')),
    CONSTRAINT chk_order_amount CHECK (amount>0),
    CONSTRAINT chk_order_payment CHECK (payment_status IN (0,1)),
    CONSTRAINT chk_order_notify CHECK (notify_status IN (0,1,2))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='游戏充值订单登记';

-- 统一安装脚本：SDK 功能目录和渠道按环境开关。
CREATE TABLE sdk_service (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    code VARCHAR(64) COLLATE utf8mb4_bin NOT NULL COMMENT '全局唯一服务编码',
    name VARCHAR(100) NOT NULL COMMENT '服务名称',
    description VARCHAR(500) NOT NULL DEFAULT '' COMMENT '服务说明',
    enabled TINYINT NOT NULL DEFAULT 1 COMMENT '全局开关 0关闭 1开启',
    sort_order INT NOT NULL DEFAULT 0 COMMENT '显示顺序',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE KEY uk_sdk_service_code (code),
    CONSTRAINT chk_sdk_service_enabled CHECK (enabled IN (0, 1))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
  COMMENT='SDK 全局功能定义';

CREATE TABLE sdk_service_channel (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    channel_app_id BIGINT NOT NULL COMMENT '关联 config_channel_app.id',
    service_id BIGINT NOT NULL COMMENT '关联 sdk_service.id',
    env VARCHAR(16) COLLATE utf8mb4_bin NOT NULL COMMENT 'prod/test/review',
    enabled TINYINT NOT NULL DEFAULT 0 COMMENT '渠道包服务开关 0关闭 1开启',
    config JSON NOT NULL COMMENT '服务独立扩展参数',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE KEY uk_sdk_service_channel (channel_app_id, service_id, env),
    KEY idx_sdk_service_channel_service (service_id),
    CONSTRAINT fk_sdk_service_channel_app FOREIGN KEY (channel_app_id)
        REFERENCES config_channel_app(id) ON DELETE CASCADE,
    CONSTRAINT fk_sdk_service_channel_service FOREIGN KEY (service_id)
        REFERENCES sdk_service(id) ON DELETE RESTRICT,
    CONSTRAINT chk_sdk_service_channel_enabled CHECK (enabled IN (0, 1)),
    CONSTRAINT chk_sdk_service_channel_env CHECK (env IN ('prod', 'test', 'review')),
    CONSTRAINT chk_sdk_service_channel_config CHECK (JSON_TYPE(config) = 'OBJECT')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
  COMMENT='渠道包按环境配置 SDK 功能';

-- 服务目录预置项。是否开放接口仍由对应 Go 业务模块决定。
-- 不包含充值：关闭服务不能中断已确认支付订单的发货补偿。
INSERT INTO sdk_service (code, name, description, enabled, sort_order) VALUES
('event_report', '事件上报', '游戏事件向发行平台上报', 1, 10),
('role_query', '角色查询', '平台查询游戏角色信息', 1, 20),
('server_query', '区服查询', '平台查询区服信息', 1, 30),
('user_check', '用户信息核验', '核验平台用户信息', 1, 40),
('role_ban', '封解禁', '角色封号、禁言及解除', 1, 50),
('subscribe_message', '订阅消息', '微信等渠道的订阅消息', 1, 60);
