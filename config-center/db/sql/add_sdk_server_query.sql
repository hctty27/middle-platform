-- 已有 config_center 库手工执行，不会修改任何已配置的渠道开关。
-- 可重复执行，已存在 server_query 时不会重复创建。
INSERT INTO sdk_service (code, name, description, enabled)
SELECT 'server_query', '区服查询', '平台实时分页查询游戏区服信息', 1
WHERE NOT EXISTS (SELECT 1 FROM sdk_service WHERE code='server_query');
