-- 目标数据库：config_access。部署前备份，并在维护窗口执行一次。
-- 将本地副本和同步游标的时间列从 DATETIME(3) 统一为 DATETIME(0)，内部按 UTC 约定存储。
-- 先扣除 MICROSECOND，明确截断而不是依赖 MySQL 降精度时的默认四舍五入。
-- 已经是秒级的记录不会被更新；重复执行在数据和结构上均安全。

UPDATE config_node
SET created_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(created_at), created_at),
    updated_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(updated_at), updated_at)
WHERE MICROSECOND(created_at) <> 0 OR MICROSECOND(updated_at) <> 0;

UPDATE config_sync_state
SET updated_at = TIMESTAMPADD(MICROSECOND, -MICROSECOND(updated_at), updated_at)
WHERE MICROSECOND(updated_at) <> 0;

ALTER TABLE config_node
    MODIFY created_at DATETIME(0) NOT NULL,
    MODIFY updated_at DATETIME(0) NOT NULL;

ALTER TABLE config_sync_state
    MODIFY updated_at DATETIME(0) NOT NULL;
