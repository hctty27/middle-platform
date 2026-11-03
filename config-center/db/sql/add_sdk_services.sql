-- 现有 config_center 数据库手动执行一次；请先备份并确认两张表不存在。
-- SDK Core 不会自动建表，现有订单和配置表不受影响。
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
  COMMENT='SDK 全局服务定义';

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
  COMMENT='渠道包按环境配置 SDK 服务';

-- 服务目录预置项。是否开放接口仍由对应 Go 业务模块决定。
-- 不包含充值：关闭服务不能中断已确认支付订单的发货补偿。
INSERT INTO sdk_service (code, name, description, enabled, sort_order) VALUES
('event_report', '事件上报', '游戏事件向发行平台上报', 1, 10),
('role_query', '角色查询', '平台查询游戏角色信息', 1, 20),
('server_query', '区服查询', '平台查询区服信息', 1, 30),
('user_check', '用户信息核验', '核验平台用户信息', 1, 40),
('role_ban', '封解禁', '角色封号、禁言及解除', 1, 50),
('subscribe_message', '订阅消息', '微信等渠道的订阅消息', 1, 60);
