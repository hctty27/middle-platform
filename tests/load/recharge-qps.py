"""Hosted-only recharge transaction/s and duplicate payment callback/s benchmarks.

A first-time recharge consists of two independent HTTP requests:
game order registration and the first signed platform payment callback.
Never label http_reqs/s as recharge transactions/s.
"""
import csv
import json
import math
import os
from pathlib import Path
import subprocess
import sys

if os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("RUNNER_ENVIRONMENT") != "github-hosted":
    raise SystemExit("Recharge benchmark requires an isolated GitHub-hosted runner")
if os.environ.get("ENDPOINT_BUSINESS_ONLY") != "true" or os.environ.get("LOAD_PROFILE") != "endpoint-qps":
    raise SystemExit("Recharge benchmark requires non-admin endpoint QPS workflow")
root = Path(sys.argv[1]).resolve()
results = root / "results"
results.mkdir(parents=True, exist_ok=True)
seconds = 5
rates = (20, 50, 100, 250)

def metric(data, name, key):
    raw = data.get("metrics", {}).get(name, {})
    return raw.get("values", raw).get(key)

def count(data, name):
    return int(metric(data, name, "count") or 0)

def query(sql):
    return subprocess.check_output(
        ["mysql", "-uroot", "-h127.0.0.1", "-N", "-B", "config_center", "-e", sql],
        text=True, timeout=25).strip()

def run_k6(label, script, env):
    prefix = results / label
    with (Path(str(prefix) + ".log")).open("w") as log:
        proc = subprocess.run(
            ["k6", "run", "--summary-export", str(prefix) + ".json", script],
            env=env, stdout=log, stderr=subprocess.STDOUT, timeout=seconds + 65)
    summary_path = Path(str(prefix) + ".json")
    if not summary_path.exists():
        print("\n".join(Path(str(prefix) + ".log").read_text().splitlines()[-30:]), flush=True)
        raise SystemExit("Recharge test did not write a summary: " + label)
    return proc.returncode, json.loads(summary_path.read_text())

def qualifier(data, exit_code, target):
    planned = target * seconds
    iterations = count(data, "iterations")
    dropped = count(data, "dropped_iterations")
    failed = metric(data, "business_failed", "value")
    if failed is None:
        failed = metric(data, "endpoint_bad", "value")
    p95 = metric(data, "http_req_duration", "p(95)")
    p99 = metric(data, "http_req_duration", "p(99)")
    return (exit_code == 0 and failed is not None and failed < .01
            and p95 is not None and p95 < 500
            and p99 is not None and p99 < 1500
            and iterations >= .99 * planned and dropped <= .001 * planned)

rows = []
print("## Hosted-only recharge benchmarks (no admin API)\n", flush=True)
print("| Scenario | Target/s | Completed iterations | HTTP/s | Business bad % | P95 ms | P99 ms | Dropped | Paid rows | Status |", flush=True)
print("|---|---:|---:|---:|---:|---:|---:|---:|---:|---|", flush=True)

# First-time recharge flow, requiring a unique unpaid order on every iteration.
# This scenario performs TWO HTTP calls: registration followed by payment notify.
first_baseline_failed = False
for rate in rates:
    run_id = "rq" + os.environ["GITHUB_RUN_ID"] + "r" + str(rate)
    env = dict(os.environ, LOAD_SCENARIO="payment", LOAD_RATE=str(rate),
               LOAD_DURATION=str(seconds) + "s", LOAD_RUN_ID=run_id)
    rc, data = run_k6(f"recharge-first-{rate}-tps", "tests/load/scenarios.js", env)
    iterations = count(data, "iterations")
    rows_total, rows_paid = query(
        "SELECT COUNT(*), COALESCE(SUM(payment_status=1),0) "
        "FROM game_order WHERE appid='load-app' AND game_order_id LIKE '" + run_id + "-%'").split("\t")
    paid = int(rows_paid)
    total = int(rows_total)
    qualified = bool(qualifier(data, rc, rate) and paid >= .99 * rate * seconds
                     and paid == iterations and total == iterations)
    failed = metric(data, "business_failed", "value")
    row = dict(scenario="first_recharge",rate_per_second=rate,
               iterations=iterations,actual_http_per_second=metric(data,"http_reqs","rate"),
               business_bad_rate=failed,p95_ms=metric(data,"http_req_duration","p(95)"),
               p99_ms=metric(data,"http_req_duration","p(99)"),
               dropped=count(data,"dropped_iterations"),registered_rows=total,
               paid_rows=paid,nonces_409=count(data,"failed_http_409"),
               auth_401=count(data,"failed_http_401"),
               store_503=count(data,"failed_http_503"),exit_code=rc,qualified=qualified)
    rows.append(row)
    print(f"| First recharge | {rate} TPS | {iterations} | "
          f"{(row['actual_http_per_second'] or 0):.1f} | "
          f"{(failed or 0)*100:.2f} | "
          f"{(row['p95_ms'] or 0):.2f} | {(row['p99_ms'] or 0):.2f} | "
          f"{row['dropped']} | {paid} | {'PASS' if qualified else 'STOP'} |", flush=True)
    if not qualified:
        if rate == rates[0]:
            first_baseline_failed = True
        print("First recharge stopped at target " + str(rate) + " TPS", flush=True)
        break

