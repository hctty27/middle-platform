"""Sample Linux /proc CPU ticks and RSS, without instrumenting production services."""
import csv
import os
import pathlib
import sys
import time
from collections import defaultdict
from statistics import mean

ROLES = {"config-center", "sdk-core", "sdk-service", "mysqld", "nginx", "k6", "mock", "supervisord"}
HZ = os.sysconf("SC_CLK_TCK")
PAGE = os.sysconf("SC_PAGE_SIZE")


def snapshot():
    result = {}
    for path in pathlib.Path("/proc").iterdir():
        if not path.name.isdigit():
            continue
        try:
            raw = (path / "stat").read_text()
            end = raw.rindex(")")
            role = raw[raw.index("(") + 1:end]
            if role not in ROLES:
                continue
            fields = raw[end + 2:].split()
            key = (role, int(path.name), fields[19])  # start ticks distinguish reused PIDs
            result[key] = (int(fields[11]) + int(fields[12]), max(0, int(fields[21])) * PAGE / 1048576)
        except (OSError, ValueError, IndexError):
            continue
    return result


def monitor(target):
    with open(target, "w", buffering=1) as file:
        writer = csv.writer(file, delimiter="\t")
        writer.writerow(("elapsed_s", "role", "pid", "cpu_pct_one_core", "rss_mib"))
        start = last_time = time.monotonic()
        previous = snapshot()
        while True:
            time.sleep(2)
            now = time.monotonic()
            current = snapshot()
            elapsed = now - last_time
            for key, (ticks, rss) in current.items():
                prior = previous.get(key)
                usage = "" if prior is None else round(100 * (ticks - prior[0]) / HZ / elapsed, 2)
                writer.writerow((round(now-start, 2), key[0], key[1], usage, round(rss, 2)))
            previous, last_time = current, now


def print_process_report(root):
    print("\n### Process CPU and RSS by stage\n")
    print("CPU is percent of one logical core, summed per role; RSS sums processes and can double-count shared pages. First/last RSS are samples, not heap measurements.\n")
    print("| Stage | Process | Samples | CPU avg % | CPU peak % | RSS first MiB | RSS peak MiB | RSS last MiB |")
    print("|---|---|---:|---:|---:|---:|---:|---:|")
    for path in sorted(root.glob("*.process.tsv")):
        groups = defaultdict(lambda: defaultdict(lambda: [[], 0.0]))
        with path.open() as file:
            for row in csv.DictReader(file, delimiter="\t"):
                if not row.get("rss_mib"):
                    continue
                bucket = groups[row["role"]][float(row["elapsed_s"])]
                bucket[1] += float(row["rss_mib"])
                if row["cpu_pct_one_core"]:
                    bucket[0].append(float(row["cpu_pct_one_core"]))
        for role, series in sorted(groups.items()):
            ordered = [series[key] for key in sorted(series)]
            cpus = [sum(r[0]) for r in ordered if r[0]]
            rss = [r[1] for r in ordered]
            avg = f"{mean(cpus):.1f}" if cpus else "—"
            peak = f"{max(cpus):.1f}" if cpus else "—"
            print(f"| {path.name.removesuffix('.process.tsv')} | {role} | {len(rss)} | {avg} | {peak} | {rss[0]:.1f} | {max(rss):.1f} | {rss[-1]:.1f} |")


if __name__ == "__main__":
    monitor(sys.argv[1])
