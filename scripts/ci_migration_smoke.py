#!/usr/bin/env python3
"""Run a real V1 -> V2 -> V3 -> V4 upgrade with legacy rows against MySQL 8.4."""

import glob
import json
import os
import pathlib
import subprocess
import time
import urllib.request


PORT = 8093


def wait_ready(process, log_path, seconds=75):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise AssertionError("Migration server exited:\n" + pathlib.Path(log_path).read_text()[-7000:])
        try:
            with urllib.request.urlopen(f"http://127.0.0.1:{PORT}/actuator/health", timeout=2) as response:
                if json.load(response)["status"] == "UP":
                    return
        except Exception:
            pass
        time.sleep(0.5)
    raise AssertionError("Migration server did not become healthy:\n" + pathlib.Path(log_path).read_text()[-7000:])


def main():
    containers = subprocess.check_output(
        ["docker", "ps", "--filter", "ancestor=mysql:8.4", "--format", "{{.ID}}"],
        text=True).strip().splitlines()
    assert containers, "MySQL CI container is not running"
    container = containers[0]

    def mysql(sql):
        result = subprocess.run(
            ["docker", "exec", "-i", container, "mysql", "-uroot", "-pintegration-test",
             "-N", "-B"], input=sql, text=True, capture_output=True, check=True)
        return result.stdout.strip()

    mysql("CREATE DATABASE config_migration CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;")
    jars = [p for p in glob.glob("config-center-java/build/libs/*.jar") if not p.endswith("-plain.jar")]
    assert len(jars) == 1, jars
    env = os.environ.copy()
    env["CONFIG_DB_URL"] = "jdbc:mysql://127.0.0.1:3306/config_migration?connectionTimeZone=UTC"
    env["CONFIG_CENTER_PORT"] = str(PORT)
    java_bin = str(pathlib.Path(env["JAVA_HOME"]) / "bin" / "java") if env.get("JAVA_HOME") else "java"

    def boot(label, *args):
        log_path = f"/tmp/middle-platform-{label}.log"
        with open(log_path, "w", encoding="utf-8") as log:
            process = subprocess.Popen([java_bin, "-jar", jars[0], *args], env=env,
                                       stdout=log, stderr=subprocess.STDOUT)
            try:
                wait_ready(process, log_path)
            finally:
                process.terminate()
                try:
                    process.wait(timeout=12)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
        return log_path

    boot("legacy-v1-bootstrap", "--spring.flyway.target=1")
    assert mysql("SELECT version FROM config_migration.flyway_schema_history "
                 "WHERE success=1 ORDER BY installed_rank;").splitlines() == ["1"]

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
    boot("legacy-v3-upgrade", "--spring.flyway.target=3")
    versions = mysql("SELECT version FROM config_migration.flyway_schema_history "
                     "WHERE success=1 ORDER BY installed_rank;").splitlines()
    assert versions == ["1", "2", "3"], versions
    rows = mysql("""
        SELECT id,COALESCE(game_id,0),COALESCE(region_id,0),
               COALESCE(server_group_id,0),COALESCE(channel_id,0)
        FROM config_migration.config_node ORDER BY id;
    """).splitlines()
    expected = [
        "101\t0\t0\t0\t0",
        "102\t101\t0\t0\t0",
        "103\t101\t102\t0\t0",
        "104\t101\t102\t103\t0",
        "105\t101\t102\t103\t104",
    ]
    assert rows == expected, (rows, expected)
    assert mysql("SELECT COUNT(*) FROM information_schema.columns WHERE "
                 "table_schema='config_migration' AND table_name='config_node' "
                 "AND column_name='parent_id';") == "0"
    assert mysql("SELECT acknowledged_revision,COALESCE(acknowledged_epoch,'NULL') "
                 "FROM config_migration.config_delivery_state WHERE subscriber_id=77;") == "7\tNULL"
    print("PASS V1 legacy data -> V2 ancestry -> V3 epoch cursor", flush=True)

    # 原数据由 V4 自动归档。SDK 应用保留 id/code/config，直属父节点直接变为区服组。
    previous_epoch = mysql("SELECT epoch FROM config_migration.config_revision WHERE id=1;")
    boot("legacy-v4-upgrade")
    versions = mysql("SELECT version FROM config_migration.flyway_schema_history "
                     "WHERE success=1 ORDER BY installed_rank;").splitlines()
    assert versions == ["1", "2", "3", "4"], versions
    rows = mysql("""
        SELECT id,node_type,COALESCE(game_id,0),COALESCE(region_id,0),
               COALESCE(server_group_id,0)
        FROM config_migration.config_node ORDER BY id;
    """).splitlines()
    assert rows == [
        "101\tgame\t0\t0\t0",
        "102\tregion\t101\t0\t0",
        "103\tserver_group\t101\t102\t0",
        "105\tchannel_app\t101\t102\t103",
    ], rows
    assert mysql("SELECT COUNT(*) FROM information_schema.columns WHERE "
                 "table_schema='config_migration' AND table_name='config_node' "
                 "AND column_name='channel_id';") == "0"
    assert mysql("SELECT id,node_type,COALESCE(channel_id,0) FROM "
                 "config_migration.config_node_v4_legacy_archive ORDER BY id;").splitlines() == [
        "104\tchannel\t0",
        "105\tsdk_app\t104",
    ]
    assert mysql("SELECT epoch FROM config_migration.config_revision WHERE id=1;") != previous_epoch
    assert mysql("SELECT revision FROM config_migration.config_revision WHERE id=1;") == "7"
    print("PASS V4 four-level hierarchy, legacy archive and epoch change", flush=True)




if __name__ == "__main__":
    main()
