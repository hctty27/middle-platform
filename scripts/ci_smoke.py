#!/usr/bin/env python3
"""Go 配置中心双实例、Webhook 与 Go 接入端 MySQL 端到端回归。"""

import hashlib
import json
import os
import pathlib
import secrets
import urllib.parse
import subprocess
import sys
import time
import urllib.error
import urllib.request

PROCESSES = []
LOGS = []


def request(port, path, method="GET", data=None, expected=200, extra_headers=None):
    """发送 HTTP 请求并校验响应状态与 JSON 数据。"""
    payload = None if data is None else json.dumps(data).encode("utf-8")
    headers = {"Content-Type": "application/json"} if payload is not None else {}
    headers.update(extra_headers or {})
    if path == "/api/config/admin" or path.startswith("/api/config/admin/"):
        secret = os.environ["ADMIN_AUTH_SECRET"]
        stamp = str(int(time.time()))
        nonce = secrets.token_hex(8)
        parsed = urllib.parse.urlsplit(path)
        query = urllib.parse.urlencode(sorted(
            urllib.parse.parse_qsl(parsed.query, keep_blank_values=True)
        ))
        values = [
            secret, "method:" + method.upper(),
            "path:" + parsed.path, "query:" + query,
            "body:" + hashlib.sha256(payload or b"").hexdigest(),
            "timestamp:" + stamp, "nonce:" + nonce,
        ]
        signature = hashlib.sha1("_".join(sorted(values)).encode("utf-8")).hexdigest()
        headers.update({"X-Timestamp": stamp, "X-Nonce": nonce, "X-Signature": signature})
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
    """轮询异步条件，超时后报告最后一次失败原因。"""
    end = time.monotonic() + seconds
    last = None
    while time.monotonic() < end:
        try:
            result = predicate()
            if result:
                print(f"通过：{label}", flush=True)
                return result
        except Exception as ex:
            last = str(ex)
        time.sleep(0.8)
    raise AssertionError(f"等待超时：{label}。最后一次错误：{last}")


def start(name, module, port, extra=None):
    """启动测试用 Go 服务进程，设置对应环境变量并保存日志。"""
    env = os.environ.copy()
    env.update(extra or {})
    if name.startswith("access"):
        env["CONFIG_ACCESS_ADDR"] = f":{port}"
    else:
        env["CONFIG_GO_ADDR"] = f":{port}"
        env["CONFIG_DB_DSN"] = "root:integration-test@tcp(127.0.0.1:3306)/config_center?parseTime=true&loc=UTC"
        env["CONFIG_GO_ENABLE_WRITES"] = "true"
        env["CONFIG_GO_ENABLE_WEBHOOK"] = "true"
    path = f"/tmp/middle-platform-{name}.log"
    log = open(path, "w", encoding="utf-8")
    LOGS.append((name, path, log))
    is_access = name.startswith("access")
    command = ["/tmp/config-center-access-ci"] if is_access else ["/tmp/config-center-ci"]
    service = "config-center-access" if is_access else "config-center"
    config_path = pathlib.Path(__file__).resolve().parent.parent / service / "configs" / "config.yaml"
    command.extend(["-config", str(config_path)])
    proc = subprocess.Popen(command, env=env, stdout=log, stderr=subprocess.STDOUT)
    PROCESSES.append(proc)
    print(f"正在启动 {name}（进程号={proc.pid}）", flush=True)
    return proc


def stop(proc):
    """停止测试进程并等待退出。"""
    if proc.poll() is None:
        proc.terminate()
        try:
            proc.wait(timeout=12)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=5)


def create_node(node_type, parent, code):
    """通过管理接口创建节点并返回 ID。"""
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
    """读取当前订阅者同步状态。"""
    rows = request(8080, "/api/config/admin/subscribers")
    return next(row for row in rows if row["code"] == "ci-access")


def replica():
    """读取接入端维护的本地节点副本。"""
    return request(8081, "/api/access/nodes")


def synchronized_nodes(count, revision):
    """核对同步版本与节点数量。"""
    rows = replica()
    return len(rows) == count and subscriber()["acknowledgedRevision"] >= revision


