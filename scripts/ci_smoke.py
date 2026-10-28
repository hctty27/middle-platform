#!/usr/bin/env python3
"""End-to-end CI smoke test using real MySQL and two Spring Boot instances."""

import glob
import json
import os
import pathlib
import subprocess
import sys
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor

PROCESSES = []
LOGS = []


def request(port, path, method="GET", data=None, expected=200):
    payload = None if data is None else json.dumps(data).encode("utf-8")
    headers = {"Content-Type": "application/json"} if payload is not None else {}
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
    return json.loads(body) if body else None


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


def jar(module):
    matches = [
        item for item in glob.glob(f"{module}/build/libs/*.jar")
        if not item.endswith("-plain.jar")
    ]
    if len(matches) != 1:
        raise AssertionError(f"Expected one executable jar for {module}: {matches}")
    return matches[0]


def start(name, module, port, extra=None):
    env = os.environ.copy()
    env.update(extra or {})
    env["CONFIG_CENTER_PORT" if "center" in name else "CONFIG_CLIENT_PORT"] = str(port)
    path = f"/tmp/middle-platform-{name}.log"
    log = open(path, "w", encoding="utf-8")
    LOGS.append((name, path, log))
    java_home = env.get("JAVA_HOME")
    java_bin = str(pathlib.Path(java_home) / "bin" / "java") if java_home else "java"
    if java_home and not os.access(java_bin, os.X_OK):
        raise RuntimeError(f"JAVA_HOME points to a missing Java executable: {java_bin}")
    proc = subprocess.Popen(
        [java_bin, "-jar", jar(module)],
        env=env, stdout=log, stderr=subprocess.STDOUT
    )
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
    return request(8080, "/api/v1/admin/nodes", "POST", {
        "parentId": parent,
        "nodeType": node_type,
        "code": code,
        "name": f"{node_type}-{code}",
        "status": 1,
        "config": '{"enabled":true}',
        "remark": "CI smoke test"
    }, 201)


def subscriber():
    rows = request(8080, "/api/v1/admin/subscribers")
    return next(row for row in rows if row["code"] == "ci-demo")


def replica():
    return request(8081, "/demo/nodes")


def synchronized_nodes(count, revision):
    rows = replica()
    return len(rows) == count and subscriber()["acknowledgedRevision"] >= revision


def main():
    center1 = start("center-1", "config-center-server", 8080)
    wait_until("primary config-center ready", lambda: request(8080, "/actuator/health")["status"] == "UP")
    center2 = start("center-2", "config-center-server", 8082)
    wait_until("second config-center ready", lambda: request(8082, "/actuator/health")["status"] == "UP")

    demo = start("demo", "config-client-demo", 8081)
    wait_until("client demo ready", lambda: request(8081, "/demo/revision") is not None)
    request(8080, "/api/v1/admin/subscribers", "POST", {
        "code": "ci-demo",
        "callbackUrl": "http://127.0.0.1:8081/internal/config/callback",
        "secret": os.environ["CONFIG_WEBHOOK_SECRET"]
    }, 201)

    # Five-level hierarchy and versioned change log.
    parent = 0
    ids = []
    for kind in ("game", "region", "server_group", "channel", "sdk_app"):
        node = create_node(kind, parent, f"ci-{kind}")
        ids.append(node["id"])
        parent = node["id"]
    head = request(8080, "/api/v1/sync/revision")
    assert head["revision"] == 5, head
    history = request(8082, "/api/v1/sync/changes?afterRevision=0&limit=100")
    assert [event["revision"] for event in history] == list(range(1, 6)), history
    print("PASS five-level tree and cluster-wide ordered revisions", flush=True)
    # Navigation endpoints must return small summaries without any JSON configuration.
    roots = request(8080, "/api/v1/admin/nodes/tree?parentId=0")
    assert len(roots) == 1 and roots[0]["id"] == ids[0], roots
    assert "config" not in roots[0] and roots[0]["leaf"] is False, roots
    regions = request(8080, f"/api/v1/admin/nodes/tree?parentId={ids[0]}")
    assert len(regions) == 1 and regions[0]["id"] == ids[1], regions
    groups = request(8080, f"/api/v1/admin/nodes/tree?parentId={ids[1]}")
    assert len(groups) == 1 and groups[0]["id"] == ids[2] and groups[0]["leaf"], groups
    assert request(8080, f"/api/v1/admin/nodes/tree?parentId={ids[2]}") == []
    page = request(8080, f"/api/v1/admin/nodes/children?parentId={ids[2]}&page=1&size=1&keyword=ci-channel")
    assert page["total"] == 1 and len(page["items"]) == 1, page
    assert page["items"][0]["id"] == ids[3] and "config" not in page["items"][0], page
    assert request(8080, f"/api/v1/admin/nodes/children?parentId={ids[2]}&page=2&size=1")["items"] == []
    assert json.loads(request(8080, f"/api/v1/admin/nodes/{ids[3]}")["config"]) == {"enabled": True}
    request(8080, f"/api/v1/admin/nodes/children?parentId={ids[2]}&page=0", expected=400)
    print("PASS lightweight lazy tree and paginated child management APIs", flush=True)
    wait_until("Webhook pull copied all five nodes", lambda: synchronized_nodes(5, 5))

    # Write through a different config-center instance, verify client upsert.
    request(8082, f"/api/v1/admin/nodes/{ids[-1]}", "PUT", {
        "name": "updated-by-second-instance",
        "status": 0,
        "config": '{"enabled":false}',
        "remark": "Updated on node 2"
    })
    wait_until("second instance update propagated",
               lambda: any(row["id"] == ids[-1] and row["name"] == "updated-by-second-instance"
                           and row["status"] == 0 for row in replica()))
    before_bad = request(8080, "/api/v1/sync/revision")["revision"]
    request(8080, "/api/v1/admin/nodes", "POST", {
        "parentId": ids[0], "nodeType": "sdk_app",
        "code": "bad-parent", "name": "invalid",
        "status": 1, "config": "{}"
    }, 400)
    assert request(8080, "/api/v1/sync/revision")["revision"] == before_bad
    print("PASS invalid parent rejects write without version increment", flush=True)

    # Offline client must be retried and must recover without losing changes.
    stop(demo)
    request(8082, f"/api/v1/admin/nodes/{ids[0]}", "PUT", {
        "name": "changed-while-offline", "status": 1,
        "config": '{"enabled":true}', "remark": "offline"
    })
    latest = request(8080, "/api/v1/sync/revision")["revision"]
    demo = start("demo-restarted", "config-client-demo", 8081)
    wait_until("offline client catches up to authority",
               lambda: any(row["id"] == ids[0] and row["name"] == "changed-while-offline"
                           for row in replica()) and subscriber()["acknowledgedRevision"] >= latest,
               seconds=90)

    # Deletion must remove the local replica, not just disappear from batch-get.
    request(8082, f"/api/v1/admin/nodes/{ids[-1]}", "DELETE", expected=204)
    wait_until("delete propagated to local replica",
               lambda: len(replica()) == 4 and all(row["id"] != ids[-1] for row in replica()))

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
