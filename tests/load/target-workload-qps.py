"""Concurrent quarter-target workload for all non-admin business APIs.

All traffic stays on the disposable GitHub-hosted runner. Category QPS is an
aggregate target distributed evenly over the routes in that category.
"""
import csv
import json
import math
import os
from pathlib import Path
import re
import subprocess
import sys
import time

from process_monitor import print_process_report

if os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("RUNNER_ENVIRONMENT") != "github-hosted":
    raise SystemExit("GitHub-hosted runner is required")
if os.environ.get("LOAD_PROFILE") != "endpoint-qps":
    raise SystemExit("Quarter workload requires endpoint-qps profile")
if os.environ.get("LOAD_TOPOLOGY") != "cluster":
    raise SystemExit("Quarter workload requires cluster topology")
if os.environ.get("ENDPOINT_BUSINESS_ONLY") != "true":
    raise SystemExit("Quarter workload must exclude admin APIs")
if os.environ.get("SDK_SERVICE_CACHE_TTL") != "1s":
    raise SystemExit("Quarter workload requires the isolated SDK Service 1s cache A/B setting")

root = Path(sys.argv[1]).resolve()
results = root / "results"
results.mkdir(parents=True, exist_ok=True)
config = json.loads(Path("tests/load/target-workload.json").read_text())
catalog = json.loads(Path("tests/load/endpoint-qps-catalog.json").read_text())
catalog_by_id = {item["id"]: item for item in catalog}
duration = int(config["duration_seconds"])
scale = float(config["scale"])
stage = f"quarter-{os.environ['GITHUB_RUN_ID']}-{os.environ.get('GITHUB_RUN_ATTEMPT', '1')}"
expected_ttl = (results / "sdk-service-cache-ttl.txt").read_text().strip()
if expected_ttl != "1s":
    raise SystemExit(f"SDK Service cache TTL mismatch: {expected_ttl!r}")
if (results / "topology.txt").read_text().strip() != "cluster":
    raise SystemExit("Runner topology marker is not cluster")

supervisor = subprocess.check_output(
    ["supervisorctl", "-c", str(root / "supervisor.conf"), "status"], text=True)
service_instances = sorted(
    int(match.group(1)) for match in re.finditer(r"^sdk-service-(\d+)\s+RUNNING\b", supervisor, re.M))
if service_instances != [1, 2, 3, 4, 5, 6]:
    raise SystemExit(f"Expected six running SDK Service instances, found {service_instances}")

def route_id_for_catalog(workload_id):
    return "core-payment-duplicate" if workload_id == "core-payment-notify" else workload_id

def route_rates():
    result = []
    for category, workload_ids in config["categories"].items():
        full = int(config["full_target_qps"][category])
        target = int(full * scale)
        if target != full // 4:
            raise SystemExit(f"Category {category} is not an exact quarter target")
        base, remainder = divmod(target, len(workload_ids))
        for index, workload_id in enumerate(workload_ids):
            route_id = route_id_for_catalog(workload_id)
            entry = catalog_by_id.get(route_id)
            if not entry or "/admin/" in entry["route"]:
                raise SystemExit(f"Admin/unknown API is included in workload: {workload_id}")
            result.append({
                "id": workload_id,
                "category": category,
                "route": entry["route"],
                "rate": base + (1 if index < remainder else 0),
            })
    return result

cases = route_rates()
target_total = sum(case["rate"] for case in cases)
category_targets = {
    category: int(full * scale)
    for category, full in config["full_target_qps"].items()
}
if len(cases) != 19 or target_total != 11600:
    raise SystemExit(f"Unexpected workload composition: {len(cases)} routes / {target_total} QPS")
if len({case["id"] for case in cases}) != len(cases):
    raise SystemExit("Duplicate route IDs in workload")

def mysql(query, database="config_center"):
    command = ["mysql", "-uroot", "-h127.0.0.1", "-N", "-B"]
    if database:
        command.append(database)
    return subprocess.check_output(command + ["-e", query], text=True, timeout=30).strip()

def run_sql(sql):
    subprocess.run(
        ["mysql", "-uroot", "-h127.0.0.1", "config_center"],
        input=sql, text=True, check=True, timeout=60)

