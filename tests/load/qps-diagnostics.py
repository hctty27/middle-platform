"""Separate k6/HTTP scheduling from HMAC replay-cache limits on a hosted Runner."""
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
    raise SystemExit("Diagnostic tests require a disposable GitHub-hosted runner")
root = Path(sys.argv[1]).resolve()
results = root / "results"
results.mkdir(parents=True, exist_ok=True)

def restart(program, port):
    subprocess.run(["supervisorctl", "-c", str(root/"supervisor.conf"), "restart", program],
                   check=True, timeout=45, stdout=subprocess.DEVNULL)
    url = f"http://127.0.0.1:{port}/health/ready"
    for attempt in range(35):
        try:
            with urlopen(url, timeout=2) as response:
                if response.status == 200:
                    return
        except OSError:
            pass
        time.sleep(0.2)
    raise RuntimeError("Isolated diagnostic service did not restart: " + program)

def metric(data, name, key):
    raw = data.get("metrics", {}).get(name, {})
    return raw.get("values", raw).get(key, 0) or 0

# Same-runner no-auth controls explicitly distinguish the load generator / HTTP
# stack from app routes. Signed direct-to-service cases use freshly restarted
# nonce caches; observing ~10k successful auth requests and 409s thereafter
# tests the documented finite replay-window behavior without bypassing auth.
stages = [
    ("mock", 10000, 3, "", 0),
    ("mock", 20000, 3, "", 0),
    ("health", 10000, 3, "", 0),
    ("health", 20000, 3, "", 0),
    ("admin-core", 3000, 5, "sdk-core-1", 8211),
    ("admin-config", 3000, 5, "config-center-1", 8201),
    ("subscriber-access", 3000, 5, "config-center-access", 8081),
    ("subscriber-sync", 3000, 5, "config-center-1", 8201),
]
rows = []
print("\n## HMAC nonce vs. generator diagnostics (all on 127.0.0.1)\n", flush=True)
print("| Probe | Target QPS | Actual QPS | Good 2xx | HTTP 409 | Other HTTP | Dropped | P95 ms | P99 ms |", flush=True)
print("|---|---:|---:|---:|---:|---:|---:|---:|---:|", flush=True)
for i, (mode, rate, seconds, program, port) in enumerate(stages):
    if program:
        restart(program, port)
    prefix = results / f"qps-diagnostic-{i:02d}-{mode}-{rate}"
    vus = min(800, max(100, math.ceil(rate * 0.05)))
    env = dict(os.environ, DIAG_CASE=mode, DIAG_RATE=str(rate),
               DIAG_SECONDS=str(seconds), DIAG_VUS=str(vus))
    sampler = subprocess.Popen([sys.executable, "tests/load/process_monitor.py",
                                str(prefix)+".process.tsv"])
    try:
        with open(str(prefix)+".log","w") as output:
            proc = subprocess.run(
                ["k6", "run", "--summary-export", str(prefix)+".json",
                 "tests/load/qps-diagnostics.js"],
                env=env, stdout=output, stderr=subprocess.STDOUT,
                timeout=seconds+60)
    finally:
        sampler.terminate()
        sampler.wait(timeout=10)
    output_file = Path(str(prefix)+".json")
    if not output_file.exists():
        print("\n".join(Path(str(prefix)+".log").read_text().splitlines()[-20:]), flush=True)
        raise SystemExit("No k6 diagnostic metrics: " + mode)
    data = json.loads(output_file.read_text())
    success = int(metric(data, "diag_success_2xx", "count"))
    conflicts = int(metric(data, "diag_http_409", "count"))
    other = int(metric(data, "diag_unexpected_http", "count"))
    actual = float(metric(data, "http_reqs", "rate"))
    dropped = int(metric(data, "dropped_iterations", "count"))
    p95 = float(metric(data, "http_req_duration", "p(95)"))
    p99 = float(metric(data, "http_req_duration", "p(99)"))
    row = dict(probe=mode, target_qps=rate, seconds=seconds, vus=vus,
               actual_qps=actual, http_2xx=success, http_409=conflicts,
               other_http=other, dropped=dropped, p95_ms=p95, p99_ms=p99,
               runner_exit=proc.returncode)
    rows.append(row)
    print(f"| {mode} | {rate} | {actual:.1f} | {success} | {conflicts} | "
          f"{other} | {dropped} | {p95:.2f} | {p99:.2f} |", flush=True)
    if proc.returncode != 0:
        # No thresholds are set for expected HMAC conflicts. A nonzero exit is
        # a broken diagnostic experiment, not a demonstrated capacity bound.
        print("\n".join(Path(str(prefix)+".log").read_text().splitlines()[-20:]), flush=True)
        raise SystemExit("Broken diagnostic probe: " + mode)
(results/"qps-diagnostics-summary.json").write_text(json.dumps({
    "environment":"github-hosted-single-VM",
    "notes":[
        "No HMAC on mock and health controls; signed probes use valid fresh nonces",
        "409 can indicate replay cache exhaustion; verify with unique nonce count and code paths",
        "Dropped iterations alone cannot prove whether k6 or the service is the limit",
        "The signed probes target a single isolated instance and cannot represent cluster capacity",
        "No production host or database accessed"
    ], "stages":rows}, indent=2))
with (results/"qps-diagnostics-summary.csv").open("w",newline="") as output:
    writer = csv.DictWriter(output, fieldnames=list(rows[0]))
    writer.writeheader()
    writer.writerows(rows)
print("\n**Interpretation:** Compare 10k/20k unsigned generator probes with "
      "the signed 3000/s x 5s probes. About 10k successful signed requests followed "
      "by 409s points to a finite per-process nonce cache. Both controls and "
      "SUT share the same VM; do not infer production capacity from either.\n",flush=True)
print_process_report(results)
