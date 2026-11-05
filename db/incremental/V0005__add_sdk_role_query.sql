-- 目标数据库：config_center。历史条件增量；先核对前置表结构和数据，切勿按编号全部顺序执行。
-- 现有 config_center 库手动执行，仅注册角色查询功能。
-- 不执行 DDL，不修改已有渠道包开关，不自动启用渠道环境。
INSERT INTO sdk_service (code, name, description, enabled)
SELECT 'role_query', '角色查询', '平台实时查询游戏角色信息', 1
WHERE NOT EXISTS (SELECT 1 FROM sdk_service WHERE code='role_query');
