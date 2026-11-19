"""Runner-only 1:1:2 business mix and critical-interface isolation checks."""
import json
import os
import re
from pathlib import Path
import subprocess
import sys
import time
import urllib.request

from process_monitor import print_process_report

if os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("RUNNER_ENVIRONMENT") != "github-hosted":
    raise SystemExit("Disposable GitHub-hosted runner required")
root = Path(sys.argv[1])
results = root / "results"
mock = "http://127.0.0.1:8290"
rows = []
valid = True


def mode(delay, fail):
    request = urllib.request.Request(f"{mock}/load/event-mode?delay_ms={delay}&mode={'fail' if fail else 'ok'}", method="POST")
    with urllib.request.urlopen(request, timeout=5) as response:
        if response.status != 204:
            raise RuntimeError("mock control failed")


def stats():
    with urllib.request.urlopen(mock + "/load/stats", timeout=5) as response:
        return json.load(response)


def mysql():
    query = "SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_running','Threads_connected','Com_select','Com_stmt_execute','Innodb_row_lock_waits','Innodb_row_lock_time','Innodb_log_waits')"
    data = subprocess.check_output(["mysql", "-uroot", "-h127.0.0.1", "-N", "-B", "-e", query], text=True)
    return {key: int(val) for key, val in (line.split("\t") for line in data.splitlines())}


def metric(data, name, field, scenario):
    key = name + "{scenario:" + scenario + "}"
    item = data.get("metrics", {}).get(key, {})
    return item.get("values", item).get(field)


def fmt(v, digits=2):
    return "—" if v is None else f"{v:.{digits}f}"


def service_config(ttl, isolated, idle, event_cap, access_enabled):
    """Only edit the disposable runner's service YAML, then restart its instances."""
    config = root / "config" / "sdk-service.yaml"
    content = config.read_text()
    for key, value in {
        "ttl": ttl, "isolateConnections": str(isolated).lower(),
        "maxIdlePerHost": idle, "eventMaxConns": event_cap, "accessEnabled": str(access_enabled).lower(),
    }.items():
        content, count = re.subn(r"(?m)^(  " + key + r":)[ \t]*[^#\r\n]+$", lambda m: m.group(1) + " " + str(value), content)
        if count != 1:
            raise RuntimeError("missing or duplicate fixture setting " + key)
    config.write_text(content)
    ports = [int(p) for p in (root / "ports.txt").read_text().splitlines() if 8221 <= int(p) <= 8224]
    if not ports:
        raise RuntimeError("no SDK Service fixture ports")
    names = [f"sdk-service-{port - 8220}" for port in ports]
    subprocess.run(["supervisorctl", "-c", str(root / "supervisor.conf"), "restart", *names], check=True)
    for port in ports:
        for attempt in range(30):
            try:
                with urllib.request.urlopen(f"http://127.0.0.1:{port}/health/ready", timeout=2):
                    break
            except OSError:
                if attempt == 29:
                    raise
                time.sleep(1)
    print(f"Service fixture: ttl={ttl}, isolated={isolated}, idle={idle}, event cap={event_cap}, access_enabled={access_enabled}", flush=True)


def runtime_snapshot():
    if os.environ.get("LOAD_PROFILE") != "traffic-multi":
        return {}
    result = {}
    for line in (root / "diagnostics.tsv").read_text().splitlines():
        name, port = line.split("\t")
        values = {}
        for endpoint in ("dbstats", "runtime"):
            with urllib.request.urlopen(f"http://127.0.0.1:{int(port)}/debug/{endpoint}", timeout=3) as response:
                values[endpoint] = json.load(response)
        result[name] = values
    if not result:
        raise RuntimeError("missing multi-channel diagnostics")
    return result


