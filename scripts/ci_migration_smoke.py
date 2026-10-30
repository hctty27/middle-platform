#!/usr/bin/env python3
"""Go SQL 迁移真实 MySQL V1→V10 与旧版配置结构回归。"""

import os
import subprocess
import re
from pathlib import Path

CONTAINER = subprocess.check_output(
    ["docker", "ps", "--filter", "ancestor=mysql:8.4", "--format", "{{.ID}}"],
    text=True,
).strip().splitlines()[0]


def mysql(statement):
    proc = subprocess.run(
        ["docker", "exec", "-i", CONTAINER, "mysql", "-uroot", "-pintegration-test", "-N", "-B"],
        input=statement, capture_output=True, text=True, check=True,
    )
    return proc.stdout.strip()


def migrate(version, repair=False, expected_success=True):
    env = os.environ.copy()
    env["CONFIG_DB_DSN"] = (
        "root:integration-test@tcp(127.0.0.1:3306)/config_migration?parseTime=true&loc=UTC"
    )
    command = ["/tmp/config-center-migrate-ci", "-target", str(version)]
    if repair:
        command.append("-repair-v6-gtid")
    result = subprocess.run(command, env=env, capture_output=True, text=True)
    if (result.returncode == 0) != expected_success:
        raise AssertionError(f"unexpected migration status {result.returncode}: {result.stderr}")


def assert_rows(sql, expected):
    actual = mysql(sql).splitlines()
    assert actual == expected, (actual, expected)


