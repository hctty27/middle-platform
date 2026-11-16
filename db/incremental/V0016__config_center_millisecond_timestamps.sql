-- 目标数据库：config_center。部署前备份，并在已有库手工执行一次。
-- 把仍为微秒的业务时间列收成毫秒。订单、SDK 服务和 sdk_node_config 已是 DATETIME(3)，这里不改。
-- MySQL 降低小数秒精度时四舍五入到最近的毫秒，不是截断。同步顺序仍以 revision 为准，不依赖这段小数。
-- 列已经是 DATETIME(3) 时重复执行不会改变数据。
ALTER TABLE config_node
    MODIFY created_at DATETIME(3) NOT NULL,
    MODIFY updated_at DATETIME(3) NOT NULL;

ALTER TABLE config_node_change_log
    MODIFY created_at DATETIME(3) NOT NULL;

ALTER TABLE config_subscriber
    MODIFY created_at DATETIME(3) NOT NULL,
    MODIFY updated_at DATETIME(3) NOT NULL;

ALTER TABLE config_subscriber_delivery_state
    MODIFY lease_until DATETIME(3) NULL,
    MODIFY next_attempt_at DATETIME(3) NOT NULL,
    MODIFY updated_at DATETIME(3) NOT NULL;

ALTER TABLE sdk_channel_app
    MODIFY created_at DATETIME(3) NOT NULL,
    MODIFY updated_at DATETIME(3) NOT NULL;
