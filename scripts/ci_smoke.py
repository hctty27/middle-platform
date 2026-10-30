#!/usr/bin/env python3
"""Go 配置中心双实例、Webhook 与 Go 接入端 MySQL 端到端回归。"""

import hashlib
import hmac
import json
import os
import pathlib
import subprocess
import sys
import time
import urllib.error
import urllib.request

PROCESSES = []
LOGS = []


def request(port, path, method="GET", data=None, expected=200, extra_headers=None):
    payload = None if data is None else json.dumps(data).encode("utf-8")
    headers = {"Content-Type": "application/json"} if payload is not None else {}
    headers.update(extra_headers or {})
    req = urllib.request.Request(
        f"http://127.0.0.1:{port}{path}", data=payload, headers=headers, method=method
    )
    try:
        with urllib.request.urlopen(req, timeout=8) as response:
            status = response.status
            body = response.read().decode("utf-8")
    except urllib.error.HTTPError as ex:
        status = ex.code
        body = ex.read().decode("utf-8")
    if status != expected:
        raise AssertionError(f"{method} {path}: HTTP {status} != {expected}: {body[:500]}")
    if not body:
        return None
    try:
        return json.loads(body)
    except json.JSONDecodeError:
        return body


def wait_until(label, predicate, seconds=75):
    end = time.monotonic() + seconds
    last = None
    while time.monotonic() < end:
        try:
            result = predicate()
            if result:
                print(f"PASS {label}", flush=True)
                return result
        except Exception as ex:
            last = str(ex)
        time.sleep(0.8)
    raise AssertionError(f"Timed out: {label}. Last failure: {last}")


def start(name, module, port, extra=None):
    env = os.environ.copy()
    env.update(extra or {})
    if name.startswith("access"):
        env["CONFIG_ACCESS_ADDR"] = f":{port}"
    else:
        env["CONFIG_GO_ADDR"] = f":{port}"
        env["CONFIG_DB_DSN"] = "root:integration-test@tcp(127.0.0.1:3306)/config_center?parseTime=true&loc=UTC"
        env["CONFIG_GO_ENABLE_WRITES"] = "true"
        env["CONFIG_GO_ENABLE_WEBHOOK"] = "true"
        env["WEBHOOK_ALLOWED_HOSTS"] = "localhost,127.0.0.1"
    path = f"/tmp/middle-platform-{name}.log"
    log = open(path, "w", encoding="utf-8")
    LOGS.append((name, path, log))
    command = ["/tmp/config-center-access-ci"] if name.startswith("access") else ["/tmp/config-center-ci"]
    proc = subprocess.Popen(command, env=env, stdout=log, stderr=subprocess.STDOUT)
    PROCESSES.append(proc)
    print(f"Starting {name} (pid={proc.pid})", flush=True)
    return proc


def stop(proc):
    if proc.poll() is None:
        proc.terminate()
        try:
            proc.wait(timeout=12)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=5)


def create_node(node_type, parent, code):
    return request(8080, "/api/config/admin/nodes", "POST", {
        "parentId": parent,
        "nodeType": node_type,
        "envType": "test" if node_type == "region" else None,
        "code": code,
        "name": f"{node_type}-{code}",
        "status": 1,
        "config": '{"enabled":true}',
        "remark": "CI smoke test"
    }, 201)


def subscriber():
    rows = request(8080, "/api/config/admin/subscribers")
    return next(row for row in rows if row["code"] == "ci-access")


def replica():
    return request(8081, "/api/access/nodes")


def synchronized_nodes(count, revision):
    rows = replica()
    return len(rows) == count and subscriber()["acknowledgedRevision"] >= revision


