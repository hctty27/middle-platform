-- 目标数据库：config_center。历史条件增量；先核对前置表结构和数据，切勿按编号全部顺序执行。
-- 完成迁移核验并切换所有服务后人工执行；执行前保留备份。
UPDATE config_node SET config=JSON_REMOVE(config,'$.gameSecret','$.serviceAdapters')
WHERE node_type='game';
UPDATE config_node SET config=JSON_REMOVE(config,
 '$.payNotifyUrl','$.userCheckUrl','$.roleQueryUrl','$.serverQueryUrl','$.roleControlUrl')
WHERE node_type='region';
SELECT COUNT(*) AS legacy_sdk_fields FROM config_node
WHERE JSON_CONTAINS_PATH(config,'one','$.gameSecret','$.serviceAdapters',
 '$.payNotifyUrl','$.userCheckUrl','$.roleQueryUrl','$.serverQueryUrl','$.roleControlUrl');
