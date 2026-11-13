-- 目标库：config_center。执行前备份；不要重跑会删除配置的 V0014。
-- 仅补回缺失的目录，保留已有的全局开关、描述及渠道环境绑定。
-- V0014 已删除的历史绑定无法由本脚本恢复；未绑定的用户核验默认开启。
INSERT INTO sdk_service (code, name, description, enabled)
SELECT 'user_check', '核验用户信息', '游戏经 SDK Service 向平台核验用户信息', 1
WHERE NOT EXISTS (SELECT 1 FROM sdk_service WHERE code='user_check');
