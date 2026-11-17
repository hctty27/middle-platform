"""Runner-only 1:1:2 business mix and critical-interface isolation checks."""
import json
import os
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


def stage(label, config, user, event, delay=0, fail=False, required=False, seconds=20):
    global valid
    mode(delay, fail)
    before, sql_before = stats(), mysql()
    prefix = results / label
    env = dict(os.environ, LOAD_PROFILE="traffic", LOAD_SCENARIO="mixed", LOAD_TARGET="nginx",
               TRAFFIC_CONFIG=str(config), TRAFFIC_USER=str(user), TRAFFIC_EVENT=str(event),
               TRAFFIC_SECONDS=str(seconds), TRAFFIC_EVENT_DELAY=str(delay), TRAFFIC_EVENT_FAIL="1" if fail else "0")
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
