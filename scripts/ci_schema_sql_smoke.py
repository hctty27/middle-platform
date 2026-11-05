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


def verify_sql_layout():
    for name in ("config_center.sql", "config_access.sql"):
        if not (ROOT / "db" / "full" / name).is_file():
            raise AssertionError(f"缺少全量 SQL: {name}")
    files = sorted((ROOT / "db" / "incremental").iterdir())
    if not files:
        raise AssertionError("增量 SQL 目录为空")
    for expected, path in enumerate(files, 1):
        if not path.is_file() or not path.name.startswith(f"V{expected:04d}__") or not path.name.endswith(".sql"):
            raise AssertionError(f"增量 SQL 编号不合法或不连续: {path.name}")


def main():
    verify_sql_layout()
    mysql("""CREATE DATABASE manual_fresh CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE DATABASE manual_existing CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE DATABASE manual_replica CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;""")
    mysql("""SET GLOBAL enforce_gtid_consistency=ON;
SET GLOBAL gtid_mode=OFF_PERMISSIVE;
SET GLOBAL gtid_mode=ON_PERMISSIVE;
SET GLOBAL gtid_mode=ON;""")
    assert_sql("SELECT @@GLOBAL.gtid_mode,@@GLOBAL.enforce_gtid_consistency", "ON\tON")

    init = source("db/full/config_center.sql")
    order = source("db/incremental/V0001__add_game_order.sql")
    replica = source("db/full/config_access.sql")

    mysql(database="manual_fresh", script=init)
    assert_sql("""SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema='manual_fresh' AND table_name IN
('config_revision','config_node','config_node_change_log','config_subscriber',
'config_subscriber_delivery_state','sdk_channel_app','sdk_node_config','game_order')""", "8")
    assert_sql("""SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema='manual_fresh' AND table_name='config_schema_migrations'""", "0")
    # 人工重复执行完整初始化必须拒绝，不允许静默覆盖旧数据。
    mysql(database="manual_fresh", script=init, ok=False)

    # 演练现有节点 JSON 数据复制与渠道包保留，旧字段清理由管理员另行执行。
    mysql("""INSERT INTO manual_fresh.config_node
      (id,node_type,market_type,code,name,status,config,created_at,updated_at)
      VALUES(801,'game','domestic','sdk_demo','SDK Demo',1,
      '{"gameSecret":"example-key","serviceAdapters":{"user_check":"standard"},"custom":"keep"}',
      UTC_TIMESTAMP(6),UTC_TIMESTAMP(6));""")
    mysql("RENAME TABLE manual_fresh.sdk_channel_app TO manual_fresh.config_channel_app")
    sdk_migration = source("db/incremental/V0009__migrate_sdk_node_config.sql")
    # 新库已有 sdk_node_config，只执行数据复制段和表重命名，完整 SQL 应在旧库执行。
    sdk_copy = sdk_migration[sdk_migration.index("INSERT INTO sdk_node_config("):]
    mysql(database="manual_fresh",script=sdk_copy)
    assert_sql("""SELECT JSON_UNQUOTE(JSON_EXTRACT(config,'$.gameSecret'))
      FROM manual_fresh.sdk_node_config WHERE node_id=801""", "example-key")
    assert_sql("""SELECT JSON_UNQUOTE(JSON_EXTRACT(config,'$.custom'))
      FROM manual_fresh.config_node WHERE id=801""", "keep")

    # 运营地区新字段采用手动 SQL 升级；验证旧游戏自动回填国内且重复执行被拒绝。
    mysql("CREATE DATABASE manual_market_upgrade CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;")
    mysql(database="manual_market_upgrade", script=init)
    mysql("""ALTER TABLE manual_market_upgrade.config_node DROP CHECK chk_config_node_market_type;
ALTER TABLE manual_market_upgrade.config_node DROP COLUMN market_type;
INSERT INTO manual_market_upgrade.config_node
(id,node_type,code,name,status,config,created_at,updated_at)
VALUES(901,'game','legacy','Legacy',1,'{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6));""")
    market_migration = source("db/incremental/V0011__add_game_market_type.sql")
    mysql(database="manual_market_upgrade", script=market_migration)
    assert_sql("SELECT market_type FROM manual_market_upgrade.config_node WHERE id=901", "domestic")
    mysql(database="manual_market_upgrade", script=market_migration, ok=False)

    # 模拟从历史 V11 权威库升级：保留旧版本表与真实业务节点，手动添加订单表。
    mysql(database="manual_existing", script=init)
    mysql("""DROP TABLE manual_existing.game_order;
CREATE TABLE manual_existing.config_schema_migrations(
id TINYINT PRIMARY KEY,version INT NOT NULL,dirty BOOLEAN NOT NULL,updated_at DATETIME(6) NOT NULL);
INSERT INTO manual_existing.config_schema_migrations VALUES(1,11,0,UTC_TIMESTAMP(6));
INSERT INTO manual_existing.config_node
(id,node_type,market_type,code,name,status,config,created_at,updated_at)
VALUES(101,'game','domestic','demo','Demo',1,'{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6));""")
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

    # 从当前通用绑定表升级到渠道专用表，保留事件上报绑定。
    mysql("CREATE DATABASE manual_service_current CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;")
    mysql(database="manual_service_current", script="""CREATE TABLE sdk_service (
      id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
      code VARCHAR(64) COLLATE utf8mb4_bin NOT NULL,
      name VARCHAR(100) NOT NULL,
      scope_type VARCHAR(32) COLLATE utf8mb4_bin NOT NULL,
      description VARCHAR(500) NOT NULL DEFAULT '',
      enabled TINYINT NOT NULL DEFAULT 1,
      created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
      updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
      UNIQUE KEY uk_sdk_service_code (code),
      CONSTRAINT chk_sdk_service_scope CHECK (scope_type IN ('channel_app','server_group'))
    );
    CREATE TABLE sdk_channel_app (id BIGINT PRIMARY KEY);
    INSERT INTO sdk_channel_app (id) VALUES (900);
    CREATE TABLE sdk_service_binding (
      id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
      service_id BIGINT NOT NULL,
      target_id BIGINT NOT NULL,
      env VARCHAR(16) COLLATE utf8mb4_bin NOT NULL,
      enabled TINYINT NOT NULL DEFAULT 0,
      config JSON NOT NULL,
      created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
      updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
      UNIQUE KEY uk_sdk_service_binding (service_id,target_id,env),
      KEY idx_sdk_binding_target (target_id,env),
      CONSTRAINT fk_sdk_binding_service FOREIGN KEY (service_id)
        REFERENCES sdk_service(id) ON DELETE RESTRICT,
      CONSTRAINT chk_sdk_binding_env CHECK (env IN ('prod','test','review')),
      CONSTRAINT chk_sdk_binding_enabled CHECK (enabled IN (0,1)),
      CONSTRAINT chk_sdk_binding_config CHECK (JSON_TYPE(config)='OBJECT')
    );
    INSERT INTO sdk_service (id,code,name,scope_type,enabled)
      VALUES (11,'event_report','事件上报','channel_app',1),
             (12,'role_query','角色查询','channel_app',1);
    INSERT INTO sdk_service_binding (id,service_id,target_id,env,enabled,config)
      VALUES (77,11,900,'test',1,'{"retained":true}'),
             (78,12,900,'test',1,'{}');
    """)

    migration = source("db/incremental/V0003__migrate_sdk_service_channel.sql")
    mysql(database="manual_service_current", script=migration)
    assert_sql("""SELECT id,service_id,channel_app_id,env,enabled,
JSON_UNQUOTE(JSON_EXTRACT(config,'$.retained'))
FROM manual_service_current.sdk_service_channel""", "77\t11\t900\ttest\t1\ttrue")
    assert_sql("SELECT code FROM manual_service_current.sdk_service", "event_report")
    assert_sql("""SELECT COUNT(*) FROM information_schema.columns
WHERE table_schema='manual_service_current' AND table_name='sdk_service'
  AND column_name='scope_type'""", "0")
    assert_sql("""SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema='manual_service_current' AND table_name='sdk_service_binding'""", "0")
    assert_sql("""SELECT COUNT(*) FROM information_schema.table_constraints
WHERE constraint_schema='manual_service_current'
  AND table_name='sdk_service_channel'
  AND constraint_name IN
    ('fk_sdk_channel_service','chk_sdk_channel_env',
     'chk_sdk_channel_enabled','chk_sdk_channel_config')""", "4")
    # 重复迁移与重复渠道绑定均应被拒绝。
    mysql(database="manual_service_current", script=migration, ok=False)
    mysql("""INSERT INTO manual_service_current.sdk_service_channel
(service_id,channel_app_id,env,enabled,config)
VALUES (11,900,'test',0,'{}');""", ok=False)

    mysql(database="manual_replica", script=replica)
    assert_sql("SELECT epoch,revision FROM manual_replica.config_sync_state WHERE id=1", "NULL\t0")
    mysql(database="manual_replica", script=replica, ok=False)
    print("通过：手动 SQL 初始化、升级、现有事件绑定保留、GTID 与数据库约束", flush=True)


if __name__ == "__main__":
    main()
