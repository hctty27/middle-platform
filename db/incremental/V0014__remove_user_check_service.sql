-- 目标库：config_center。升级 SDK Service 后手工执行；执行前备份。
-- 将永久删除 user_check 的渠道环境开关及其扩展配置，然后删除服务目录记录。
START TRANSACTION;
DELETE ch FROM sdk_service_channel AS ch
JOIN sdk_service AS sv ON sv.id = ch.service_id
WHERE sv.code = 'user_check';
DELETE FROM sdk_service WHERE code = 'user_check';
COMMIT;
