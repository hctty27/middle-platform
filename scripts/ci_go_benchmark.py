#!/usr/bin/env python3
"""Informational Java/Go HTTP latency baselines against the same MySQL data."""
import concurrent.futures
import json
import pathlib
import statistics
import time
import urllib.request

import ci_go_compat as compat

def measure(port, endpoint, requests=120, concurrency=8):
    url = f"http://127.0.0.1:{port}{endpoint}"

    def get():
        start = time.perf_counter_ns()
        with urllib.request.urlopen(url, timeout=10) as response:
            if response.status != 200:
                raise RuntimeError(f"HTTP {response.status}: {url}")
            response.read()
        return (time.perf_counter_ns() - start) / 1_000_000

    for _ in range(8):
        get()
    before = time.perf_counter()
    with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
        latencies = list(pool.map(lambda _: get(), range(requests)))
    elapsed = time.perf_counter() - before
    latencies.sort()
    return {
        "count": requests, "concurrency": concurrency, "qps": round(requests / elapsed, 1),
        "p50_ms": round(statistics.median(latencies), 3),
        "p95_ms": round(latencies[max(0, int(len(latencies) * 0.95) - 1)], 3),
        "p99_ms": round(latencies[max(0, int(len(latencies) * 0.99) - 1)], 3),
    }

def main():
    java = compat.start_java()
    compat.wait_ready(compat.JAVA_PORT, "/actuator/health", java, "Java performance")
    go = compat.start_go()
    compat.wait_ready(compat.GO_PORT, "/health/ready", go, "Go performance")
    cases = {
        "revision": "/api/config/sync/revision",
        "node": "/api/config/nodes/105",
        "tree": "/api/config/admin/nodes/tree?parentId=0",
        "children": "/api/config/admin/nodes/103/children?page=1&size=20",
    }
    results = {}
    for case, path in cases.items():
        results[case] = {}
        for concurrency in (1, 8):
            for name, port in (("java", compat.JAVA_PORT), ("go", compat.GO_PORT)):
                metric = measure(port, path, concurrency=concurrency)
                results[case][f"{name}_c{concurrency}"] = metric
                print(f"PERF {case:9s} {name:4s} c={concurrency}: "
                      f"p50={metric['p50_ms']}ms p95={metric['p95_ms']}ms "
                      f"p99={metric['p99_ms']}ms qps={metric['qps']}", flush=True)
    dest = pathlib.Path("/tmp/config-go-benchmark.json")
    dest.write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding="utf-8")
    print(f"PASS baseline finished; results saved to {dest}", flush=True)
    print("INFO small shared-host CI results are not production throughput claims", flush=True)

if __name__ == "__main__":
    try:
        main()
    finally:
        for proc, log in reversed(compat.PROCESSES):
            if proc.poll() is None:
                proc.terminate()
                try:
                    proc.wait(timeout=12)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait(timeout=5)
            log.close()
