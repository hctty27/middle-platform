-- 目标数据库：config_access。已有副本升级：部署新版接入端前备份，并手工执行一次。
-- 全新安装使用 db/full/config_access.sql，不需要执行此脚本。
-- 前置：config_node 已存在且尚无 market_type。游戏节点回填 domestic，与权威库创建时的默认一致；下次同步会用权威快照覆盖。
ALTER TABLE config_node
    ADD COLUMN market_type VARCHAR(16) COLLATE utf8mb4_bin NULL
    COMMENT '游戏运营地区：domestic国内，overseas海外；仅游戏节点使用';

UPDATE config_node SET market_type = 'domestic' WHERE node_type = 'game';

ALTER TABLE config_node
    ADD CONSTRAINT chk_replica_node_market_type CHECK (
        (node_type = 'game' AND market_type IS NOT NULL AND market_type IN ('domestic', 'overseas'))
        OR (node_type <> 'game' AND market_type IS NULL)
    );
