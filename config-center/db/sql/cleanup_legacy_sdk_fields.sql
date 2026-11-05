-- 完成迁移核验并切换所有服务后人工执行；执行前保留备份。
UPDATE config_node SET config=JSON_REMOVE(config,'$.gameSecret','$.serviceAdapters')
WHERE node_type='game';
UPDATE config_node SET config=JSON_REMOVE(config,
 '$.payNotifyUrl','$.userCheckUrl','$.roleQueryUrl','$.serverQueryUrl','$.roleControlUrl')
WHERE node_type='region';
SELECT COUNT(*) AS legacy_sdk_fields FROM config_node
WHERE JSON_CONTAINS_PATH(config,'one','$.gameSecret','$.serviceAdapters',
 '$.payNotifyUrl','$.userCheckUrl','$.roleQueryUrl','$.serverQueryUrl','$.roleControlUrl');
