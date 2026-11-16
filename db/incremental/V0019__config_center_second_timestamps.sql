-- 目标数据库：config_center。部署前备份，并在维护窗口执行一次。
-- 将全部业务时间列从 DATETIME(3) 统一为 DATETIME(0)，内部仍按 UTC 约定存储。
-- 先用 TIMESTAMPADD 扣除 MICROSECOND，明确截断而不是依赖 MySQL 降精度时的默认四舍五入。
-- updated_at 与 created_at 在同一条 UPDATE 中显式赋值，避免触发 ON UPDATE 自动刷新。
-- 已经是秒级的记录不会被更新；重复执行在数据和结构上均安全。

UPDATE config_node
SET created_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(created_at), created_at),
    updated_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(updated_at), updated_at)
WHERE MICROSECOND(created_at) <> 0 OR MICROSECOND(updated_at) <> 0;

UPDATE sdk_node_config
SET created_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(created_at), created_at),
    updated_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(updated_at), updated_at)
WHERE MICROSECOND(created_at) <> 0 OR MICROSECOND(updated_at) <> 0;

UPDATE config_node_change_log
SET created_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(created_at), created_at)
WHERE MICROSECOND(created_at) <> 0;

UPDATE config_subscriber
SET created_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(created_at), created_at),
    updated_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(updated_at), updated_at)
WHERE MICROSECOND(created_at) <> 0 OR MICROSECOND(updated_at) <> 0;

UPDATE config_subscriber_delivery_state
SET lease_until = TIMESTAMPADD(MICROSECOND, -MICROSECOND(lease_until), lease_until),
    next_attempt_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(next_attempt_at), next_attempt_at),
    updated_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(updated_at), updated_at)
WHERE COALESCE(MICROSECOND(lease_until), 0) <> 0
   OR MICROSECOND(next_attempt_at) <> 0
   OR MICROSECOND(updated_at) <> 0;

UPDATE sdk_channel_app
SET created_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(created_at), created_at),
    updated_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(updated_at), updated_at)
WHERE MICROSECOND(created_at) <> 0 OR MICROSECOND(updated_at) <> 0;

UPDATE game_order
SET paid_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(paid_at), paid_at),
    last_notify_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(last_notify_at), last_notify_at),
    notify_success_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(notify_success_at), notify_success_at),
    next_notify_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(next_notify_at), next_notify_at),
    created_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(created_at), created_at),
    updated_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(updated_at), updated_at)
WHERE COALESCE(MICROSECOND(paid_at), 0) <> 0
   OR COALESCE(MICROSECOND(last_notify_at), 0) <> 0
   OR COALESCE(MICROSECOND(notify_success_at), 0) <> 0
   OR COALESCE(MICROSECOND(next_notify_at), 0) <> 0
   OR MICROSECOND(created_at) <> 0
   OR MICROSECOND(updated_at) <> 0;

UPDATE sdk_service
SET created_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(created_at), created_at),
    updated_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(updated_at), updated_at)
WHERE MICROSECOND(created_at) <> 0 OR MICROSECOND(updated_at) <> 0;

UPDATE sdk_service_channel
SET created_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(created_at), created_at),
    updated_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(updated_at), updated_at)
WHERE MICROSECOND(created_at) <> 0 OR MICROSECOND(updated_at) <> 0;

ALTER TABLE config_node
    MODIFY created_at DATETIME(0) NOT NULL,
    MODIFY updated_at DATETIME(0) NOT NULL;

ALTER TABLE sdk_node_config
    MODIFY created_at DATETIME(0) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    MODIFY updated_at DATETIME(0) NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;

ALTER TABLE config_node_change_log
    MODIFY created_at DATETIME(0) NOT NULL;

ALTER TABLE config_subscriber
    MODIFY created_at DATETIME(0) NOT NULL,
    MODIFY updated_at DATETIME(0) NOT NULL;

ALTER TABLE config_subscriber_delivery_state
    MODIFY lease_until DATETIME(0) NULL,
    MODIFY next_attempt_at DATETIME(0) NOT NULL,
    MODIFY updated_at DATETIME(0) NOT NULL;

ALTER TABLE sdk_channel_app
    MODIFY created_at DATETIME(0) NOT NULL,
    MODIFY updated_at DATETIME(0) NOT NULL;

ALTER TABLE game_order
    MODIFY paid_at DATETIME(0) DEFAULT NULL,
    MODIFY last_notify_at DATETIME(0) DEFAULT NULL,
    MODIFY notify_success_at DATETIME(0) DEFAULT NULL,
    MODIFY next_notify_at DATETIME(0) DEFAULT NULL,
    MODIFY created_at DATETIME(0) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    MODIFY updated_at DATETIME(0) NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;

ALTER TABLE sdk_service
    MODIFY created_at DATETIME(0) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    MODIFY updated_at DATETIME(0) NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;

ALTER TABLE sdk_service_channel
    MODIFY created_at DATETIME(0) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    MODIFY updated_at DATETIME(0) NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;