def main():
    center1 = start("center-1", "config-center-java", 8080)
    wait_until("primary config-center ready", lambda: request(8080, "/health/ready")["status"] == "UP")
    center2 = start("center-2", "config-center-java", 8082)
    wait_until("second config-center ready", lambda: request(8082, "/health/ready")["status"] == "UP")

    access = start("access", "config-center-access", 8081)
    wait_until("client access ready", lambda: request(8081, "/api/access/revision") is not None)
    request(8080, "/api/config/admin/subscribers", "POST", {
        "code": "ci-access",
        "callbackUrl": "http://127.0.0.1:8081/internal/config/callback",
        "secret": os.environ["CONFIG_WEBHOOK_SECRET"]
    }, 201)

    # Webhook must return 202 after HMAC validation, without waiting for sync.
    initial = request(8080, "/api/config/sync/revision")
    timestamp = str(int(time.time()))
    message = f'{timestamp}.{initial["epoch"]}.{initial["revision"]}'
    signature = hmac.new(os.environ["CONFIG_WEBHOOK_SECRET"].encode("utf-8"),
                         message.encode("utf-8"), hashlib.sha256).hexdigest()
    notification = {"epoch": initial["epoch"], "revision": initial["revision"]}
    request(8081, "/internal/config/callback", "POST", notification, 401,
            extra_headers={"X-Config-Timestamp": timestamp,
                           "X-Config-Signature": "sha256=invalid"})
    request(8081, "/internal/config/callback", "POST", notification, 202,
            extra_headers={"X-Config-Timestamp": timestamp,
                           "X-Config-Signature": "sha256=" + signature})
    print("PASS invalid callback rejected and valid callback acknowledged asynchronously", flush=True)

    # 三层配置树 + 同一个游戏级渠道应用绑定到区服组。
    parent = 0
    ids = []
    for kind in ("game", "region", "server_group"):
        node = create_node(kind, parent, f"ci-{kind}")
        ids.append(node["id"])
        parent = node["id"]
    app = request(8080, "/api/config/admin/channel-apps", "POST", {
        "gameId": ids[0], "code": "ci-huawei", "name": "Huawei",
        "status": 1, "config": '{"appId":"10001"}', "remark": "shared"
    }, 201)
    binding = request(8082, f'/api/config/admin/channel-apps/{app["id"]}/bindings', "POST", {
        "serverGroupId": ids[2], "status": 1, "config": '{"authUrl":"auth-review"}'
    }, 201)
    assert json.loads(binding["effectiveConfig"]) == {"appId": "10001", "authUrl": "auth-review"}
    assert len(request(8080, f'/api/config/admin/channel-apps?gameId={ids[0]}')) == 1

    expected = [
        (None, None),
        (ids[0], None),
        (ids[0], ids[1]),
    ]
    for index,node_id in enumerate(ids):
        node = request(8080, f"/api/config/nodes/{node_id}")
        assert tuple(node[field] for field in ("gameId", "regionId")) == expected[index]
        assert "serverGroupId" not in node
        assert node["parentId"] == (ids[index - 1] if index else 0)
    head = request(8080, "/api/config/sync/revision")
    assert head["revision"] == 3, head
    history = request(8082, "/api/config/sync/changes?afterRevision=0&limit=100")
    assert [event["revision"] for event in history] == list(range(1,4)), history
    # 渠道应用、绑定 CRUD 不产生节点 revision，也不进入节点快照。
    snapshot = request(8080, "/api/config/sync/snapshot")
    assert set(snapshot) == {"epoch", "revision", "nodes"}, snapshot
    assert len(snapshot["nodes"]) == 3 and snapshot["revision"] == 3
    updated_app = request(8080, f'/api/config/admin/channel-apps/{app["id"]}', "PUT", {
        "name": "Huawei Updated", "status": 1, "config": '{"appId":"10001","enabled":true}', "remark": "shared"
    })
    assert updated_app["name"] == "Huawei Updated"
    updated_binding = request(8082, f'/api/config/admin/channel-app-bindings/{binding["id"]}', "PUT", {
        "status": 1, "config": '{"authUrl":"auth-production"}'
    })
    assert json.loads(updated_binding["effectiveConfig"])["authUrl"] == "auth-production"
    assert request(8080, "/api/config/sync/revision") == head
    print("PASS channel CRUD does not affect node sync", flush=True)
    # 旧版版本化路由应彻底失效（404），统一无版本路径仍正常工作。
    legacy_prefix = "/api/" + "v1"
    request(8080, legacy_prefix + "/sync/revision", expected=404)
    request(8082, legacy_prefix + "/sync/changes?afterRevision=0", expected=404)
    request(8080, legacy_prefix + "/admin/nodes", expected=404)
    request(8080, legacy_prefix + "/admin/nodes/tree?parentId=0", expected=404)
    request(8080, legacy_prefix + "/admin/subscribers", expected=404)
    request(8080, legacy_prefix + f"/config/nodes/{ids[0]}", expected=404)
    request(8080, legacy_prefix + "/config/nodes/batch-get", "POST", {"ids": ids}, 404)
    print("PASS unversioned API routes and removal of legacy aliases", flush=True)

    print("PASS three-level tree, shared channel app and cluster-wide revisions", flush=True)
    # Navigation endpoints must return small summaries without any JSON configuration.
    roots = request(8080, "/api/config/admin/nodes/tree?parentId=0")
    assert len(roots) == 1 and roots[0]["id"] == ids[0], roots
    assert "config" not in roots[0] and roots[0]["leaf"] is False, roots
    regions = request(8080, f"/api/config/admin/nodes/tree?parentId={ids[0]}")
    assert len(regions) == 1 and regions[0]["id"] == ids[1], regions
    assert regions[0]["envType"] == "test", regions
    filtered = request(8080, f"/api/config/admin/nodes/{ids[0]}/children?envType=test")
    assert filtered["total"] == 1 and filtered["items"][0]["envType"] == "test", filtered
    filtered = request(8080, f"/api/config/admin/nodes/{ids[0]}/children?envType=prod")
    assert filtered["total"] == 0, filtered
    groups = request(8080, f"/api/config/admin/nodes/tree?parentId={ids[1]}")
    assert len(groups) == 1 and groups[0]["id"] == ids[2] and groups[0]["leaf"], groups
    assert request(8080, f"/api/config/admin/nodes/tree?parentId={ids[2]}") == []
    page = request(8080, f"/api/config/admin/nodes/{ids[2]}/children?page=1&size=1")
    assert page["total"] == 0 and page["items"] == [], page
    request(8080, f"/api/config/admin/nodes/{ids[2]}/children?page=0", expected=400)
    print("PASS three-level lazy tree and paginated child management APIs", flush=True)
    # Global search must find unloaded descendants and return their complete ancestor path.
    matches = request(8080, "/api/config/admin/nodes/search?keyword=ci-server_group")
    assert len(matches) == 1 and matches[0]["node"]["id"] == ids[-1], matches
    assert [node["id"] for node in matches[0]["path"]] == ids, matches
    assert "config" not in matches[0]["node"], matches
    path = request(8082, f"/api/config/admin/nodes/{ids[2]}/path")
    assert [node["id"] for node in path["path"]] == ids, path
    assert request(8080, "/api/config/admin/nodes/search?keyword=") == []
    assert request(8080, "/api/config/admin/nodes/search?keyword=x&limit=51", expected=400)
    print("PASS global search, deep navigation paths and input validation", flush=True)

    wait_until("Webhook pull copied all three nodes", lambda: synchronized_nodes(3, 3))
    local = {row["id"]: row for row in replica()}
    for index, node_id in enumerate(ids):
        assert tuple(local[node_id][field] for field in
                     ("game_id", "region_id")) == expected[index], local
        assert "server_group_id" not in local[node_id], local
    assert local[ids[1]]["env_type"] == "test", local
    print("PASS complete ancestor paths and region environment in synchronized local replica", flush=True)

    # Write through a different config-center instance, verify client upsert.
    request(8082, f"/api/config/admin/nodes/{ids[-1]}", "PUT", {
        "name": "updated-by-second-instance",
        "status": 0,
        "config": '{"enabled":false}',
        "remark": "Updated on node 2"
    })
    wait_until("second instance update propagated",
               lambda: any(row["id"] == ids[-1] and row["name"] == "updated-by-second-instance"
                           and row["status"] == 0 for row in replica()))
    # 相同内容（包括 JSON 格式变化）不应产生 revision 或 updated_at 的变化。
    before_noop = request(8080, "/api/config/sync/revision")["revision"]
    unchanged = request(8082, f"/api/config/admin/nodes/{ids[-1]}", "PUT", {
        "name": "updated-by-second-instance", "status": 0,
        "config": '{ "enabled" : false }', "remark": "Updated on node 2"
    })
    assert unchanged["name"] == "updated-by-second-instance", unchanged
    assert request(8080, "/api/config/sync/revision")["revision"] == before_noop
    print("PASS semantic no-op update does not increment revision", flush=True)
    before_bad = request(8080, "/api/config/sync/revision")["revision"]
    request(8080, "/api/config/admin/nodes", "POST", {
        "parentId": ids[0], "nodeType": "channel_app",
        "code": "bad-parent", "name": "invalid",
        "status": 1, "config": "{}"
    }, 400)
    assert request(8080, "/api/config/sync/revision")["revision"] == before_bad
    print("PASS invalid parent rejects write without version increment", flush=True)
    for removed_type in ("channel", "sdk_app"):
        request(8080, "/api/config/admin/nodes", "POST", {
            "parentId": ids[2], "nodeType": removed_type,
            "code": "old-type", "name": "old", "status": 1, "config": "{}"
        }, 400)
    assert request(8080, "/api/config/sync/revision")["revision"] == before_bad
    print("PASS legacy node types rejected", flush=True)
    # 同一游戏下渠道应用编码唯一。
    request(8080, "/api/config/admin/channel-apps", "POST", {
        "gameId": ids[0], "code": "ci-huawei", "name": "Duplicated",
        "status": 1, "config": "{}"
    }, 409)
    assert request(8080, "/api/config/sync/revision")["revision"] == before_bad
    print("PASS sibling code uniqueness under derived parent scope", flush=True)

    # Offline client must be retried and must recover without losing changes.
    stop(access)
    request(8082, f"/api/config/admin/nodes/{ids[0]}", "PUT", {
        "name": "changed-while-offline", "status": 1,
        "config": '{"enabled":true}', "remark": "offline"
    })
    latest = request(8080, "/api/config/sync/revision")["revision"]
    access = start("access-restarted", "config-center-access", 8081)
    wait_until("offline client catches up to authority",
               lambda: any(row["id"] == ids[0] and row["name"] == "changed-while-offline"
                           for row in replica()) and subscriber()["acknowledgedRevision"] >= latest,
               seconds=90)


    # 数据集 epoch 单独变更，即使 revision 未变也必须重新通知接入方。
    container = subprocess.check_output(
        ["docker", "ps", "--filter", "ancestor=mysql:8.4", "--format", "{{.ID}}"],
        text=True).strip().splitlines()[0]

    def mysql(sql):
        return subprocess.check_output(
            ["docker", "exec", container, "mysql", "-uroot", "-pintegration-test",
             "-N", "-B", "-e", sql], text=True, stderr=subprocess.DEVNULL).strip()

    old_head = request(8080, "/api/config/sync/revision")
    mysql("UPDATE config_center.config_revision SET epoch=UUID() WHERE id=1")
    changed_head = request(8080, "/api/config/sync/revision")
    assert changed_head["epoch"] != old_head["epoch"], changed_head
    assert changed_head["revision"] == old_head["revision"], changed_head
    wait_until("epoch-only change triggers Webhook acknowledgement",
               lambda: mysql("SELECT acknowledged_epoch FROM config_center.config_subscriber_delivery_state "
                             "WHERE subscriber_id=(SELECT id FROM config_center.config_subscriber "
                             "WHERE code='ci-access')") == changed_head["epoch"], seconds=75)
    wait_until("client rebuilt snapshot for new epoch",
               lambda: request(8081, "/api/access/revision")["epoch"] == changed_head["epoch"], seconds=75)
    print("PASS epoch-only change triggers notification and full snapshot", flush=True)

    # 删除时必须先解除绑定，避免区服组和应用出现孤儿引用。
    request(8082, f'/api/config/admin/channel-apps/{app["id"]}', "DELETE", expected=409)
    request(8082, f'/api/config/admin/channel-app-bindings/{binding["id"]}', "DELETE", expected=204)
    request(8082, f'/api/config/admin/channel-apps/{app["id"]}', "DELETE", expected=204)
    assert request(8080, "/api/config/sync/revision") == changed_head
    request(8082, f"/api/config/admin/nodes/{ids[-1]}", "DELETE", expected=204)
    wait_until("delete propagated to local replica",
               lambda: len(replica()) == 2 and all(row["id"] != ids[-1] for row in replica()))

    print("ALL END-TO-END SMOKE TESTS PASSED", flush=True)


if __name__ == "__main__":
    try:
        main()
    except Exception:
        import traceback
        traceback.print_exc()
        for name, path, log in LOGS:
            log.flush()
            if pathlib.Path(path).exists():
                print(f"--- {name} last 100 log lines ---", flush=True)
                print("\n".join(pathlib.Path(path).read_text(errors="replace").splitlines()[-100:]))
        sys.exit(1)
    finally:
        for proc in reversed(PROCESSES):
            stop(proc)
        for _, _, log in LOGS:
            log.close()
