-- 目标数据库：config_center。历史条件增量；先核对前置表结构和数据，切勿按编号全部顺序执行。
-- 已有库专用：停掉所有服务并备份数据库后执行，不可对新库重复执行。
CREATE TABLE sdk_node_config (
 node_id BIGINT NOT NULL PRIMARY KEY COMMENT '关联 config_node.id',
 config JSON NOT NULL COMMENT 'SDK 节点专属配置',
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 CONSTRAINT fk_sdk_node FOREIGN KEY(node_id) REFERENCES config_node(id) ON DELETE CASCADE,
 CONSTRAINT chk_sdk_node_config CHECK(JSON_TYPE(config)='OBJECT')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='SDK 节点专属配置';

-- 仅迁移明确归属 SDK 的字段；原节点 JSON 保留以便校验。
INSERT INTO sdk_node_config(node_id,config)
SELECT n.id, JSON_MERGE_PATCH(
 CASE WHEN JSON_CONTAINS_PATH(n.config,'one','$.gameSecret') THEN JSON_OBJECT('gameSecret',JSON_EXTRACT(n.config,'$.gameSecret')) ELSE JSON_OBJECT() END,
 CASE WHEN JSON_CONTAINS_PATH(n.config,'one','$.serviceAdapters') THEN JSON_OBJECT('serviceAdapters',JSON_EXTRACT(n.config,'$.serviceAdapters')) ELSE JSON_OBJECT() END,
 CASE WHEN JSON_CONTAINS_PATH(n.config,'one','$.payNotifyUrl') THEN JSON_OBJECT('payNotifyUrl',JSON_EXTRACT(n.config,'$.payNotifyUrl')) ELSE JSON_OBJECT() END,
 CASE WHEN JSON_CONTAINS_PATH(n.config,'one','$.userCheckUrl') THEN JSON_OBJECT('userCheckUrl',JSON_EXTRACT(n.config,'$.userCheckUrl')) ELSE JSON_OBJECT() END,
 CASE WHEN JSON_CONTAINS_PATH(n.config,'one','$.roleQueryUrl') THEN JSON_OBJECT('roleQueryUrl',JSON_EXTRACT(n.config,'$.roleQueryUrl')) ELSE JSON_OBJECT() END,
 CASE WHEN JSON_CONTAINS_PATH(n.config,'one','$.serverQueryUrl') THEN JSON_OBJECT('serverQueryUrl',JSON_EXTRACT(n.config,'$.serverQueryUrl')) ELSE JSON_OBJECT() END,
 CASE WHEN JSON_CONTAINS_PATH(n.config,'one','$.roleControlUrl') THEN JSON_OBJECT('roleControlUrl',JSON_EXTRACT(n.config,'$.roleControlUrl')) ELSE JSON_OBJECT() END
)
FROM config_node n
WHERE n.node_type IN ('game','region') AND JSON_CONTAINS_PATH(n.config,'one',
 '$.gameSecret','$.serviceAdapters','$.payNotifyUrl','$.userCheckUrl','$.roleQueryUrl','$.serverQueryUrl','$.roleControlUrl')
ON DUPLICATE KEY UPDATE node_id=node_id;

-- 保留原渠道包的 ID 与绑定；请确认新表名不存在。
RENAME TABLE config_channel_app TO sdk_channel_app;
SELECT COUNT(*) AS sdk_node_count FROM sdk_node_config;
SELECT COUNT(*) AS sdk_app_count FROM sdk_channel_app;
