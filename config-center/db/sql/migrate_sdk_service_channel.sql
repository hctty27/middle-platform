-- 通用服务绑定表回退为渠道包绑定表，只保留 event_report。
-- 适用：数据库存在 sdk_service_binding、sdk_service.scope_type。
-- 手动执行：先全库备份、停止 config-center 和 sdk-core，核对旧表是否为历史备份。
-- DDL 会隐式提交；中途失败不可简单重跑，须先检查实际表结构。
-- 若只有旧 sdk_service_channel 和 sort_order，先运行 migrate_sdk_service_binding.sql。

-- 缺少通用绑定表时主动中止，避免误处理只有旧渠道表的数据库。
SET @has_binding = (SELECT COUNT(*) FROM information_schema.tables
    WHERE table_schema=DATABASE() AND table_name='sdk_service_binding');
SET @guard_sql = IF(@has_binding=1,'SELECT 1',
    'SELECT * FROM sdk_service_binding_not_found_abort_migration');
PREPARE service_migration_stmt FROM @guard_sql;
EXECUTE service_migration_stmt;
DEALLOCATE PREPARE service_migration_stmt;

-- 之前的迁移可能保留同名旧表；先改名归档，禁止覆盖旧记录。
-- 若归档名已存在，则先人工核对并处理该情况，不能直接重跑。
SET @has_old_channel = (SELECT COUNT(*) FROM information_schema.tables
    WHERE table_schema=DATABASE() AND table_name='sdk_service_channel');
SET @archive_sql = IF(@has_old_channel=1,
    'RENAME TABLE sdk_service_channel TO sdk_service_channel_legacy','SELECT 1');
PREPARE service_migration_stmt FROM @archive_sql;
EXECUTE service_migration_stmt;
DEALLOCATE PREPARE service_migration_stmt;

-- 解除历史归档表对服务目录的外键，仅用于保留历史记录，不影响新表。
SET @legacy_fk = (SELECT CONSTRAINT_NAME FROM information_schema.KEY_COLUMN_USAGE
    WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='sdk_service_channel_legacy'
      AND REFERENCED_TABLE_NAME='sdk_service' LIMIT 1);
SET @drop_fk_sql = IF(@legacy_fk IS NULL,'SELECT 1',
    CONCAT('ALTER TABLE sdk_service_channel_legacy DROP FOREIGN KEY `',
           REPLACE(@legacy_fk,'`','``'),'`'));
PREPARE service_migration_stmt FROM @drop_fk_sql;
EXECUTE service_migration_stmt;
DEALLOCATE PREPARE service_migration_stmt;

-- 仅删除未实现功能及对应绑定，保留 event_report 的 ID、环境、开关和配置。
DELETE b FROM sdk_service_binding b
JOIN sdk_service s ON s.id=b.service_id
WHERE s.code<>'event_report';
DELETE FROM sdk_service WHERE code<>'event_report';

RENAME TABLE sdk_service_binding TO sdk_service_channel;

ALTER TABLE sdk_service_channel
    RENAME COLUMN target_id TO channel_app_id,
    DROP INDEX uk_sdk_service_binding,
    ADD UNIQUE KEY uk_sdk_service_channel (service_id,channel_app_id,env),
    DROP INDEX idx_sdk_binding_target,
    ADD KEY idx_sdk_channel_app_env (channel_app_id,env);

ALTER TABLE sdk_service
    DROP CHECK chk_sdk_service_scope,
    DROP COLUMN scope_type;

-- 如果历史目录缺少事件上报，初始化唯一功能定义。
INSERT INTO sdk_service (code,name,description,enabled)
SELECT 'event_report','事件上报','游戏事件向发行平台上报',1
WHERE NOT EXISTS (SELECT 1 FROM sdk_service WHERE code='event_report');

-- 预期：服务目录恰好一条 event_report；孤儿绑定数应为 0。
SELECT id,code,name,enabled FROM sdk_service ORDER BY id;
SELECT COUNT(*) AS binding_count FROM sdk_service_channel;
SELECT COUNT(*) AS orphan_binding_count
FROM sdk_service_channel b
LEFT JOIN config_channel_app a ON a.id=b.channel_app_id
WHERE a.id IS NULL;
SHOW CREATE TABLE sdk_service;
SHOW CREATE TABLE sdk_service_channel;
