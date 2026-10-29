#!/usr/bin/env python3
"""Compare Java and Go API contracts against the same Flyway V1->V4 MySQL fixture."""
import glob
import json
import os
import pathlib
import subprocess
import time
import urllib.error
import urllib.request

JAVA_PORT = 8080
GO_PORT = 8081
PROCESSES = []
LOGS = []

def request(port, path, method="GET", payload=None, expected=200):
    data = json.dumps(payload).encode() if payload is not None else None
    headers = {"Content-Type": "application/json"} if data is not None else {}
    req = urllib.request.Request(f"http://127.0.0.1:{port}{path}", data=data,
                                 headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=12) as resp:
            status, body = resp.status, resp.read()
    except urllib.error.HTTPError as exc:
        status, body = exc.code, exc.read()
    if status != expected:
        raise AssertionError(f"{port} {method} {path}: HTTP {status}, expected {expected}: {body[:600]!r}")
    if not body:
        return None
    try:
        return json.loads(body)
    except json.JSONDecodeError:
        return body.decode("utf-8", errors="replace")

def wait_ready(port, path, proc, label):
    deadline = time.monotonic() + 85
    while time.monotonic() < deadline:
        if proc.poll() is not None:
            raise AssertionError(f"{label} exited; log:\n{pathlib.Path(LOGS[-1]).read_text()[-7000:]}")
        try:
            if request(port, path).get("status") == "UP":
                print(f"PASS {label} ready", flush=True)
                return
        except Exception:
            pass
        time.sleep(0.5)
    raise AssertionError(f"{label} not ready; log: {pathlib.Path(LOGS[-1]).read_text()[-7000:]}")

def start_java():
    jars = [p for p in glob.glob("config-center/build/libs/*.jar")
            if not p.endswith("-plain.jar")]
    assert len(jars) == 1, jars
    env = os.environ.copy()
    env.update(CONFIG_DB_URL="jdbc:mysql://127.0.0.1:3306/config_migration?connectionTimeZone=UTC",
               CONFIG_DB_USER="root", CONFIG_DB_PASSWORD="integration-test",
               CONFIG_CENTER_PORT=str(JAVA_PORT),
               WEBHOOK_ALLOWED_HOSTS="localhost,127.0.0.1")
    cmd = [str(pathlib.Path(env["JAVA_HOME"]) / "bin" / "java"), "-jar", jars[0]]
    return start("java", cmd, env)

def start_go():
    env = os.environ.copy()
    env.update(CONFIG_DB_DSN="root:integration-test@tcp(127.0.0.1:3306)/config_migration?parseTime=true&loc=UTC",
               CONFIG_GO_ADDR=f":{GO_PORT}", CONFIG_GO_ENABLE_WRITES="true",
               CONFIG_GO_ENABLE_WEBHOOK="false",
               WEBHOOK_ALLOWED_HOSTS="localhost,127.0.0.1")
    return start("go", ["/tmp/config-center-go-ci"], env)

def start(name, command, env):
    path = f"/tmp/config-center-compat-{name}.log"
    log = open(path, "w", encoding="utf-8")
    proc = subprocess.Popen(command, stdout=log, stderr=subprocess.STDOUT, env=env)
    PROCESSES.append((proc, log))
    LOGS.append(path)
    return proc

def check(method, path, payload=None, expected=200):
    java = request(JAVA_PORT, path, method, payload, expected)
    go = request(GO_PORT, path, method, payload, expected)
    if java != go:
        raise AssertionError(
            f"Java/Go mismatch for {method} {path}\n"
            f"Java = {json.dumps(java, sort_keys=True, ensure_ascii=False)[:4000]}\n"
            f"Go   = {json.dumps(go, sort_keys=True, ensure_ascii=False)[:4000]}"
        )
    return go