def seed_unpaid_payment_orders(count):
    # Each payment callback must confirm a distinct unpaid order. Replaying an
    # already-paid order would only test idempotency, not first-time payment.
    chunk_size = 500
    for start in range(0, count, chunk_size):
        values = []
        for index in range(start, min(start + chunk_size, count)):
            game_order_id = f"qps-pay-{stage}-{index}"
            values.append(
                "('load-app',1,'test',"
                f"'{game_order_id}',100,'runner-qps',"
                "'http://127.0.0.1:8290/game/notify')")
        sql = (
            "INSERT INTO game_order"
            "(appid,game_id,env,game_order_id,amount,attach,game_notify_url) VALUES\n"
            + ",\n".join(values) + ";\n"
        )
        run_sql(sql)

def mysql_status():
    output = mysql(
        "SHOW GLOBAL STATUS WHERE Variable_name IN "
        "('Threads_connected','Threads_running','Max_used_connections','Slow_queries',"
        "'Questions','Com_select','Com_insert','Com_update','Innodb_row_lock_waits',"
        "'Innodb_row_lock_time')", database="")
    result = {}
    for line in output.splitlines():
        parts = line.split("\t")
        if len(parts) == 2:
            try:
                result[parts[0]] = int(parts[1])
            except ValueError:
                pass
    return result

def metric_values(data, metric_name):
    metric = data.get("metrics", {}).get(metric_name, {})
    return metric.get("values", metric)

def metric_number(data, metric_name, field):
    value = metric_values(data, metric_name).get(field)
    return float(value) if value is not None else None

def scenario_dropped(data, scenario):
    for name, metric in data.get("metrics", {}).items():
        if name.startswith("dropped_iterations{") and f"scenario:{scenario}" in name:
            raw = metric.get("values", metric)
            return int(raw.get("count", 0) or 0)
    return None

def format_number(value, digits=2):
    return "—" if value is None else f"{value:.{digits}f}"

# Seed a one-to-one pool of unpaid payment orders before load begins.
payment_rate = category_targets["payment"]
payment_planned = payment_rate * duration
seed_unpaid_payment_orders(payment_planned)
seeded = int(mysql(
    "SELECT COUNT(*) FROM game_order WHERE appid='load-app' "
    f"AND game_order_id LIKE 'qps-pay-{stage}-%'"))
if seeded != payment_planned:
    raise SystemExit(f"Payment fixture count mismatch: {seeded} != {payment_planned}")

(results / "quarter-workload-stage.txt").write_text(stage + "\n")
(results / "quarter-workload-targets.json").write_text(
    json.dumps({"duration_seconds": duration, "scale": scale,
                "category_targets": category_targets, "routes": cases,
                "total_target_qps": target_total, "sdk_service_instances": service_instances},
               ensure_ascii=False, indent=2))

print("## Concurrent quarter-target business workload\n", flush=True)
print(f"- Runner-only target: {target_total:,} QPS aggregate for {duration}s")
print(f"- Cluster: config-center=2, sdk-core=4, sdk-service={len(service_instances)}")
print(f"- SDK Service configCache TTL: {expected_ttl}")
print("- Scope: 19 non-admin routes; all /admin/ routes excluded from high-QPS traffic.")
print("- First-time payment callbacks use unique pre-seeded unpaid orders; no duplicate replay is counted as payment throughput.\n")
print("| Category | Full target | Quarter target | Routes |", flush=True)
print("|---|---:|---:|---:|", flush=True)
for category, full in config["full_target_qps"].items():
    print(f"| {category} | {int(full):,} | {category_targets[category]:,} | "
          f"{len(config['categories'][category])} |", flush=True)
print("\n| Endpoint | Category | Target QPS | VUs | Actual QPS | P95 ms | P99 ms | Bad % | Dropped | 401 | 409 | 429 | 503 | 5xx | DB rows | Result |")
print("|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|", flush=True)

before = mysql_status()
monitor = subprocess.Popen([
    sys.executable, "tests/load/process_monitor.py",
    str(results / "quarter-workload.process.tsv")
])
summary_path = results / "quarter-workload-summary.json"
log_path = results / "quarter-workload.log"
env = dict(os.environ, QPS_WORKLOAD="quarter-target",
           QPS_SECONDS=str(duration), QPS_STAGE=stage)
