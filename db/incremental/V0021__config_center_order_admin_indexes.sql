-- 目标库: config_center。仅用于已有 game_order 且缺少下列索引的数据库。
-- 备份并检查 SHOW INDEX FROM game_order；低峰维护窗口人工执行，不得重复执行。
ALTER TABLE game_order
    ADD INDEX idx_game_env_created (game_id,env,created_at,id),
    ADD INDEX idx_game_env_paid (game_id,env,paid_at),
    ADD INDEX idx_game_order_no (game_id,game_order_id),
    ADD INDEX idx_game_notify (game_id,payment_status,notify_status,env),
    ALGORITHM=INPLACE, LOCK=NONE;
