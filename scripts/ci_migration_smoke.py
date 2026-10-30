#!/usr/bin/env python3
"""Go SQL 迁移真实 MySQL V1→V4 与旧四级配置结构回归。"""

import os
import subprocess

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


def migrate(version):
    env = os.environ.copy()
    env["CONFIG_DB_DSN"] = (
        "root:integration-test@tcp(127.0.0.1:3306)/config_migration?parseTime=true&loc=UTC"
    )
    subprocess.run(["/tmp/config-center-migrate-ci", "-target", str(version)],
                   check=True, env=env)


def assert_rows(sql, expected):
    actual = mysql(sql).splitlines()
    assert actual == expected, (actual, expected)


def main():
    mysql("CREATE DATABASE config_migration CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci")
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
    migrate(4)  # 幂等性校验
    # 验证已有历史 V4 记录的数据库能直接接入 Go，不重新执行破坏性 SQL。
    mysql("""
        DROP TABLE config_migration.config_schema_migrations;
        CREATE TABLE config_migration.flyway_schema_history (
            installed_rank INT PRIMARY KEY, version VARCHAR(50), success TINYINT NOT NULL
        );
        INSERT INTO config_migration.flyway_schema_history(installed_rank,version,success)
        VALUES(1,'4',1);
    """)
    migrate(4)
    assert mysql("SELECT version FROM config_migration.config_schema_migrations WHERE id=1") == "4"
    assert mysql("SELECT revision FROM config_migration.config_revision WHERE id=1") == "7"
    print("PASS Go SQL migrations V1-V4, legacy V4 baseline and idempotence", flush=True)


if __name__ == "__main__":
    main()
