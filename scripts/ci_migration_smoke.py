#!/usr/bin/env python3
"""在 MySQL 真实实例上验证 V11 基线、V12 订单表迁移、GTID 与旧库拒绝策略。"""

import os
import subprocess

MYSQL = ["mysql", "--protocol=tcp", "--host=127.0.0.1", "--user=root", "--batch", "--skip-column-names"]


def mysql(statement):
    env = os.environ.copy()
    env["MYSQL_PWD"] = "integration-test"
    return subprocess.run(MYSQL, input=statement, text=True, capture_output=True, check=True, env=env).stdout.strip()


def migrate(database="config_migration", target=None, expected_success=True):
    env = os.environ.copy()
    env["CONFIG_DB_DSN"] = (
        f"root:integration-test@tcp(127.0.0.1:3306)/{database}?parseTime=true&loc=UTC"
    )
    command = ["/tmp/config-center-migrate-ci"]
    if target is not None:
        command.extend(["-target", str(target)])
    result = subprocess.run(command, env=env, capture_output=True, text=True)
    if (result.returncode == 0) != expected_success:
        raise AssertionError(f"unexpected exit={result.returncode}: {result.stderr}")


def assert_sql(statement, expected):
    actual = mysql(statement)
    assert actual == expected, (statement, actual, expected)