# Repeated callback is fundamentally different from the first payment.
# A dedicated, already-paid synthetic order is created in api-coverage preflight.
existing = query(
    "SELECT id,game_order_id,COALESCE(platform_order_no,''),amount "
    "FROM game_order WHERE appid='load-app' AND game_order_id LIKE 'cov-%' "
    "AND payment_status=1 ORDER BY id DESC LIMIT 1").split("\t")
if len(existing) != 4 or not existing[2]:
    raise SystemExit("No previously paid synthetic order for duplicate callback")
duplicate_baseline_failed = False
for rate in (50, 200, 500):
    env = dict(os.environ, QPS_CASE="core-payment-duplicate", QPS_RATE=str(rate),
               QPS_SECONDS=str(seconds), QPS_VUS=str(max(20, math.ceil(rate*.05))),
               QPS_ORDER_ID=existing[0],QPS_GAME_ORDER_ID=existing[1],
               QPS_PLATFORM_ORDER_NO=existing[2],QPS_ORDER_AMOUNT=existing[3],
               QPS_STAGE="recharge-dup-"+str(rate))
    rc, data = run_k6(f"recharge-duplicate-{rate}-qps", "tests/load/endpoint-qps.js", env)
    qualified = bool(qualifier(data, rc, rate))
    row = dict(scenario="duplicate_payment_callback",rate_per_second=rate,
               iterations=count(data,"iterations"),
               actual_http_per_second=metric(data,"http_reqs","rate"),
               business_bad_rate=metric(data,"endpoint_bad","value"),
               p95_ms=metric(data,"http_req_duration","p(95)"),
               p99_ms=metric(data,"http_req_duration","p(99)"),
               dropped=count(data,"dropped_iterations"),registered_rows=None,
               paid_rows=None,nonces_409=count(data,"endpoint_http_409"),
               auth_401=count(data,"endpoint_http_401"),
               store_503=count(data,"endpoint_http_503"),exit_code=rc,qualified=qualified)
    rows.append(row)
    print(f"| Duplicate callback | {rate} QPS | {row['iterations']} | "
          f"{(row['actual_http_per_second'] or 0):.1f} | "
          f"{(row['business_bad_rate'] or 0)*100:.2f} | "
          f"{(row['p95_ms'] or 0):.2f} | {(row['p99_ms'] or 0):.2f} | "
          f"{row['dropped']} | — | {'PASS' if qualified else 'STOP'} |", flush=True)
    if not qualified:
        if rate == 50:
            duplicate_baseline_failed = True
        print("Duplicate callback stopped at target " + str(rate) + " QPS", flush=True)
        break

# Verify repeated notifications did not change payment state.
if query("SELECT payment_status FROM game_order WHERE id=" + str(int(existing[0]))) != "1":
    raise SystemExit("Already-paid fixture changed state during duplicate callbacks")
(results/"recharge-qps-summary.json").write_text(json.dumps({
    "source":"github-hosted-loopback",
    "warning":"First recharge measures 2 HTTP requests per transaction (TPS, not QPS). "
              "Duplicate callback QPS is idempotent replay performance, not new payment throughput.",
    "rows":rows
},ensure_ascii=False,indent=2))
with (results/"recharge-qps-summary.csv").open("w",newline="") as output:
    writer = csv.DictWriter(output,fieldnames=list(rows[0]))
    writer.writeheader()
    writer.writerows(rows)
if first_baseline_failed or duplicate_baseline_failed:
    sys.exit(1)
