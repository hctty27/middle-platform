-- 现有 config_center 数据库手动执行一次；请先备份并确认两张表不存在。
-- SDK Core 不会自动建表，现有订单和配置表不受影响。
CREATE TABLE sdk_service (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    code VARCHAR(64) COLLATE utf8mb4_bin NOT NULL COMMENT '全局唯一服务编码',
    name VARCHAR(100) NOT NULL COMMENT '服务名称',
    description VARCHAR(500) NOT NULL DEFAULT '' COMMENT '服务说明',
    enabled TINYINT NOT NULL DEFAULT 1 COMMENT '全局开关',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE KEY uk_sdk_service_code (code),
    CONSTRAINT chk_sdk_service_enabled CHECK (enabled IN (0,1))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='SDK 服务目录及全局开关';

CREATE TABLE sdk_service_channel (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    service_id BIGINT NOT NULL COMMENT '关联 sdk_service.id',
    channel_app_id BIGINT NOT NULL COMMENT '关联 config_channel_app.id',
    env VARCHAR(16) COLLATE utf8mb4_bin NOT NULL COMMENT 'prod/test/review',
    enabled TINYINT NOT NULL DEFAULT 0 COMMENT '目标开关',
    config JSON NOT NULL COMMENT '服务扩展配置',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE KEY uk_sdk_service_channel (service_id,channel_app_id,env),
    KEY idx_sdk_channel_app_env (channel_app_id,env),
    CONSTRAINT fk_sdk_channel_service FOREIGN KEY (service_id)
        REFERENCES sdk_service(id) ON DELETE RESTRICT,
    CONSTRAINT chk_sdk_channel_env CHECK (env IN ('prod','test','review')),
    CONSTRAINT chk_sdk_channel_enabled CHECK (enabled IN (0,1)),
    CONSTRAINT chk_sdk_channel_config CHECK (JSON_TYPE(config)='OBJECT')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='SDK 渠道包功能绑定';

-- 只初始化已实现的事件上报；充值不受此开关影响。
INSERT INTO sdk_service (code,name,description,enabled) VALUES
('event_report','事件上报','游戏事件向发行平台上报',1),
('user_check','核验用户信息','平台核验用户信息并转发至游戏',1);
