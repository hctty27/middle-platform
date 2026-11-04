-- 已有 config_center 库手工执行，禁止在服务启动时自动执行 DDL。
-- SDK 功能表必须事先存在；重复执行不会创建重复的服务目录。
INSERT INTO sdk_service (code, name, description, enabled)
SELECT 'user_check', '核验用户信息', '平台核验用户信息并转发至游戏', 1
WHERE NOT EXISTS (SELECT 1 FROM sdk_service WHERE code='user_check');