def main():
    mysql("""CREATE DATABASE config_migration CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
        CREATE DATABASE config_migration_legacy CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
        CREATE DATABASE config_migration_unknown CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
        CREATE DATABASE config_migration_fresh CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
        CREATE DATABASE config_migration_existing_order CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;""")
    # 与生产 GTID + ROW 的 MySQL 行为保持一致。
    mysql("""SET GLOBAL enforce_gtid_consistency=ON;
        SET GLOBAL gtid_mode=OFF_PERMISSIVE;
        SET GLOBAL gtid_mode=ON_PERMISSIVE;
        SET GLOBAL gtid_mode=ON;""")
    assert_sql("SELECT @@GLOBAL.gtid_mode,@@GLOBAL.enforce_gtid_consistency", "ON\tON")

    migrate(target=11)
    assert_sql("SELECT version,dirty FROM config_migration.config_schema_migrations WHERE id=1", "11\t0")
    assert_sql("""SELECT COUNT(*) FROM information_schema.tables
        WHERE table_schema='config_migration'
          AND table_name IN ('config_node','config_revision','config_node_change_log',
                             'config_subscriber','config_subscriber_delivery_state',
                             'config_channel_app','config_schema_migrations')""", "7")
    assert_sql("""SELECT COUNT(*) FROM information_schema.tables
        WHERE table_schema='config_migration'
        AND table_name IN ('config_channel_app_binding','config_change_log',
                           'config_delivery_state','config_node_v6_channel_app_archive')""", "0")

    # 基线必须具备正式/测试/提审三种独立绑定和全局唯一 code。
    mysql("""INSERT INTO config_migration.config_node
        (id,game_id,region_id,node_type,env_type,code,name,status,config,created_at,updated_at)
        VALUES
        (101,NULL,NULL,'game',NULL,'game-1','Game',1,'{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)),
        (102,101,NULL,'region','prod','region-1','Region',1,'{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)),
        (103,101,102,'server_group',NULL,'group-1','Group',1,'{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)),
        (201,NULL,NULL,'game',NULL,'game-2','Game 2',1,'{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6));
        INSERT INTO config_migration.config_channel_app
          (game_id,code,name,status,config,prod_server_group_id,created_at,updated_at)
        VALUES
          (101,'app-1','App 1',1,'{"sdkKey":"value"}',103,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6));""")
    assert_sql("""SELECT prod_server_group_id,
        JSON_UNQUOTE(JSON_EXTRACT(config,'$.sdkKey'))
        FROM config_migration.config_channel_app WHERE code='app-1'""", "103\tvalue")
    try:
        mysql("""INSERT INTO config_migration.config_channel_app
          (game_id,code,name,status,config,created_at,updated_at)
          VALUES (201,'app-1','Duplicate',1,'{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6));""")
        raise AssertionError("global code uniqueness was not enforced")
    except subprocess.CalledProcessError:
        pass
    try:
        mysql("UPDATE config_migration.config_node SET env_type='production' WHERE id=102")
        raise AssertionError("obsolete environment accepted")
    except subprocess.CalledProcessError:
        pass
    migrate()  # V11 -> V12：仅创建订单表，保留渠道应用与配置节点。
    assert_sql("SELECT COUNT(*) FROM config_migration.config_channel_app WHERE code='app-1'", "1")
    assert_sql("SELECT version,dirty FROM config_migration.config_schema_migrations WHERE id=1", "12\t0")
    assert_sql("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='config_migration' AND table_name='game_order'", "1")

    # 订单记录与唯一键在同一个 config_center 类型的权威库中，不能跨 AppID/订单号重复。
    mysql("""INSERT INTO config_migration.game_order
        (appid,game_id,env,game_order_id,amount,attach,game_notify_url)
        VALUES ('app-1',101,'prod','game-100',600,'','https://game.example.com/notify');""")
    try:
        mysql("""INSERT INTO config_migration.game_order
            (appid,game_id,env,game_order_id,amount,attach,game_notify_url)
            VALUES ('app-1',101,'prod','game-100',600,'','https://game.example.com/notify');""")
        raise AssertionError("order business uniqueness not enforced")
    except subprocess.CalledProcessError:
        pass
    migrate()  # V12 重复运行不重建订单表，也不修改业务记录。
    assert_sql("SELECT version,dirty FROM config_migration.config_schema_migrations WHERE id=1", "12\t0")
    assert_sql("SELECT COUNT(*) FROM config_migration.game_order WHERE game_order_id='game-100'", "1")

    # 全新空库默认先安装 V11 基线，再自动升级到 V12。
    migrate("config_migration_fresh")
    assert_sql("SELECT version,dirty FROM config_migration_fresh.config_schema_migrations WHERE id=1", "12\t0")
    assert_sql("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='config_migration_fresh' AND table_name='game_order'", "1")

    # 旧版 SDK 若曾手工建过 game_order，必须人工核对，迁移不得冒然覆盖或标 dirty。
    migrate("config_migration_existing_order",target=11)
    mysql("CREATE TABLE config_migration_existing_order.game_order (id BIGINT PRIMARY KEY)")
    migrate("config_migration_existing_order",expected_success=False)
    assert_sql("SELECT version,dirty FROM config_migration_existing_order.config_schema_migrations WHERE id=1", "11\t0")

    migrate(target=11, expected_success=False)
    assert_sql("SELECT version,dirty FROM config_migration.config_schema_migrations WHERE id=1", "12\t0")
    migrate(target=10, expected_success=False)
    assert_sql("SELECT version,dirty FROM config_migration.config_schema_migrations WHERE id=1", "12\t0")

    # 已有 V10 迁移记录的库不能被当作空库安装 V11 基线。
    mysql("""CREATE TABLE config_migration_legacy.config_schema_migrations (
        id TINYINT PRIMARY KEY,version INT NOT NULL,dirty BOOLEAN NOT NULL,
        updated_at DATETIME(6) NOT NULL) ENGINE=InnoDB;
        INSERT INTO config_migration_legacy.config_schema_migrations VALUES
          (1,10,0,UTC_TIMESTAMP(6));""")
    migrate("config_migration_legacy", expected_success=False)
    assert_sql("SELECT version,dirty FROM config_migration_legacy.config_schema_migrations WHERE id=1", "10\t0")

    # 没有版本记录但已经有表的数据库，绝不能自动创建或覆盖。
    mysql("CREATE TABLE config_migration_unknown.config_node (id BIGINT PRIMARY KEY)")
    migrate("config_migration_unknown", expected_success=False)
    assert_sql("SELECT COUNT(*) FROM information_schema.tables "
               "WHERE table_schema='config_migration_unknown'", "1")

    # dirty 标记存在时拒绝继续执行，保证不重跑已经部分完成的 DDL。
    mysql("UPDATE config_migration.config_schema_migrations SET dirty=1 WHERE id=1")
    migrate(expected_success=False)
    assert_sql("SELECT version,dirty FROM config_migration.config_schema_migrations WHERE id=1", "12\t1")

    print("PASS V11 baseline -> V12 orders, GTID, same-schema constraints, idempotency and legacy safety", flush=True)


if __name__ == "__main__":
    main()
