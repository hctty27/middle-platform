-- 从当前通用绑定表迁移到渠道包专用绑定表，仅保留事件上报功能。
-- 适用：sdk_service_binding 和 sdk_service.scope_type 存在的当前版本。
-- 必须先备份 config_center、停止 config-center 与 sdk-core，再人工执行。
-- 此脚本只执行一次；MySQL DDL 隐式提交，失败后先核对现状，不能直接重跑。

-- 先重命名；若目标表已存在或源表不存在，直接失败且不删除任何数据。
RENAME TABLE sdk_service_binding TO sdk_service_channel;

-- 外键和 CHECK 约束统一为渠道包命名，不保留通用绑定的历史约束名。
ALTER TABLE sdk_service_channel
    DROP FOREIGN KEY fk_sdk_binding_service;

ALTER TABLE sdk_service_channel
    RENAME COLUMN target_id TO channel_app_id,
    DROP INDEX uk_sdk_service_binding,
    ADD UNIQUE KEY uk_sdk_service_channel (service_id,channel_app_id,env),
    DROP INDEX idx_sdk_binding_target,
    ADD KEY idx_sdk_channel_app_env (channel_app_id,env),
    DROP CHECK chk_sdk_binding_env,
    DROP CHECK chk_sdk_binding_enabled,
    DROP CHECK chk_sdk_binding_config,
    ADD CONSTRAINT chk_sdk_channel_env CHECK (env IN ('prod','test','review')),
    ADD CONSTRAINT chk_sdk_channel_enabled CHECK (enabled IN (0,1)),
    ADD CONSTRAINT chk_sdk_channel_config CHECK (JSON_TYPE(config)='OBJECT');

ALTER TABLE sdk_service_channel
    ADD CONSTRAINT fk_sdk_channel_service FOREIGN KEY (service_id)
    REFERENCES sdk_service(id) ON DELETE RESTRICT;

-- 清理未实现功能的绑定和定义；保留事件上报的绑定 ID、环境、开关和 JSON 配置。
DELETE ch FROM sdk_service_channel ch
JOIN sdk_service sv ON sv.id=ch.service_id
WHERE sv.code<>'event_report';

DELETE FROM sdk_service WHERE code<>'event_report';

ALTER TABLE sdk_service
    DROP CHECK chk_sdk_service_scope,
    DROP COLUMN scope_type;

-- 功能目录只初始化已实现的 event_report。
INSERT INTO sdk_service (code,name,description,enabled)
SELECT 'event_report','事件上报','游戏事件向发行平台上报',1
WHERE NOT EXISTS (SELECT 1 FROM sdk_service WHERE code='event_report');

-- 预期：服务定义恰好一条 event_report；渠道绑定无孤儿数据。
SELECT id,code,name,enabled FROM sdk_service ORDER BY id;
SELECT COUNT(*) AS channel_binding_count FROM sdk_service_channel;
SELECT COUNT(*) AS orphan_binding_count
FROM sdk_service_channel ch
LEFT JOIN sdk_channel_app app ON app.id=ch.channel_app_id
WHERE app.id IS NULL;

SHOW CREATE TABLE sdk_service;
SHOW CREATE TABLE sdk_service_channel;