def report_runtime(label, before, after):
    for name, current in after.items():
        previous = before[name]
        db, old_db = current["dbstats"], previous["dbstats"]
        gc, old_gc = current["runtime"], previous["runtime"]
        print(f"Runtime {label} {name}: DB waits={db['WaitCount']-old_db['WaitCount']}, wait_ms={(db['WaitDuration']-old_db['WaitDuration'])/1e6:.2f}, open={db['OpenConnections']}, in_use={db['InUse']}, GC={gc['num_gc']-old_gc['num_gc']}, gc_pause_ms={(gc['pause_total_ns']-old_gc['pause_total_ns'])/1e6:.2f}, heap_mib={gc['heap_alloc']/1048576:.2f}, goroutines={gc['goroutines']}", flush=True)


def stage(label, config, user, event, delay=0, fail=False, required=False, seconds=20, apps=0):
    global valid
    mode(delay, fail)
    before, sql_before = stats(), mysql()
    runtime_before = runtime_snapshot()
    prefix = results / label
    env = dict(os.environ, LOAD_PROFILE="traffic", LOAD_SCENARIO="mixed", LOAD_TARGET="nginx",
               TRAFFIC_CONFIG=str(config), TRAFFIC_USER=str(user), TRAFFIC_EVENT=str(event),
               TRAFFIC_SECONDS=str(seconds), TRAFFIC_EVENT_DELAY=str(delay), TRAFFIC_EVENT_FAIL="1" if fail else "0", TRAFFIC_APPS=str(apps))
    print(f"\n### {label}: {config}/{user}/{event} QPS, event delay={delay}ms fail={fail}", flush=True)
    monitor = subprocess.Popen([sys.executable, "tests/load/process_monitor.py", str(prefix) + ".process.tsv"])
    try:
        with open(str(prefix) + ".txt", "w") as log:
            run = subprocess.run(["k6", "run", "--summary-export", str(prefix) + ".json",
                                  "tests/load/traffic.js"], env=env, stdout=log, stderr=subprocess.STDOUT, timeout=100)
    finally:
        monitor.terminate()
        monitor.wait(timeout=10)
    # Let timed-out upstream requests settle before taking mock counters.
    time.sleep(1)
    after, sql_after = stats(), mysql()
    report_runtime(label, runtime_before, runtime_snapshot())
    delta = {key: after[key] - before[key] for key in after}
    sql_delta = {key: sql_after[key] - sql_before[key] for key in sql_after
                 if key not in ("Threads_running", "Threads_connected")}
    print(f"Mock verified/accepted counters: {delta}; MySQL global deltas: {sql_delta}", flush=True)
    path = Path(str(prefix) + ".json")
    data = json.loads(path.read_text()) if path.exists() else {}
    complete = True
    counts = {}
    for name, rate in (("sdk_config", config), ("user_check", user), ("event_report", event)):
        if not rate:
            continue
        count = metric(data, "http_reqs", "count", name)
        failure = metric(data, "business_failed", "rate", name)
        if failure is None:
            failure = metric(data, "business_failed", "value", name)
        dropped = metric(data, "dropped_iterations", "count", name)
        p95, p99 = (metric(data, "http_req_duration", q, name) for q in ("p(95)", "p(99)"))
        counts[name] = count
        complete = complete and all(x is not None for x in (count, failure, p95, p99))
        rows.append((label, name, str(rate), fmt(count / seconds if count is not None else None),
                     fmt(p95), fmt(p99), fmt(None if failure is None else failure*100), fmt(dropped, 0),
                     str(run.returncode), "expected fail" if fail and name == "event_report" else "normal"))
        print("Endpoint result: " + " | ".join(rows[-1]), flush=True)
    # Successful fixtures must actually traverse the signed platform path, not a disabled switch.
    # Compare signed mock receipts to observed requests with the same 1% budget.
    plausible = (not user or counts.get("user_check") is not None and
                 delta["user_received"] >= counts["user_check"] * .99)
    if event:
        observed = counts.get("event_report")
        plausible = plausible and observed is not None and delta["event_received"] >= observed * .99
        plausible = plausible and (delta["event_accepted"] == 0 if fail else delta["event_accepted"] >= observed * .99) if observed is not None else False
    passed = run.returncode == 0 and complete and plausible
    print(f"Stage acceptance: {passed}, k6={run.returncode}, mock counter check={plausible}", flush=True)
    if not passed:
        print("\n".join(Path(str(prefix) + ".txt").read_text().splitlines()[-75:]), flush=True)
        if required or not data:
            valid = False
    return passed