try:
    with log_path.open("w") as output:
        run = subprocess.run(
            ["k6", "run", "--summary-export", str(summary_path),
             "tests/load/endpoint-qps.js"],
            env=env, stdout=output, stderr=subprocess.STDOUT,
            timeout=duration + 120)
finally:
    monitor.terminate()
    monitor.wait(timeout=10)

after = mysql_status()
if not summary_path.exists():
    print("\n".join(log_path.read_text().splitlines()[-60:]), flush=True)
    raise SystemExit("k6 did not produce a workload summary")
data = json.loads(summary_path.read_text())

# Confirm the two independent order write paths persisted what the endpoints accepted.
registered_orders = int(mysql(
    "SELECT COUNT(*) FROM game_order WHERE appid='load-app' "
    f"AND game_order_id LIKE 'qps-order-{stage}-%'"))
payment_stats = mysql(
    "SELECT COUNT(*),COALESCE(SUM(payment_status=1),0),"
    "COALESCE(SUM(payment_status=1 AND notify_status=1),0),"
    "COALESCE(SUM(payment_status=1 AND notify_status<>1),0),"
    "COALESCE(SUM(payment_status=1 AND notify_status=2),0) "
    "FROM game_order WHERE appid='load-app' "
    f"AND game_order_id LIKE 'qps-pay-{stage}-%'").split("\t")
payment_total, payment_paid, notify_success, notify_pending, notify_failed = map(int, payment_stats)

rows = []
failures = []
for case in cases:
    suffix = case["id"].replace("-", "_")
    planned = case["rate"] * duration
    iterations = int(metric_number(data, "workload_iterations_" + suffix, "count") or 0)
    requests = int(metric_number(data, "workload_requests_" + suffix, "count") or 0)
    bad_rate = metric_number(data, "workload_bad_" + suffix, "rate")
    p95 = metric_number(data, "workload_latency_" + suffix, "p(95)")
    p99 = metric_number(data, "workload_latency_" + suffix, "p(99)")
    builtin_dropped = scenario_dropped(data, case["id"])
    dropped = max(0, planned - iterations)
    if builtin_dropped is not None:
        dropped = max(dropped, builtin_dropped)
    actual_qps = requests / duration
    http_401 = int(metric_number(data, "workload_http401_" + suffix, "count") or 0)
    http_409 = int(metric_number(data, "workload_http409_" + suffix, "count") or 0)
    http_429 = int(metric_number(data, "workload_http429_" + suffix, "count") or 0)
    http_503 = int(metric_number(data, "workload_http503_" + suffix, "count") or 0)
    http_5xx = int(metric_number(data, "workload_http5xx_" + suffix, "count") or 0)
    db_rows = None
    if case["id"] == "core-order-register":
        db_rows = registered_orders
    elif case["id"] == "core-payment-notify":
        db_rows = payment_paid
    vu_count = max(20, math.ceil(case["rate"] * 0.05))
    qualified = bool(
        run.returncode == 0 and requests >= planned * 0.99
        and iterations >= planned * 0.999 and dropped <= planned * 0.001
        and bad_rate is not None and bad_rate < 0.01
        and p95 is not None and p95 < 500
        and p99 is not None and p99 < 1500
        and (db_rows is None or db_rows >= requests * 0.99)
    )
    if not qualified:
        failures.append(case["id"])
    row = {
        "id": case["id"], "category": case["category"], "route": case["route"],
        "target_qps": case["rate"], "preallocated_vus": vu_count,
        "actual_qps": actual_qps, "planned_iterations": planned,
        "iterations": iterations, "requests": requests, "dropped": dropped,
        "bad_rate": bad_rate, "p95_ms": p95, "p99_ms": p99,
        "http_401": http_401, "http_409": http_409, "http_429": http_429,
        "http_503": http_503, "http_5xx": http_5xx,
        "db_rows": db_rows, "qualified": qualified,
    }
    rows.append(row)
    print("| " + " | ".join([
        case["id"], case["category"], str(case["rate"]), str(vu_count),
        format_number(actual_qps), format_number(p95), format_number(p99),
        format_number(None if bad_rate is None else bad_rate * 100),
        str(dropped), str(http_401), str(http_409), str(http_429),
        str(http_503), str(http_5xx), "—" if db_rows is None else str(db_rows),
        "PASS" if qualified else "STOP"
    ]) + " |", flush=True)

