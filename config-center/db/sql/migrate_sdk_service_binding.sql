-- 已有 config_center 数据库升级：须先备份并停止 config-center 和 sdk-core 的写入/请求。
-- 仅适用旧 sdk_service + sdk_service_channel 存在而新 sdk_service_binding 不存在的情况。
-- 手动执行一次；不可重复运行。旧 sdk_service_channel 保留作审计与回退，升级后禁止旧版服务继续写入。
-- MySQL DDL 会隐式提交，请按顺序逐步执行并人工核对结果。

ALTER TABLE sdk_service
    ADD COLUMN scope_type VARCHAR(32) COLLATE utf8mb4_bin NOT NULL DEFAULT 'channel_app' AFTER name,
    DROP COLUMN sort_order,
    ADD CONSTRAINT chk_sdk_service_scope CHECK (scope_type IN ('channel_app','server_group'));

-- 保留原 id、service_id、渠道目标、环境、状态、config 和时间戳。
CREATE TABLE sdk_service_binding (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    service_id BIGINT NOT NULL,
    target_id BIGINT NOT NULL,
    env VARCHAR(16) COLLATE utf8mb4_bin NOT NULL,
    enabled TINYINT NOT NULL DEFAULT 0,
    config JSON NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE KEY uk_sdk_service_binding (service_id,target_id,env),
    KEY idx_sdk_binding_target (target_id,env),
    CONSTRAINT fk_sdk_binding_service FOREIGN KEY (service_id)
        REFERENCES sdk_service(id) ON DELETE RESTRICT,
    CONSTRAINT chk_sdk_binding_env CHECK (env IN ('prod','test','review')),
    CONSTRAINT chk_sdk_binding_enabled CHECK (enabled IN (0,1)),
    CONSTRAINT chk_sdk_binding_config CHECK (JSON_TYPE(config)='OBJECT')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='SDK 服务通用目标绑定';

INSERT INTO sdk_service_binding (id,service_id,target_id,env,enabled,config,created_at,updated_at)
SELECT id,service_id,channel_app_id,env,enabled,config,created_at,updated_at
FROM sdk_service_channel;

-- 仅新增未实现的区服组服务目录占位，默认关闭。
INSERT INTO sdk_service (code,name,scope_type,description,enabled)
VALUES ('server_status','区服状态上报','server_group','预留区服组状态上报能力，尚未实现业务接口',0);

-- 核对：以下计数应为 0，且新旧记录数相同；未通过不得部署新版。
SELECT (SELECT COUNT(*) FROM sdk_service_channel) AS legacy_count,
       (SELECT COUNT(*) FROM sdk_service_binding) AS binding_count;
SELECT COUNT(*) AS mismatch_count
FROM sdk_service_channel old
LEFT JOIN sdk_service_binding current ON current.id=old.id
WHERE current.id IS NULL OR current.service_id<>old.service_id
   OR current.target_id<>old.channel_app_id OR current.env<>old.env
   OR current.enabled<>old.enabled OR NOT (current.config <=> old.config)
   OR current.created_at<>old.created_at OR current.updated_at<>old.updated_at;

-- 旧表暂不删除。切换后旧代码不能继续使用；确认稳定后另行安排清理。