def run():
    jp = start_java()
    wait_ready(JAVA_PORT, "/actuator/health", jp, "Java Flyway V4")
    gp = start_go()
    wait_ready(GO_PORT, "/health/ready", gp, "Go")

    # V1 historical rows were inserted and migrated to V4 by ci_migration_smoke.py.
    root, region, group, app = 101, 102, 103, 105
    paths = [
        "/api/config/admin/nodes",
        "/api/config/admin/nodes/tree?parentId=0",
        "/api/config/admin/nodes/tree?parentId=101",
        "/api/config/admin/nodes/tree?parentId=102",
        "/api/config/admin/nodes/tree?parentId=103",
        "/api/config/admin/nodes/103/children?page=1&size=20",
        "/api/config/admin/nodes/103/children?page=2&size=1",
        "/api/config/admin/nodes/children?parentId=0&page=1&size=20",
        "/api/config/admin/nodes/search?keyword=legacy-sdk",
        "/api/config/admin/nodes/search?keyword=legacy",
        "/api/config/admin/nodes/search?keyword=",
        "/api/config/admin/nodes/105/path",
        "/api/config/admin/nodes/101",
        "/api/config/nodes/105",
        "/api/config/sync/revision",
        "/api/config/sync/changes?afterRevision=0&limit=100",
        "/api/config/sync/snapshot",
        "/api/config/admin/subscribers",
    ]
    for path in paths:
        check("GET", path)
    check("POST", "/api/config/nodes/batch-get", {"ids": [app, group, root, app]})
    check("POST", "/api/config/nodes/batch-get", {"ids": []})
    print(f"PASS {len(paths) + 2} real-migration Java/Go read contracts", flush=True)

    payloads = (
        ("game", 0, "compat-game"),
        ("region", None, "compat-region"),
        ("server_group", None, "compat-group"),
        ("channel_app", None, "compat-app"),
    )
    current = 0
    ids = []
    for kind, _, code in payloads:
        payload = {"parentId": current, "nodeType": kind, "code": code,
                   "name": f"{kind} 兼容", "status": 1,
                   "config": '{"ready":true}', "remark": "compatibility"}
        # Alternate Java and Go as the writer; validate both readers after every write.
        port = JAVA_PORT if len(ids) % 2 == 0 else GO_PORT
        created = request(port, "/api/config/admin/nodes", "POST", payload, 201)
        current = created["id"]
        ids.append(current)
        check("GET", f"/api/config/admin/nodes/{current}")
        check("GET", "/api/config/sync/revision")
    check("GET", f"/api/config/admin/nodes/{ids[-1]}/path")
    check("GET", f"/api/config/admin/nodes/{ids[2]}/children?page=1&size=20")
    check("GET", "/api/config/admin/nodes/search?keyword=compat")
    check("GET", "/api/config/sync/snapshot")
    check("GET", "/api/config/sync/changes?afterRevision=7&limit=100")

    # JSON semantic no-op should leave revision and timestamp unchanged.
    before = request(JAVA_PORT, "/api/config/sync/revision")["revision"]
    noop = {"name": "channel_app 兼容", "status": 1, "config": '{ "ready" : true }',
            "remark": "compatibility"}
    go_update = request(GO_PORT, f"/api/config/admin/nodes/{ids[-1]}", "PUT", noop)
    after = request(JAVA_PORT, "/api/config/sync/revision")["revision"]
    assert before == after and go_update["id"] == ids[-1], (before, after, go_update)
    check("GET", f"/api/config/admin/nodes/{ids[-1]}")

    changed = {"name": "已更新", "status": 0, "config": '{"ready":false}',
               "remark": None}
    request(GO_PORT, f"/api/config/admin/nodes/{ids[-1]}", "PUT", changed)
    check("GET", f"/api/config/admin/nodes/{ids[-1]}")
    request(GO_PORT, f"/api/config/admin/nodes/{ids[-1]}", "DELETE", expected=204)
    check("GET", "/api/config/sync/changes?afterRevision=7&limit=100")
    check("GET", "/api/config/sync/snapshot")
    for port in (JAVA_PORT, GO_PORT):
        request(port, f"/api/config/admin/nodes/{ids[-1]}", expected=404)
    check("GET", "/api/config/sync/revision")
    print("PASS mixed Java/Go writes, no-op revision, update and deletion", flush=True)

    # Server-side errors only need stable HTTP statuses, not identical exception bodies.
    for p in (JAVA_PORT, GO_PORT):
        request(p, "/api/config/admin/nodes/103/children?page=0", expected=400)
        request(p, "/api/config/admin/nodes/search?keyword=a&limit=51", expected=400)
        request(p, "/api/" + "v1/config/sync/revision", expected=404)
    print("PASS status and legacy route checks", flush=True)

if __name__ == "__main__":
    try:
        run()
    finally:
        for proc, log in reversed(PROCESSES):
            if proc.poll() is None:
                proc.terminate()
                try:
                    proc.wait(timeout=12)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait(timeout=5)
            log.close()
