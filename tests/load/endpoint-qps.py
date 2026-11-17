"""Bounded per-endpoint QPS ladders on disposable GitHub-hosted runners only."""
import csv
import json
import math
import os
from pathlib import Path
import subprocess
import sys
import time
from urllib.request import urlopen

from process_monitor import print_process_report

if os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("RUNNER_ENVIRONMENT") != "github-hosted":
    raise SystemExit("GitHub-hosted runner is required")
root = Path(sys.argv[1]).resolve()
results = root / "results"
results.mkdir(parents=True, exist_ok=True)
catalog = json.loads(Path("tests/load/endpoint-qps-catalog.json").read_text())
by_name = {case["id"]: case for case in catalog}
if len(catalog) != 52 or len(by_name) != len(catalog):
    raise SystemExit("Invalid or incomplete QPS catalog")
group = os.environ.get("ENDPOINT_GROUP", "quick")
tiers = os.environ.get("ENDPOINT_TIERS", "basic")
duration = int(os.environ.get("ENDPOINT_SECONDS", "4"))
selection = os.environ.get("ENDPOINT_CASE", "").strip()
if group not in ("quick", "read", "write", "all") or tiers not in ("basic", "high"):
    raise SystemExit("Invalid QPS group or tiers")
if duration < 3 or duration > 15:
    raise SystemExit("Stage seconds out of bounds: 3..15")
if selection and selection not in by_name:
    raise SystemExit("Unknown endpoint ID")
if selection:
    chosen = [by_name[selection]]
elif group == "quick":
    chosen = [by_name["core-config"]]
else:
    chosen = [c for c in catalog if group == "all" or c["group"] == group]

def mysql(query, database="config_center"):
    cmd = ["mysql", "-uroot", "-h127.0.0.1", "-N", "-B"]
    if database:
        cmd.append(database)
    return subprocess.check_output(cmd + ["-e", query], text=True, timeout=20).strip()

def seed_sql(lines):
    for start in range(0, len(lines), 250):
        sql = "\n".join(lines[start:start + 250]) + "\n"
        subprocess.run(["mysql", "-uroot", "-h127.0.0.1", "config_center"],
                       input=sql, text=True, check=True, timeout=30)

def seed_delete(case_id, start, n):
    statements = []
    for x in range(n):
        identifier = start + x
        if case_id == "config-delete-node":
            statements.append(
                "INSERT INTO config_node(id,game_id,region_id,node_type,env_type,market_type,"
                "code,name,status,config,created_at,updated_at) VALUES "
                f"({identifier},1,2,'server_group',NULL,NULL,'qps-delete-{identifier}',"
                "'Disposable QPS server group',1,JSON_OBJECT(),UTC_TIMESTAMP(),UTC_TIMESTAMP());")
        elif case_id == "config-delete-subscriber":
            statements.append(
                "INSERT INTO config_subscriber(id,code,callback_url,signing_secret,enabled,"
                "created_at,updated_at) VALUES "
                f"({identifier},'qps-delete-{identifier}','http://127.0.0.1:8290/config/webhook',"
                f"'runner-qps-secret-{identifier}-abcdef0123456789',0,UTC_TIMESTAMP(),UTC_TIMESTAMP());")
        elif case_id == "core-delete-channel":
            statements.append(
                "INSERT INTO sdk_channel_app(id,game_id,code,name,status,config,"
                "test_server_group_id,created_at,updated_at) VALUES "
                f"({identifier},1,'qps-delete-{identifier}','Disposable QPS channel',1,"
                "JSON_OBJECT(),NULL,UTC_TIMESTAMP(),UTC_TIMESTAMP());")
        else:
            raise RuntimeError("Unsupported DELETE benchmark")
    seed_sql(statements)

def mysql_status():
    data = mysql("SHOW GLOBAL STATUS WHERE Variable_name IN "
                 "('Threads_running','Threads_connected','Innodb_row_lock_waits',"
                 "'Innodb_row_lock_time','Com_select','Com_insert','Com_update','Com_delete','Questions')",
                 database="")
    result = {}
    for line in data.splitlines():
        parts = line.split("\t")
        if len(parts) == 2:
            result[parts[0]] = int(parts[1])
    return result

def value(data, name, field):
    metric = data.get("metrics", {}).get(name, {})
    return metric.get("values", metric).get(field)

def number(x, places=2):
    return "—" if x is None else f"{x:.{places}f}"