def main():
    """覆盖双实例写入、回调签名、增量同步和故障恢复。"""
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

    # Webhook 使用共享 SHA-1 排序签名；同一个 nonce 不允许重放。
    initial = request(8080, "/api/config/sync/revision")
    timestamp = str(int(time.time()))
    nonce = secrets.token_hex(8)
    values = [os.environ["CONFIG_WEBHOOK_SECRET"], timestamp, nonce,
              initial["epoch"], str(initial["revision"])]
    signed = hashlib.sha1("_".join(sorted(values)).encode("utf-8")).hexdigest()
    notification = {"epoch": initial["epoch"], "revision": initial["revision"]}
    request(8081, "/internal/config/callback", "POST", notification, 401,
            extra_headers={"X-Timestamp": timestamp, "X-Nonce": nonce,
                           "X-Signature": "invalid"})
    request(8081, "/internal/config/callback", "POST", notification, 202,
            extra_headers={"X-Timestamp": timestamp, "X-Nonce": nonce,
                           "X-Signature": signed})
    request(8081, "/internal/config/callback", "POST", notification, 409,
            extra_headers={"X-Timestamp": timestamp, "X-Nonce": nonce,
                           "X-Signature": signed})
    print("通过：拒绝无效签名与 nonce 重放，并确认有效回调", flush=True)

    # 三层配置树与跨实例的同步。
    parent = 0
    ids = []
    for kind in ("game", "region", "server_group"):
        node = create_node(kind, parent, f"ci-{kind}")
        ids.append(node["id"])
        parent = node["id"]
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
    # 基础节点快照不包含 SDK 配置数据。
    snapshot = request(8080, "/api/config/sync/snapshot")
    assert set(snapshot) == {"epoch", "revision", "nodes"}, snapshot
    assert len(snapshot["nodes"]) == 3 and snapshot["revision"] == 3
    # 旧版版本化路由应彻底失效（404），统一无版本路径仍正常工作。
    legacy_prefix = "/api/" + "v1"
    request(8080, legacy_prefix + "/sync/revision", expected=404)
    request(8082, legacy_prefix + "/sync/changes?afterRevision=0", expected=404)
    request(8080, legacy_prefix + "/admin/nodes", expected=404)
    request(8080, legacy_prefix + "/admin/nodes/tree?parentId=0", expected=404)
    request(8080, legacy_prefix + "/admin/subscribers", expected=404)
    request(8080, legacy_prefix + f"/config/nodes/{ids[0]}", expected=404)
    request(8080, legacy_prefix + "/config/nodes/batch-get", "POST", {"ids": ids}, 404)
    print("通过：无版本 API 路由及旧别名清理", flush=True)

    print("通过：三级目录与集群版本", flush=True)
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
    print("通过：三级懒加载目录及子项分页接口", flush=True)
    # Global search must find unloaded descendants and return their complete ancestor path.
    matches = request(8080, "/api/config/admin/nodes/search?keyword=ci-server_group")
    assert len(matches) == 1 and matches[0]["node"]["id"] == ids[-1], matches
    assert [node["id"] for node in matches[0]["path"]] == ids, matches
    assert "config" not in matches[0]["node"], matches
    path = request(8082, f"/api/config/admin/nodes/{ids[2]}/path")
    assert [node["id"] for node in path["path"]] == ids, path
    assert request(8080, "/api/config/admin/nodes/search?keyword=") == []
    assert request(8080, "/api/config/admin/nodes/search?keyword=x&limit=51", expected=400)
    print("通过：全局搜索、节点路径及输入校验", flush=True)

    wait_until("Webhook pull copied all three nodes", lambda: synchronized_nodes(3, 3))
    local = {row["id"]: row for row in replica()}
    for index, node_id in enumerate(ids):
        assert tuple(local[node_id][field] for field in
                     ("game_id", "region_id")) == expected[index], local
        assert "server_group_id" not in local[node_id], local
    assert local[ids[1]]["env_type"] == "test", local
    print("通过：同步副本的完整父节点路径与大区环境", flush=True)

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
    print("通过：语义无变化的更新不增加版本号", flush=True)
    before_bad = request(8080, "/api/config/sync/revision")["revision"]
    request(8080, "/api/config/admin/nodes", "POST", {
        "parentId": ids[0], "nodeType": "channel_app",
        "code": "bad-parent", "name": "invalid",
        "status": 1, "config": "{}"
    }, 400)
    assert request(8080, "/api/config/sync/revision")["revision"] == before_bad
    print("通过：非法上级节点被拒绝且不增加版本号", flush=True)
    for removed_type in ("channel", "sdk_app"):
        request(8080, "/api/config/admin/nodes", "POST", {
            "parentId": ids[2], "nodeType": removed_type,
            "code": "old-type", "name": "old", "status": 1, "config": "{}"
        }, 400)
    assert request(8080, "/api/config/sync/revision")["revision"] == before_bad
    print("通过：拒绝旧版节点类型", flush=True)
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
    def mysql(sql):
        """直接查询本机 MySQL，确认仅改变 epoch 也能触发接入方重建快照。"""
        env = os.environ.copy()
        env["MYSQL_PWD"] = "integration-test"
        return subprocess.check_output(
            ["mysql", "--protocol=tcp", "--host=127.0.0.1",
             "--user=root", "--batch", "--skip-column-names", "-e", sql],
            text=True, stderr=subprocess.DEVNULL, env=env,
        ).strip()

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
    print("通过：仅 epoch 变更触发通知与全量快照", flush=True)

    request(8082, f"/api/config/admin/nodes/{ids[-1]}", "DELETE", expected=204)
    wait_until("delete propagated to local replica",
               lambda: len(replica()) == 2 and all(row["id"] != ids[-1] for row in replica()))

    print("所有端到端冒烟测试通过", flush=True)


if __name__ == "__main__":
    try:
        main()
    except Exception:
        import traceback
        traceback.print_exc()
        for name, path, log in LOGS:
            log.flush()
            if pathlib.Path(path).exists():
                print(f"--- {name} 最近 100 行日志 ---", flush=True)
                print("\n".join(pathlib.Path(path).read_text(errors="replace").splitlines()[-100:]))
        sys.exit(1)
    finally:
        for proc in reversed(PROCESSES):
            stop(proc)
        for _, _, log in LOGS:
            log.close()