category_rows = []
for category, target in category_targets.items():
    selected = [row for row in rows if row["category"] == category]
    actual = sum(row["requests"] for row in selected) / duration
    qualified = all(row["qualified"] for row in selected)
    category_rows.append({"category": category, "target_qps": target,
                          "actual_qps": actual, "qualified": qualified})
print("\n| Category | Target QPS | Actual aggregate QPS | Result |")
print("|---|---:|---:|---|")
for row in category_rows:
    print(f"| {row['category']} | {row['target_qps']} | {row['actual_qps']:.1f} | "
          f"{'PASS' if row['qualified'] else 'STOP'} |", flush=True)

# Payment delivery is asynchronous; report its backlog separately from the
# synchronous payment callback throughput.
deadline = time.monotonic() + 60
while notify_pending and time.monotonic() < deadline:
    time.sleep(1)
    stats = mysql(
        "SELECT COALESCE(SUM(payment_status=1 AND notify_status<>1),0),"
        "COALESCE(SUM(payment_status=1 AND notify_status=2),0) "
        "FROM game_order WHERE appid='load-app' "
        f"AND game_order_id LIKE 'qps-pay-{stage}-%'").split("\t")
    notify_pending, notify_failed = map(int, stats)

summary = {
    "source": "github-hosted-loopback",
    "duration_seconds": duration,
    "scale": scale,
    "total_target_qps": target_total,
    "total_actual_qps": sum(row["requests"] for row in rows) / duration,
    "sdk_service_instances": service_instances,
    "sdk_service_cache_ttl": expected_ttl,
    "topology": "cluster 2/4/6",
    "category_rows": category_rows,
    "rows": rows,
    "order_registration_rows": registered_orders,
    "payment_fixture_rows": payment_total,
    "payment_paid_rows": payment_paid,
    "payment_notify_success_rows": notify_success,
    "payment_notify_pending_rows_after_wait": notify_pending,
    "payment_notify_failed_rows_after_wait": notify_failed,
    "mysql_status_before": before,
    "mysql_status_after": after,
    "failures": failures,
    "warnings": [
        "All workload traffic is loopback-only on a GitHub-hosted runner.",
        "The load generator and SUT share one runner; generator saturation can limit achieved QPS.",
        "Category targets are aggregate QPS evenly distributed across their listed routes.",
        "Payment callback uses one unique unpaid synthetic order per iteration.",
        "Async game delivery status is reported separately from synchronous payment callback acceptance.",
        "No /admin/ endpoint participates in high-QPS workload."
    ],
}
(results / "quarter-workload-summary.json").write_text(
    json.dumps(summary, ensure_ascii=False, indent=2))
with (results / "quarter-workload-summary.csv").open("w", newline="") as output:
    writer = csv.DictWriter(output, fieldnames=list(rows[0].keys()))
    writer.writeheader()
    writer.writerows(rows)
(results / "quarter-workload-summary.md").write_text(
    "## Quarter-target workload result\n\n" +
    f"Target {target_total:,} QPS, actual {summary['total_actual_qps']:.1f} QPS, "
    f"duration {duration}s, topology 2/4/6.\n\n" +
    "| Category | Target | Actual | Result |\n|---|---:|---:|---|\n" +
    "".join(f"| {x['category']} | {x['target_qps']} | {x['actual_qps']:.1f} | "
            f"{'PASS' if x['qualified'] else 'STOP'} |\n" for x in category_rows) +
    f"\nPayment fixture: {payment_total} seeded, {payment_paid} paid; "
    f"{notify_pending} notification pending and {notify_failed} failed after the 60s drain window.\n"
)
print(f"\nPayment DB verification: seeded={payment_total}, paid={payment_paid}, "
      f"notify_success={notify_success}, pending_after_60s={notify_pending}, "
      f"notify_failed={notify_failed}", flush=True)
print(f"MySQL status delta: {json.dumps({k: after.get(k,0)-before.get(k,0) for k in before}, ensure_ascii=False)}", flush=True)
print_process_report(results)
if failures:
    print("Quarter workload failed endpoints: " + ", ".join(failures), flush=True)
    sys.exit(1)
print("All quarter-target non-admin API categories passed.", flush=True)