def reset_auth_nonce_cache(case_id):
    """Keep independent stage results free of accumulated 5-minute HMAC nonces."""
    name = ""
    if case_id.startswith("config-") and case_id not in (
        "config-revision", "config-snapshot", "config-changes", "config-node", "config-batch"):
        name = "config-center"
    elif case_id.startswith("core-") and case_id not in (
        "core-config", "core-order-register", "core-payment-duplicate"):
        name = "sdk-core"
    elif case_id.startswith("access-"):
        # Both access GET routes share one 10k HMAC nonce cache. Restart only
        # the isolated replica between stages so the second route gets a fair run.
        name = "config-center-access"
    if not name:
        return
    count = 2 if name == "config-center" else (4 if name == "sdk-core" and os.environ["LOAD_TOPOLOGY"] == "cluster" else 1)
    first = 8201 if name == "config-center" else (8211 if name == "sdk-core" else 8081)
    programs = [f"{name}-{i+1}" for i in range(count)] if name != "config-center-access" else [name]
    subprocess.run(["supervisorctl", "-c", str(root / "supervisor.conf"), "restart", *programs],
                   check=True, timeout=60, stdout=subprocess.DEVNULL)
    for offset in range(count):
        address = f"http://127.0.0.1:{first+offset}/health/ready"
        for attempt in range(30):
            try:
                with urlopen(address, timeout=2) as response:
                    if response.status == 200:
                        break
            except OSError:
                pass
            if attempt == 29:
                raise RuntimeError("Auth-cache reset failed: " + address)
            time.sleep(0.25)

# Prove all 52 API contracts before any load stages and create a paid order fixture.
preflight = results / "endpoint-qps-preflight"
with open(str(preflight) + ".log", "w") as output:
    proc = subprocess.run(
        ["k6","run","--summary-export",str(preflight)+".json","tests/load/api-coverage.js"],
        stdout=output,stderr=subprocess.STDOUT,timeout=120)
if proc.returncode:
    print("Full API preflight failed. Inspect endpoint-qps-preflight.log", flush=True)
    print("\n".join(Path(str(preflight)+".log").read_text().splitlines()[-70:]))
    raise SystemExit(1)

# Re-use only a disposable paid order for duplicate callback and retry rejection.
order_rows = mysql(
    "SELECT id,game_order_id,COALESCE(platform_order_no,''),amount "
    "FROM game_order WHERE appid='load-app' AND game_order_id LIKE 'cov-%' "
    "AND payment_status=1 ORDER BY id DESC LIMIT 1").split("\t")
if len(order_rows) != 4 or not order_rows[2]:
    raise SystemExit("Paid synthetic order fixture unavailable")
rev = mysql("SELECT epoch,revision FROM config_revision WHERE id=1").split("\t")
if len(rev) != 2:
    raise SystemExit("No local configuration revision found")
role = mysql("SELECT id FROM sdk_service WHERE code='role_query' LIMIT 1")
if not role.isdigit():
    raise SystemExit("Role service fixture missing")

# Separate disabled subscriber: never disable the one used for sync authentication.
seed_sql([
    "INSERT INTO config_subscriber(id,code,callback_url,signing_secret,enabled,created_at,updated_at)"
    " VALUES (90000,'qps-update-subscriber','http://127.0.0.1:8290/config/webhook',"
    "'runner-qps-update-subscriber-only-0123456789',0,UTC_TIMESTAMP(),UTC_TIMESTAMP());"
])
common = {
    "QPS_ORDER_ID":order_rows[0], "QPS_GAME_ORDER_ID":order_rows[1],
    "QPS_PLATFORM_ORDER_NO":order_rows[2], "QPS_ORDER_AMOUNT":order_rows[3],
    "QPS_EPOCH":rev[0], "QPS_REVISION":rev[1],
    "QPS_SUBSCRIBER_ID":"90000", "QPS_ROLE_SERVICE_ID":role
}
rows = []
baseline_failures = []
print("## GitHub-hosted endpoint QPS ladder (local-only)\n", flush=True)
print(f"Selected {len(chosen)} routes, group={group}, tiers={tiers}, seconds={duration}. "
      "One measured target request per iteration; no production traffic.\n", flush=True)
print("| Endpoint | Mode | Target QPS | VUs | Actual HTTP/s | P95 ms | P99 ms | Bad % | 401 | 409 | 429 | 503 | 5xx | Dropped | MySQL locks | Result |", flush=True)
print("|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|", flush=True)

