-- 在已有 config_center 库中手工执行；可重复执行，不改变渠道开关。
INSERT INTO sdk_service (code, name, description, enabled)
SELECT 'role_control', '封禁与解禁', '平台封号、解封、禁言、解禁言统一操作', 1
WHERE NOT EXISTS (SELECT 1 FROM sdk_service WHERE code='role_control');
