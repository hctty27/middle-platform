-- 目标库：config_center。仅更新未经自定义的旧用户核验描述，不改变服务开关或绑定。
UPDATE sdk_service
SET description = '游戏经 SDK Service 向平台核验用户信息'
WHERE code = 'user_check' AND description = '平台核验用户信息并转发至游戏';