for pos, case in enumerate(chosen):
    rates = ([50,200,500] if group == "quick" else
             [100,500,1000,3000,5000,10000,20000] if tiers == "high" and case["group"] == "read" else
             [20,50,100,250] if tiers == "high" else
             [50,200,500] if case["group"] == "read" else
             [10,30,60])
    if case["mode"] == "delete":
        rates = [r for r in rates if r <= 250]
    for tier, rate in enumerate(rates):
        # A shared Runner has limited memory. Drops may be generator-limited.
        vus = min(500, max(20, math.ceil(rate * 0.05)))
        planned = rate * duration
        base_id = (1000000 if case["id"] == "config-delete-node" else
                   2000000 if case["id"] == "config-delete-subscriber" else
                   3000000 if case["id"] == "core-delete-channel" else 0) + tier * 5000
        if case["mode"] == "delete":
            seed_delete(case["id"], base_id, planned + 5)
        reset_auth_nonce_cache(case["id"])
        prefix = results / f"qps-{pos:02d}-{case['id']}-{rate}"
        env = dict(os.environ, **common, QPS_CASE=case["id"], QPS_RATE=str(rate),
                   QPS_SECONDS=str(duration), QPS_VUS=str(vus),
                   QPS_BASE_ID=str(base_id), QPS_STAGE=f"c{pos:02d}-r{rate}")
        before = mysql_status()
        sampler = subprocess.Popen([sys.executable,"tests/load/process_monitor.py",
                                    str(prefix)+".process.tsv"])
        try:
            with open(str(prefix)+".log","w") as output:
                run = subprocess.run(
                    ["k6","run","--summary-export",str(prefix)+".json",
                     "tests/load/endpoint-qps.js"],
                    env=env,stdout=output,stderr=subprocess.STDOUT,
                    timeout=duration+50)
        finally:
            sampler.terminate()
            sampler.wait(timeout=10)
        after = mysql_status()
        data = json.loads(Path(str(prefix)+".json").read_text()) if Path(str(prefix)+".json").exists() else {}
        p95 = value(data,"http_req_duration","p(95)")
        p99 = value(data,"http_req_duration","p(99)")
        actual = value(data,"http_reqs","rate")
        errors = value(data,"endpoint_bad","value")
        if errors is None:
            errors = value(data,"endpoint_bad","rate")
        dropped = value(data,"dropped_iterations","count") or 0
        iterations = value(data,"iterations","count") or 0
        auth_conflicts = value(data,"endpoint_http_409","count") or 0
        throttles = value(data,"endpoint_http_429","count") or 0
        unauthenticated = value(data,"endpoint_http_401","count") or 0
        service_unavailable = value(data,"endpoint_http_503","count") or 0
        http_5xx = value(data,"endpoint_http_5xx","count") or 0
        lock_waits = after.get("Innodb_row_lock_waits",0) - before.get("Innodb_row_lock_waits",0)
        eligible = bool(
            run.returncode == 0 and data and errors is not None and errors < 0.01
            and p95 is not None and p95 < 500
            and p99 is not None and p99 < 1500
            and iterations >= planned * 0.99
            and dropped <= planned * 0.001)
        status = "PASS" if eligible else "STOP"
        row = {"id":case["id"],"route":case["route"],"group":case["group"],
               "mode":case["mode"],"target_qps":rate,"preallocated_vus":vus,
               "actual_http_qps":actual,"p95_ms":p95,"p99_ms":p99,"bad_rate":errors,
               "planned":planned,"iterations":iterations,"dropped":dropped,
               "sql_lock_waits":lock_waits,"exit":run.returncode,"qualified":eligible,
               "http_401":unauthenticated,"http_409":auth_conflicts,"http_429":throttles,
               "http_503":service_unavailable,"http_5xx":http_5xx,
               "diagnostic_hint":("signature_auth_401" if unauthenticated else
                                  "nonce_capacity_or_http_conflict_409" if auth_conflicts else
                                  "application_rate_limit_429" if throttles else
                                  "dependency_unavailable_503" if service_unavailable else
                                  "upstream_or_server_5xx" if http_5xx else
                                  "load_generator_or_sut_latency_drops" if dropped else
                                  "business_or_validation" if not eligible else "pass")}
        rows.append(row)
        print("| " + " | ".join([
            case["id"],case["mode"],str(rate),str(vus),number(actual),number(p95),
            number(p99),number(None if errors is None else errors*100),
            str(unauthenticated),str(auth_conflicts),str(throttles),
            str(service_unavailable),str(http_5xx),str(dropped),str(lock_waits),status
        ])+" |", flush=True)
        if not eligible:
            if tier == 0:
                baseline_failures.append(case["id"])
            if run.returncode or not data:
                print("~~~text\n" +
                      "\n".join(Path(str(prefix)+".log").read_text().splitlines()[-18:]) +
                      "\n~~~",flush=True)
            break
    qualified = max((r["target_qps"] for r in rows
                     if r["id"] == case["id"] and r["qualified"]), default=0)
    print(f"Endpoint {case['id']}: highest passing tested target = {qualified or 'none'} QPS", flush=True)

(results/"endpoint-qps-summary.json").write_text(json.dumps({
    "source":"github-hosted-loopback", "group":group,"tiers":tiers,
    "seconds":duration,"rows":rows, "baseline_failures":baseline_failures,
    "warnings":["single runner shared CPU: not production capacity",
                "duplicate-payment and retry-rejection are not success-path capacity",
                "delete stages use pre-seeded disposable rows",
                "failed stage stops further escalation for that endpoint"]
},ensure_ascii=False,indent=2))
with (results/"endpoint-qps-summary.csv").open("w",newline="") as f:
    writer=csv.DictWriter(f,fieldnames=list(rows[0].keys()) if rows else ["id"])
    writer.writeheader()
    writer.writerows(rows)
print("\nA passing target is the highest tested stable rate, not a mathematically "
      "proven maximum. STOP may be generator- or server-limited. "
      "Write stages are deliberately bounded.\n", flush=True)
print_process_report(results)
if baseline_failures:
    print("Baseline failures: " + ", ".join(baseline_failures), flush=True)
    sys.exit(1)
