-- 目标数据库：config_center。历史条件增量；先核对前置表结构和数据，切勿按编号全部顺序执行。
-- 存量 config_center 库手动执行；可重复执行，不修改现有渠道环境开关。
INSERT INTO sdk_service (code, name, description, enabled)
SELECT 'subscribe_message', '订阅消息推送', '游戏经 SDK Service 向平台发送订阅消息', 1
WHERE NOT EXISTS (SELECT 1 FROM sdk_service WHERE code='subscribe_message');
