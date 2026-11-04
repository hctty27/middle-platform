#!/usr/bin/env python3
"""真实 MySQL 验证手动 SQL：新库初始化、已有库补表、数据保留和约束。"""

import os
import pathlib
import subprocess

MYSQL = ["mysql", "--protocol=tcp", "--host=127.0.0.1", "--user=root", "--batch", "--skip-column-names"]
ROOT = pathlib.Path(__file__).resolve().parents[1]


def mysql(statement="", database=None, script=None, ok=True):
    env = os.environ.copy()
    env["MYSQL_PWD"] = "integration-test"
    cmd = [*MYSQL]
    if database:
        cmd.append(database)
    result = subprocess.run(cmd, input=script if script is not None else statement,
                            text=True, capture_output=True, env=env)
    if (result.returncode == 0) != ok:
        raise AssertionError(f"MySQL 执行结果不符合预期，预期成功={ok}，退出码={result.returncode}: {result.stderr}")
    return result.stdout.strip()


def source(path):
    return (ROOT / path).read_text(encoding="utf-8")


def assert_sql(query, expected):
    actual = mysql(query)
    if actual != expected:
        raise AssertionError(f"{query}: {actual!r} != {expected!r}")


def main():
    mysql("""CREATE DATABASE manual_fresh CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE DATABASE manual_existing CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE DATABASE manual_replica CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;""")
    mysql("""SET GLOBAL enforce_gtid_consistency=ON;
SET GLOBAL gtid_mode=OFF_PERMISSIVE;
SET GLOBAL gtid_mode=ON_PERMISSIVE;
SET GLOBAL gtid_mode=ON;""")
    assert_sql("SELECT @@GLOBAL.gtid_mode,@@GLOBAL.enforce_gtid_consistency", "ON\tON")

    init = source("config-center/db/sql/initialize.sql")
    order = source("config-center/db/sql/add_game_order.sql")
    replica = source("config-center-access/db/sql/initialize.sql")

    mysql(database="manual_fresh", script=init)
    assert_sql("""SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema='manual_fresh' AND table_name IN
('config_revision','config_node','config_node_change_log','config_subscriber',
'config_subscriber_delivery_state','config_channel_app','game_order')""", "7")
    assert_sql("""SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema='manual_fresh' AND table_name='config_schema_migrations'""", "0")
    # 人工重复执行完整初始化必须拒绝，不允许静默覆盖旧数据。
    mysql(database="manual_fresh", script=init, ok=False)

    # 模拟从历史 V11 权威库升级：保留旧版本表与真实业务节点，手动添加订单表。
    mysql(database="manual_existing", script=init)
    mysql("""DROP TABLE manual_existing.game_order;
CREATE TABLE manual_existing.config_schema_migrations(
id TINYINT PRIMARY KEY,version INT NOT NULL,dirty BOOLEAN NOT NULL,updated_at DATETIME(6) NOT NULL);
INSERT INTO manual_existing.config_schema_migrations VALUES(1,11,0,UTC_TIMESTAMP(6));
INSERT INTO manual_existing.config_node
(id,node_type,code,name,status,config,created_at,updated_at)
VALUES(101,'game','demo','Demo',1,'{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6));""")
    mysql(database="manual_existing", script=order)
    assert_sql("SELECT COUNT(*) FROM manual_existing.config_node WHERE id=101", "1")
    assert_sql("SELECT version,dirty FROM manual_existing.config_schema_migrations WHERE id=1", "11\t0")
    assert_sql("SELECT COUNT(*) FROM manual_existing.game_order", "0")
    mysql(database="manual_existing", script=order, ok=False)
    mysql("""INSERT INTO manual_existing.game_order
(appid,game_id,env,game_order_id,amount,attach,game_notify_url)
VALUES ('app1',101,'test','g1',600,'','https://game.example.com/notify');""")
    mysql("""INSERT INTO manual_existing.game_order
(appid,game_id,env,game_order_id,amount,attach,game_notify_url)
VALUES ('app1',101,'test','g1',600,'','https://game.example.com/notify');""", ok=False)

    # 模拟已运行旧版 sdk_service/sdK_service_channel 的数据库，验证无损迁移。
    mysql("CREATE DATABASE manual_service_legacy CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;")
    mysql(database="manual_service_legacy", script="""CREATE TABLE sdk_service (
      id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
      code VARCHAR(64) COLLATE utf8mb4_bin NOT NULL UNIQUE,
      name VARCHAR(100) NOT NULL,description VARCHAR(500) NOT NULL DEFAULT '',
      enabled TINYINT NOT NULL DEFAULT 1,sort_order INT NOT NULL DEFAULT 0,
      created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
      updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
    );
    CREATE TABLE sdk_service_channel (
      id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
      channel_app_id BIGINT NOT NULL,service_id BIGINT NOT NULL,
      env VARCHAR(16) COLLATE utf8mb4_bin NOT NULL,enabled TINYINT NOT NULL DEFAULT 0,
      config JSON NOT NULL,
      created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
      updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
      UNIQUE KEY uk_sdk_service_channel(channel_app_id,service_id,env),
      FOREIGN KEY(service_id) REFERENCES sdk_service(id)
    );
    INSERT INTO sdk_service(id,code,name,enabled,sort_order)
      VALUES(11,'event_report','事件上报',1,10);
    INSERT INTO sdk_service_channel(id,channel_app_id,service_id,env,enabled,config)
      VALUES(77,900,11,'test',1,'{"retained":true}');
    """)
    migration = source("config-center/db/sql/migrate_sdk_service_binding.sql")
    mysql(database="manual_service_legacy", script=migration)
    assert_sql("""SELECT id,service_id,target_id,env,enabled,JSON_UNQUOTE(JSON_EXTRACT(config,'$.retained'))
FROM manual_service_legacy.sdk_service_binding""", "77\t11\t900\ttest\t1\ttrue")
    assert_sql("""SELECT scope_type FROM manual_service_legacy.sdk_service WHERE id=11""", "channel_app")
    assert_sql("""SELECT scope_type,enabled FROM manual_service_legacy.sdk_service
WHERE code='server_status'""", "server_group\t0")
    assert_sql("""SELECT COUNT(*) FROM manual_service_legacy.sdk_service_channel""", "1")
    mysql(database="manual_service_legacy", script=migration, ok=False)
    mysql("""INSERT INTO manual_service_legacy.sdk_service_binding
(service_id,target_id,env,enabled,config) VALUES(11,900,'test',0,'{}');""", ok=False)

    mysql(database="manual_replica", script=replica)
    assert_sql("SELECT epoch,revision FROM manual_replica.config_sync_state WHERE id=1", "NULL\t0")
    mysql(database="manual_replica", script=replica, ok=False)
    print("通过：手动 SQL 初始化、升级、历史数据保留、GTID 与数据库约束", flush=True)


if __name__ == "__main__":
    main()
