-- 现有 config_center 数据库手动执行一次；请先备份并确认两张表不存在。
-- SDK Core 不会自动建表，现有订单和配置表不受影响。
CREATE TABLE sdk_service (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    code VARCHAR(64) COLLATE utf8mb4_bin NOT NULL COMMENT '全局唯一服务编码',
    name VARCHAR(100) NOT NULL COMMENT '服务名称',
    scope_type VARCHAR(32) COLLATE utf8mb4_bin NOT NULL COMMENT 'channel_app/server_group',
    description VARCHAR(500) NOT NULL DEFAULT '' COMMENT '服务说明',
    enabled TINYINT NOT NULL DEFAULT 1 COMMENT '全局开关',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE KEY uk_sdk_service_code (code),
    CONSTRAINT chk_sdk_service_scope CHECK (scope_type IN ('channel_app','server_group')),
    CONSTRAINT chk_sdk_service_enabled CHECK (enabled IN (0,1))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='SDK 服务定义及作用范围';

CREATE TABLE sdk_service_binding (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    service_id BIGINT NOT NULL COMMENT '关联 sdk_service.id',
    target_id BIGINT NOT NULL COMMENT '根据 scope_type 关联渠道应用或区服组',
    env VARCHAR(16) COLLATE utf8mb4_bin NOT NULL COMMENT 'prod/test/review',
    enabled TINYINT NOT NULL DEFAULT 0 COMMENT '目标开关',
    config JSON NOT NULL COMMENT '服务扩展配置',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE KEY uk_sdk_service_binding (service_id,target_id,env),
    KEY idx_sdk_binding_target (target_id,env),
    CONSTRAINT fk_sdk_binding_service FOREIGN KEY (service_id)
        REFERENCES sdk_service(id) ON DELETE RESTRICT,
    CONSTRAINT chk_sdk_binding_env CHECK (env IN ('prod','test','review')),
    CONSTRAINT chk_sdk_binding_enabled CHECK (enabled IN (0,1)),
    CONSTRAINT chk_sdk_binding_config CHECK (JSON_TYPE(config)='OBJECT')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='SDK 服务通用目标绑定';

-- 已有服务默认保持 channel_app，后续新业务单独声明 server_group 作用范围。
-- 充值不属于可关闭服务，不能中断已支付订单补偿。
INSERT INTO sdk_service (code,name,scope_type,description,enabled) VALUES
('event_report','事件上报','channel_app','游戏事件向发行平台上报',1),
('role_query','角色查询','channel_app','平台查询游戏角色信息',1),
('server_query','区服查询','channel_app','平台查询区服信息',1),
('user_check','用户信息核验','channel_app','核验平台用户信息',1),
('role_ban','封解禁','channel_app','角色封号、禁言及解除',1),
('subscribe_message','订阅消息','channel_app','微信等渠道的订阅消息',1),
('server_status','区服状态上报','server_group','预留区服组状态上报能力，尚未实现业务接口',0);
