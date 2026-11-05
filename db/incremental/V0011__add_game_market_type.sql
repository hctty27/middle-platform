-- 目标数据库：config_center。历史条件增量；先核对前置表结构和数据，切勿按编号全部顺序执行。
-- 已有 config_center 库升级：部署新版前备份，并手工执行一次。
-- 全新安装使用 initialize.sql，不需要执行此脚本。
ALTER TABLE config_node
    ADD COLUMN market_type VARCHAR(16) COLLATE utf8mb4_bin NULL
    COMMENT '游戏运营地区：domestic国内，overseas海外；仅游戏节点使用';

UPDATE config_node SET market_type = 'domestic' WHERE node_type = 'game';

ALTER TABLE config_node
    ADD CONSTRAINT chk_config_node_market_type CHECK (
        (node_type = 'game' AND market_type IS NOT NULL AND market_type IN ('domestic', 'overseas'))
        OR (node_type <> 'game' AND market_type IS NULL)
    );