def main():
    mysql("CREATE DATABASE config_migration CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci")
    # 在 GTID 强制一致性环境中验证迁移；不用关闭 binlog 或 GTID 绕过限制。
    mysql("""SET GLOBAL enforce_gtid_consistency=ON;
        SET GLOBAL gtid_mode=OFF_PERMISSIVE;
        SET GLOBAL gtid_mode=ON_PERMISSIVE;
        SET GLOBAL gtid_mode=ON;""")
    assert mysql("SELECT @@GLOBAL.gtid_mode,@@GLOBAL.enforce_gtid_consistency") == "ON\tON"
    migrate(1)
    assert mysql("SELECT version FROM config_migration.config_schema_migrations WHERE id=1") == "1"
    mysql("""
        INSERT INTO config_migration.config_node
            (id,parent_id,node_type,code,name,status,config,remark,created_at,updated_at)
        VALUES
            (101,0,'game','legacy-game','Legacy Game',1,'{}',NULL,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)),
            (102,101,'region','legacy-region','Legacy Region',1,'{}',NULL,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)),
            (103,102,'server_group','legacy-group','Legacy Group',1,'{}',NULL,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)),
            (104,103,'channel','legacy-channel','Legacy Channel',1,'{}',NULL,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)),
            (105,104,'sdk_app','legacy-sdk','Legacy SDK',1,'{}',NULL,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6));
        UPDATE config_migration.config_revision SET revision=7 WHERE id=1;
        INSERT INTO config_migration.config_subscriber
            (id,code,callback_url,webhook_secret,enabled,created_at,updated_at)
        VALUES
            (77,'legacy-client','http://127.0.0.1:9999/internal/config/callback','test',1,
             UTC_TIMESTAMP(6),UTC_TIMESTAMP(6));
        INSERT INTO config_migration.config_delivery_state
            (subscriber_id,acknowledged_revision,next_attempt_at,updated_at)
        VALUES
            (77,7,DATE_ADD(UTC_TIMESTAMP(6), INTERVAL 1 DAY),UTC_TIMESTAMP(6));
    """)
    migrate(2)
    assert_rows("""
        SELECT id,COALESCE(game_id,0),COALESCE(region_id,0),
               COALESCE(server_group_id,0),COALESCE(channel_id,0)
        FROM config_migration.config_node ORDER BY id;
    """, [
        "101\t0\t0\t0\t0",
        "102\t101\t0\t0\t0",
        "103\t101\t102\t0\t0",
        "104\t101\t102\t103\t0",
        "105\t101\t102\t103\t104",
    ])
    migrate(3)
    assert mysql("SELECT version FROM config_migration.config_schema_migrations WHERE id=1") == "3"
    assert mysql("SELECT acknowledged_revision,COALESCE(acknowledged_epoch,'NULL') "
                 "FROM config_migration.config_delivery_state WHERE subscriber_id=77") == "7\tNULL"
    old_epoch = mysql("SELECT epoch FROM config_migration.config_revision WHERE id=1")
    migrate(4)
    assert_rows("""
        SELECT id,node_type,COALESCE(game_id,0),COALESCE(region_id,0),
               COALESCE(server_group_id,0)
        FROM config_migration.config_node ORDER BY id;
    """, [
        "101\tgame\t0\t0\t0",
        "102\tregion\t101\t0\t0",
        "103\tserver_group\t101\t102\t0",
        "105\tchannel_app\t101\t102\t103",
    ])
    assert_rows("SELECT id,node_type,COALESCE(channel_id,0) FROM "
                "config_migration.config_node_v4_legacy_archive ORDER BY id", [
        "104\tchannel\t0",
        "105\tsdk_app\t104",
    ])
    assert mysql("SELECT version FROM config_migration.config_schema_migrations WHERE id=1") == "4"
    assert mysql("SELECT epoch FROM config_migration.config_revision WHERE id=1") != old_epoch
    assert mysql("SELECT revision FROM config_migration.config_revision WHERE id=1") == "7"
    migrate(5)
    assert mysql("SELECT version FROM config_migration.config_schema_migrations WHERE id=1") == "5"
    assert mysql("SELECT COALESCE(env_type,'unclassified') FROM config_migration.config_node WHERE id=102") == "unclassified"
    assert mysql("SELECT epoch FROM config_migration.config_revision WHERE id=1") != old_epoch
    mysql("UPDATE config_migration.config_node SET env_type='test' WHERE id=102")
    assert mysql("SELECT env_type FROM config_migration.config_node WHERE id=102") == "test"
    migrate(5)  # 幂等性校验
    # 模拟已有 V4 结构且仅有历史 Flyway 记录，验证 V5 可以在旧表上直接升级。
    # 回归数据库专用：撤销 V5 增量列，不触碰任何生产数据。
    mysql("""
        ALTER TABLE config_migration.config_node
            DROP CHECK chk_config_node_env_type, DROP COLUMN env_type;
        DROP TABLE config_migration.config_schema_migrations;
        CREATE TABLE config_migration.flyway_schema_history (
            installed_rank INT PRIMARY KEY, version VARCHAR(50), success TINYINT NOT NULL
        );
        INSERT INTO config_migration.flyway_schema_history(installed_rank,version,success)
        VALUES(1,'4',1);
    """)
    migrate(5)
    assert mysql("SELECT version FROM config_migration.config_schema_migrations WHERE id=1") == "5"
    assert mysql("SELECT revision FROM config_migration.config_revision WHERE id=1") == "7"
    # 测试 V6 归档前中断的实际脏状态：先执行原脚本的两张表，再标记 dirty=1。
    v6 = Path("config-center/internal/migration/sql/V6__shared_channel_apps.sql").read_text(encoding="utf-8")
    assert not re.search(r"CREATE\\s+TABLE\\s+[^;]+?\\s+AS\\s+SELECT", v6, flags=re.I | re.S)
    prefix = v6.split("-- 历史数据保留原始 id 和配置，方便迁移后人工核对。")[0]
    first_ddl = prefix[prefix.index("CREATE TABLE config_channel_app ("):]
    assert first_ddl.count("CREATE TABLE ") == 2
    mysql("USE config_migration; " + first_ddl)
    mysql("UPDATE config_migration.config_schema_migrations SET dirty=1 WHERE id=1")
    migrate(6, expected_success=False)  # 默认禁止无检查地重跑 dirty 迁移
    # 注入业务数据时恢复工具必须拒绝清理。
    mysql("""INSERT INTO config_migration.config_channel_app
      (id,game_id,code,name,status,config,created_at,updated_at)
      VALUES (9001,101,'manual-app','Manual app',1,'{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))""")
    migrate(6, repair=True, expected_success=False)
    assert mysql("SELECT COUNT(*) FROM config_migration.config_channel_app WHERE id=9001") == "1"
    mysql("DELETE FROM config_migration.config_channel_app WHERE id=9001")
    # 仅清理上述两张空表及 dirty 标记，然后自动继续应用修复后的 V6。
    migrate(6, repair=True)
    assert mysql("SELECT version,dirty FROM config_migration.config_schema_migrations WHERE id=1") == "6\t0"
    assert mysql("SELECT version FROM config_migration.config_schema_migrations WHERE id=1") == "6"
    assert_rows("SELECT id,game_id,code,config FROM config_migration.config_channel_app", [
        '105\t101\tlegacy-sdk\t{}'
    ])
    assert_rows("SELECT id,channel_app_id,server_group_id,config FROM config_migration.config_channel_app_binding", [
        '105\t105\t103\t{}'
    ])
    assert mysql("SELECT COUNT(*) FROM config_migration.config_node WHERE node_type='channel_app'") == "0"
    assert mysql("SELECT COUNT(*) FROM config_migration.config_node_v6_channel_app_archive") == "1"
    migrate(6)  # V6 幂等性校验

    # V7 删除节点冗余列，但应用游戏归属和区服组绑定必须保持。
    migrate(7)
    assert mysql("SELECT version,dirty FROM config_migration.config_schema_migrations WHERE id=1") == "7\t0"
    assert mysql("""SELECT COUNT(*) FROM information_schema.columns
        WHERE table_schema='config_migration' AND table_name='config_node'
        AND column_name='server_group_id'""") == "0"
    assert_rows("""SELECT id,node_type,COALESCE(game_id,0),COALESCE(region_id,0)
        FROM config_migration.config_node ORDER BY id""", [
        "101\tgame\t0\t0",
        "102\tregion\t101\t0",
        "103\tserver_group\t101\t102",
    ])
    assert_rows("SELECT id,game_id,code FROM config_migration.config_channel_app", [
        "105\t101\tlegacy-sdk"
    ])
    assert_rows("SELECT channel_app_id,server_group_id FROM config_migration.config_channel_app_binding", [
        "105\t103"
    ])
    migrate(7)  # V7 幂等性校验

    # V8 只切换 epoch，保留节点 revision 和渠道业务数据。
    before_v8_epoch = mysql("SELECT epoch FROM config_migration.config_revision WHERE id=1")
    migrate(8)
    assert mysql("SELECT version,dirty FROM config_migration.config_schema_migrations WHERE id=1") == "8\t0"
    after_v8_epoch = mysql("SELECT epoch FROM config_migration.config_revision WHERE id=1")
    assert after_v8_epoch != before_v8_epoch
    assert mysql("SELECT revision FROM config_migration.config_revision WHERE id=1") == "7"
    assert mysql("SELECT COUNT(*) FROM config_migration.config_channel_app_binding") == "1"
    migrate(8)
    assert mysql("SELECT epoch FROM config_migration.config_revision WHERE id=1") == after_v8_epoch

    # 一些历史 V4/Flyway 基线没有生成 V4 归档；它仅供审计，不应阻止运行态 V8 校验。
    # 本测试只操作独立的 CI 数据库，绝不可对真实业务库删除历史归档。
    mysql("DROP TABLE config_migration.config_node_v4_legacy_archive")
    migrate(8)
    assert mysql("SELECT version,dirty FROM config_migration.config_schema_migrations WHERE id=1") == "8\t0"
    assert mysql("SELECT COUNT(*) FROM config_migration.config_node_v6_channel_app_archive") == "1"
    assert mysql("SELECT COUNT(*) FROM config_migration.config_channel_app_binding") == "1"

    # V6 归档是真正的强制依赖，不能因为 V4 归档可选而放宽其校验。
    mysql("DROP TABLE config_migration.config_node_v6_channel_app_archive")
    migrate(8, expected_success=False)

    # V9 允许归档在管理员外部备份后移除，不能阻断正在运行的配置中心。
    # 两张活动表必须原子重命名，不丢失订阅游标，也不改变节点 revision/epoch。
    before_v9_epoch = mysql("SELECT epoch FROM config_migration.config_revision WHERE id=1")
    before_v9_revision = mysql("SELECT revision FROM config_migration.config_revision WHERE id=1")
    migrate(9)
    assert mysql("SELECT version,dirty FROM config_migration.config_schema_migrations WHERE id=1") == "9\t0"
    assert mysql("SELECT epoch FROM config_migration.config_revision WHERE id=1") == before_v9_epoch
    assert mysql("SELECT revision FROM config_migration.config_revision WHERE id=1") == before_v9_revision
    assert mysql("SELECT acknowledged_revision FROM config_migration.config_subscriber_delivery_state WHERE subscriber_id=77") == "7"
    assert mysql("SELECT COUNT(*) FROM config_migration.config_node_change_log") == "0"
    assert mysql("SELECT COUNT(*) FROM config_migration.config_channel_app_binding") == "1"
    assert mysql("""SELECT COUNT(*) FROM information_schema.tables
        WHERE table_schema='config_migration' AND table_name IN
        ('config_change_log','config_delivery_state')""") == "0"
    assert mysql("""SELECT COUNT(*) FROM information_schema.tables
        WHERE table_schema='config_migration' AND table_name IN
        ('config_node_change_log','config_subscriber_delivery_state')""") == "2"
    migrate(9)
    assert mysql("SELECT epoch FROM config_migration.config_revision WHERE id=1") == before_v9_epoch

    # V10 将旧 production 大区迁移为 prod，保持 revision 并切换 epoch。
    mysql("UPDATE config_migration.config_node SET env_type='production' WHERE id=102")
    before_v10_epoch = mysql("SELECT epoch FROM config_migration.config_revision WHERE id=1")
    before_v10_revision = mysql("SELECT revision FROM config_migration.config_revision WHERE id=1")
    migrate(10)
    assert mysql("SELECT version,dirty FROM config_migration.config_schema_migrations WHERE id=1").split() == ["10", "0"]
    assert mysql("SELECT env_type FROM config_migration.config_node WHERE id=102") == "prod"
    assert mysql("SELECT revision FROM config_migration.config_revision WHERE id=1") == before_v10_revision
    after_v10_epoch = mysql("SELECT epoch FROM config_migration.config_revision WHERE id=1")
    assert after_v10_epoch != before_v10_epoch
    try:
        mysql("UPDATE config_migration.config_node SET env_type='production' WHERE id=102")
        raise AssertionError("legacy production value was accepted after V10")
    except subprocess.CalledProcessError:
        pass
    migrate(10)
    assert mysql("SELECT epoch FROM config_migration.config_revision WHERE id=1") == after_v10_epoch

    # V11 全局唯一预检：不同游戏使用相同渠道 code 时不能迁移，且 dirty 不变。
    mysql("""INSERT INTO config_migration.config_node
        (id,node_type,code,name,status,config,created_at,updated_at)
        VALUES (201,'game','game-other','Other Game',1,'{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))""")
    mysql("""INSERT INTO config_migration.config_channel_app
        (id,game_id,code,name,status,config,created_at,updated_at)
        VALUES (906,201,'legacy-sdk','Duplicated',1,'{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))""")
    migrate(11, expected_success=False)
    assert mysql("SELECT version,dirty FROM config_migration.config_schema_migrations WHERE id=1").split() == ["10", "0"]
    mysql("DELETE FROM config_migration.config_channel_app WHERE id=906")

    # 旧绑定独立状态不能无声丢失；预检失败不会标记 dirty。
    mysql("UPDATE config_migration.config_channel_app_binding SET status=0 WHERE channel_app_id=105")
    migrate(11, expected_success=False)
    assert mysql("SELECT version,dirty FROM config_migration.config_schema_migrations WHERE id=1").split() == ["10", "0"]
    mysql("UPDATE config_migration.config_channel_app_binding SET status=1 WHERE channel_app_id=105")

    # 校验唯一环境下的历史覆盖字段能上升为公共配置。
    mysql("""UPDATE config_migration.config_channel_app_binding
        SET config='{"sdkKey":"live"}' WHERE channel_app_id=105""")
    # V11 安全收敛：唯一旧环境绑定的有效 JSON 迁入应用公共配置。
    migrate(11)
    assert mysql("SELECT version,dirty FROM config_migration.config_schema_migrations WHERE id=1").split() == ["11", "0"]
    assert mysql("SELECT prod_server_group_id FROM config_migration.config_channel_app WHERE id=105") == "103"
    assert mysql("SELECT JSON_UNQUOTE(JSON_EXTRACT(config, '$.sdkKey')) FROM config_migration.config_channel_app WHERE id=105") == "live"
    assert mysql("""SELECT COUNT(*) FROM information_schema.tables
        WHERE table_schema='config_migration' AND table_name='config_channel_app_binding'""") == "0"
    migrate(11)
    # 升级后的唯一索引确保跨游戏写入同 code 会失败，不影响其他 code。
    assert mysql("""SELECT COUNT(*) FROM information_schema.statistics
        WHERE table_schema='config_migration' AND table_name='config_channel_app'
          AND index_name='uq_channel_app_code' AND column_name='code' AND non_unique=0""") == "1"
    try:
        mysql("""INSERT INTO config_migration.config_channel_app
            (game_id,code,name,status,config,created_at,updated_at)
            VALUES (201,'legacy-sdk','Duplicated',1,'{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))""")
        raise AssertionError("global duplicate channel code accepted after V11")
    except subprocess.CalledProcessError:
        pass
    mysql("""INSERT INTO config_migration.config_channel_app
        (game_id,code,name,status,config,created_at,updated_at)
        VALUES (201,'other-sdk','Other SDK',1,'{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))""")

    print("PASS GTID-safe V1-V11 migration, global code uniqueness and environment binding collapse", flush=True)


if __name__ == "__main__":
    main()
