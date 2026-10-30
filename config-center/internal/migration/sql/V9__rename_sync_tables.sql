-- V9：统一节点变更日志与订阅投递状态表名；历史迁移 V1-V8 不修改。
-- 升级前请备份权威库，并确保所有 config-center 实例停写。
-- 单条 RENAME TABLE 原子重命名两张表，保留全部数据、索引及外键关联。
RENAME TABLE
    config_change_log TO config_node_change_log,
    config_delivery_state TO config_subscriber_delivery_state;

-- 补充正式表中文说明；不修改字段定义或业务数据。
ALTER TABLE config_node COMMENT = '游戏、大区、区服组三级配置节点';
ALTER TABLE config_channel_app COMMENT = '游戏级渠道应用公共配置';
ALTER TABLE config_channel_app_binding COMMENT = '渠道应用与区服组绑定及配置覆盖';
ALTER TABLE config_revision COMMENT = '全局配置同步世代与版本游标';
ALTER TABLE config_node_change_log COMMENT = '三级配置节点变更日志';
ALTER TABLE config_subscriber COMMENT = '配置订阅者与回调接入信息';
ALTER TABLE config_subscriber_delivery_state COMMENT = '订阅者回调投递状态、租约和失败重试';
ALTER TABLE config_schema_migrations COMMENT = '配置中心数据库迁移版本记录';
