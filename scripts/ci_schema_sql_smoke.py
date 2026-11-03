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
        raise AssertionError(f"MySQL expected success={ok}, exit={result.returncode}: {result.stderr}")
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

    mysql(database="manual_replica", script=replica)
    assert_sql("SELECT epoch,revision FROM manual_replica.config_sync_state WHERE id=1", "NULL\t0")
    mysql(database="manual_replica", script=replica, ok=False)
    print("PASS manual authoritative/replica SQL, upgrade-only DDL, legacy data, GTID and constraints", flush=True)


if __name__ == "__main__":
    main()
