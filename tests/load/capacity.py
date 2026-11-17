"""Bounded local capacity exploration; profiling runs are separate from the ladder."""
import concurrent.futures as cf
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import urllib.request
from process_monitor import print_process_report
from config_patch import core_settings

root = Path(sys.argv[1])
results = root / "results"
if os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("RUNNER_ENVIRONMENT") != "github-hosted":
    raise SystemExit("Disposable GitHub-hosted runner required")
servers = {}
for line in (root / "diagnostics.tsv").read_text().splitlines():
    name, port = line.split("\t")
    servers[name] = int(port)
if not servers:
    raise SystemExit("Missing tagged diagnostic listeners")


def get(port, path, timeout=8):
    with urllib.request.urlopen(f"http://127.0.0.1:{port}{path}", timeout=timeout) as response:
        return response.read()


def pools():
    return {name: json.loads(get(port, "/debug/dbstats")) for name, port in servers.items()}


def sql_stats():
    query = ("SELECT EVENT_NAME,COUNT_STAR,SUM_TIMER_WAIT FROM "
             "performance_schema.events_statements_summary_global_by_event_name "
             "WHERE COUNT_STAR>0 AND (EVENT_NAME LIKE 'statement/sql/%' "
             "OR EVENT_NAME LIKE 'statement/com/%')")
    raw = subprocess.check_output(["mysql", "-uroot", "-h127.0.0.1", "-N", "-B", "-e", query], text=True)
    return {key: [int(count), int(timer)] for key, count, timer in (line.split("\t") for line in raw.splitlines())}


def capture(name, label):
    port = servers[name]
    binary = root / "bin" / name.rsplit("-", 1)[0]
    # Start inside the traffic stage, not during the k6 initialization.
    time.sleep(5)
    files = []
    for kind, endpoint in (("heap", "heap"), ("mutex", "mutex"), ("block", "block"), ("cpu", "profile?seconds=15")):
        path = results / f"{label}.{name}.{kind}.pprof"
        path.write_bytes(get(port, "/debug/pprof/" + endpoint, 30))
        files.append((kind, path))
    for kind, path in files:
        cmd = ["go", "tool", "pprof", "-top", "-nodecount=12"]
        if kind == "heap":
            cmd.append("-inuse_space")
        cmd.extend([str(binary), str(path)])
        report = subprocess.check_output(cmd, text=True, stderr=subprocess.STDOUT, timeout=30)
        path.with_suffix(".txt").write_text(report)
        print(f"\n### {label} / {name} / {kind}\n\n```text\n{report}\n```", flush=True)


def value(data, metric, field):
    val = data.get("metrics", {}).get(metric, {})
    return val.get("values", val).get(field)


def fmt(n):
    return "—" if n is None else f"{n:.2f}"


rows = []
required_ok = True
print("## Capacity exploration (same-host, not production capacity)\n", flush=True)


def stage(label, scenario, target, rate, duration, required=False, profile=None):
    global required_ok
    env = dict(os.environ, LOAD_PROFILE="capacity", LOAD_SCENARIO=scenario, LOAD_TARGET=target,
               LOAD_RATE=str(rate), LOAD_DURATION=duration, LOAD_RUN_ID=label)
    before, sql_before = pools(), sql_stats()
    prefix = results / label
    sampler = subprocess.Popen([sys.executable, "tests/load/process_monitor.py", str(prefix) + ".process.tsv"])
    print(f"\n### {label} ({rate} iterations/s)\n", flush=True)
    profile_ok = True
    try:
        with cf.ThreadPoolExecutor(max_workers=1) as executor:
            future = executor.submit(capture, profile, label) if profile else None
            # k6 progress remains in the Actions job log, not duplicated in Summary.
            with open(str(prefix) + ".k6.txt", "w") as log:
                proc = subprocess.run(["k6", "run", "--summary-export", str(prefix) + ".json",
                                       "tests/load/scenarios.js"], env=env, stdout=log, stderr=subprocess.STDOUT, timeout=100)
            if future:
                try:
                    future.result()
                except Exception as exc:
                    profile_ok = False
                    print(f"Profile capture failed: {exc}", flush=True)
    finally:
        sampler.terminate()
        sampler.wait(timeout=10)
    after, sql_after = pools(), sql_stats()
    Path(str(prefix) + ".pools.json").write_text(json.dumps({"before": before, "after": after}, indent=2))
    Path(str(prefix) + ".sql.json").write_text(json.dumps({"before": sql_before, "after": sql_after}, indent=2))
    print("| DB pool | Open | In use | Idle | Waits delta | Wait ms delta |")
    print("|---|---:|---:|---:|---:|---:|")
    for name, end in after.items():
        start = before[name]
        print(f"| {name} | {end['OpenConnections']} | {end['InUse']} | {end['Idle']} | {end['WaitCount']-start['WaitCount']} | {(end['WaitDuration']-start['WaitDuration'])/1e6:.2f} |")
    ranked = []
    for name, (count, timer) in sql_after.items():
        old_count, old_timer = sql_before.get(name, [0, 0])
        n, t = count-old_count, timer-old_timer
        if n > 0 and t >= 0:
            ranked.append((t, n, name))
    print("\nTop MySQL statement event deltas (global, elapsed may overlap):")
    for t, n, name in sorted(ranked, reverse=True)[:5]:
        print(f"- {name}: {n} calls, {t/1e9:.2f} ms total")
    data = json.loads(Path(str(prefix) + ".json").read_text()) if Path(str(prefix) + ".json").exists() else {}
    if not data:
        print("No k6 summary produced; see raw log.")
    failure = value(data, "business_failed", "rate")
    if failure is None:
        failure = value(data, "business_failed", "value")
    dropped = value(data, "dropped_iterations", "count")
    rows.append((label, str(rate), fmt(value(data, "http_reqs", "rate")),
                 fmt(value(data, "http_req_duration", "p(95)")),
                 fmt(value(data, "http_req_duration", "p(99)")), fmt(None if failure is None else failure*100),
                 str(dropped) if dropped is not None else "—", str(proc.returncode), "yes" if required else "no"))
    print("Stage result: " + " | ".join(rows[-1]), flush=True)
    # Preserve threshold evidence even when artifact quota blocks raw files.
    if proc.returncode:
        log_lines = Path(str(prefix) + ".k6.txt").read_text().splitlines()
        print("\n```text\n" + "\n".join(log_lines[-50:]) + "\n```", flush=True)
    if required and (proc.returncode != 0 or not profile_ok or not data):
        required_ok = False