try:
    if os.environ.get("LOAD_PROFILE") == "traffic-quick":
        # Fast mandatory PR regressions; use unchanged per-endpoint thresholds.
        # Each stage has its own 1:1:2 schedule and verified Mock receipt counts.
        stage("quick-mix", 100, 100, 200, required=True, seconds=10)
        stage("quick-event-delay", 100, 100, 200, delay=10, required=True, seconds=10)
        stage("quick-event-failure", 100, 100, 200, fail=True, required=True, seconds=10)
        stage("quick-recovery", 100, 100, 200, required=True, seconds=10)
        stage("quick-multi-16", 100, 100, 200, required=True, seconds=10, apps=16)
    elif os.environ.get("LOAD_PROFILE") == "traffic-multi":
        # Uniform cyclic traffic across 16 games, 16 apps each. Both A/B arms
        # use the same app count, connection settings and sampling rate.
        service_config("0s", True, 32, 0, False)
        stage("multi-no-cache-400", 100, 100, 200, apps=256, required=True)
        stage("multi-no-cache-1000", 250, 250, 500, apps=256)
        service_config("1s", True, 32, 0, False)
        stage("multi-cache-400", 100, 100, 200, apps=256, required=True)
        stage("multi-cache-1000", 250, 250, 500, apps=256)
        for rate in (500, 750, 1250):
            if not stage(f"multi-cache-{rate*4}", rate, rate, rate*2, apps=256):
                break
        stage("multi-recovery", 100, 100, 200, apps=256, required=True)
        service_config("0s", True, 32, 0, False)
    else:
        # Same-instance A/B: keep the production default cache disabled.
        service_config("0s", False, 2, 0, False)
        stage("ab-legacy-1000", 250, 250, 500)
        service_config("0s", True, 32, 0, False)
        stage("ab-pools-1000", 250, 250, 500)
        service_config("0s", True, 32, 0, True)
        stage("ab-access-log-on-1000", 250, 250, 500)
        service_config("1s", True, 32, 0, False)
        stage("ab-cache-1000", 250, 250, 500)
        stage("ab-cache-2000", 500, 500, 1000)
        # Connection cap experiment keeps synchronous completion, never fabricates success.
        service_config("1s", True, 32, 64, False)
        stage("ab-capped-overload", 100, 100, 2000, delay=50)
        # Restore the conservative production-like settings before required stages.
        service_config("0s", True, 32, 0, False)
        # A baseline for both critical routes, then independent event traffic.
        stage("critical-baseline", 100, 100, 0, required=True)
        stage("event-only-smoke", 0, 0, 10, required=True, seconds=10)
        for rate in (100, 250, 500, 1000, 2500, 5000):
            passed = stage(f"mix-{rate}", rate, rate, 2*rate, required=(rate == 100))
            if not passed:
                print("Saturation or threshold crossed: stop increasing the combined rate.", flush=True)
                break
        # Dedicated impact checks at a known lower critical load; higher overload is exploratory.
        for delay in (1, 10, 50, 100):
            stage(f"event-delay-{delay}ms", 100, 100, 200, delay=delay, required=(delay <= 10))
        stage("event-overload", 100, 100, 2000, delay=50)
        stage("event-platform-failure", 100, 100, 200, fail=True, required=True)
        stage("critical-recovery", 100, 100, 200, required=True)
finally:
    mode(0, False)
    print("\n## Per-endpoint results\n")
    print("| Stage | Endpoint | Target QPS | Completed HTTP/s* | P95 ms | P99 ms | Business fail % | Dropped | Stage exit | Expectation |")
    print("|---|---|---:|---:|---:|---:|---:|---:|---:|---|")
    for row in rows:
        print("| " + " | ".join(row) + " |")
    print("\n* Completed count divided by scheduled duration; includes grace-period completions. Higher pressure and 50/100ms delays are exploratory. Expected event failures must not change critical route thresholds.")
    print_process_report(results)
sys.exit(0 if valid else 1)
