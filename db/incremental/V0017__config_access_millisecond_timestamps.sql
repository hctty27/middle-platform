-- 目标数据库：config_access。部署前备份，并在已有副本库手工执行一次。
-- 把节点副本和同步游标的微秒时间列收成毫秒。
-- MySQL 降低小数秒精度时四舍五入到最近的毫秒，不是截断。
-- 列已经是 DATETIME(3) 时重复执行不会改变数据。
ALTER TABLE config_node
    MODIFY created_at DATETIME(3) NOT NULL,
    MODIFY updated_at DATETIME(3) NOT NULL;

ALTER TABLE config_sync_state
    MODIFY updated_at DATETIME(3) NOT NULL;