# Change only the disposable SDK Core YAML; restart all configured SDK Core instances.
def core_config(sample, max_open=20, max_idle=10):
    config = root / "config" / "sdk-core.yaml"
    config.write_text(core_settings(config.read_text(), sample, max_open, max_idle))
    names = [name for name in servers if name.startswith("sdk-core-")]
    subprocess.run(["supervisorctl", "-c", str(root / "supervisor.conf"), "restart", *names], check=True)
    for name in names:
        port = servers[name]
        for attempt in range(30):
            try:
                get(port-100, "/health/ready")
                get(port, "/debug/dbstats")
                break
            except OSError:
                if attempt == 29:
                    raise
                time.sleep(1)

# Same runner, same Nginx, same cached response implementation: isolate the log setting.
core_config(1)
stage("log-full-warmup", "sdk-config", "direct", 1000, "10s")
for target in ("nginx", "direct"):
    stage(f"log-full-{target}-3000", "sdk-config", target, 3000, "20s")
core_config(10)
stage("log-sampled-warmup", "sdk-config", "direct", 1000, "10s")
for target in ("nginx", "direct"):
    stage(f"log-sampled-{target}-3000", "sdk-config", target, 3000, "20s")

# Fixed, bounded paired stages; report failing high targets instead of hiding them.
# Compare one compact sdk-core directly vs via the same Nginx; cluster direct is a single backend.
for rate in (1000, 3000, 5000, 7500):
    for target in ("nginx", "direct"):
        stage(f"sdk-{target}-{rate}", "sdk-config", target, rate, "20s", required=(rate == 1000))
# A short push to the 10k target followed by recovery at the baseline.
for target in ("nginx", "direct"):
    stage(f"sdk-{target}-burst-10000", "sdk-config", target, 10000, "15s")
    time.sleep(3)
    stage(f"sdk-{target}-recovery-1000", "sdk-config", target, 1000, "20s", required=True)
# Dedicated lower-rate profiling; results are not mixed with the throughput ladder.
for scenario, rate, name in (("sdk-config", 1000, "sdk-core-1"), ("order", 250, "sdk-core-1"),
                             ("user-check", 250, "sdk-service-1"), ("config-sync", 250, "config-center-1")):
    stage(f"profile-{scenario}", scenario, "nginx", rate, "35s", required=True, profile=name)
# Compare DB pool sizes without changing production defaults or SQL semantics.
# Both stages use distinct synthetic orders, with identical logger, rate and duration.
stage("order-pool20-500", "order", "nginx", 500, "30s")
core_config(10, 32, 16)
stage("order-pool32-500", "order", "nginx", 500, "30s")
print("\n## Final capacity stage results\n")
print("| Stage | Target it/s | HTTP req/s | P95 ms | P99 ms | Business failures % | Dropped | Exit | Required |")
print("|---|---:|---:|---:|---:|---:|---:|---:|---|")
for row in rows:
    print("| " + " | ".join(row) + " |")
print("\nHigh-pressure failures are exploratory; required baseline/recovery/profile stages determine this job's result.")
print_process_report(results)
sys.exit(0 if required_ok else 1)
