"""Report per-stage MySQL and CPU diagnostics from the local runner."""
import csv


def rows(path):
    try:
        with path.open() as file:
            return list(csv.reader(file, delimiter="\t"))
    except OSError:
        return []


def status(path):
    return {r[0]: int(r[1]) for r in rows(path) if len(r) == 2 and r[1].isdigit()}


def digests(path):
    return {r[0]: (int(r[1]), int(r[2]), int(r[3]), r[4])
            for r in rows(path) if len(r) == 5 and all(v.isdigit() for v in r[1:4])}


def cpu(path):
    try:
        lines = path.read_text().splitlines()
    except OSError:
        return []
    samples = []
    for line in lines:
        vals = line.split()
        if len(vals) >= 17 and all(v.isdigit() for v in vals[:17]):
            samples.append((int(vals[12]) + int(vals[13]), int(vals[15])))
    return samples[1:]  # First vmstat sample is since boot.


def print_diagnostics(root):
    print("\n### Per-stage database and host diagnostics\n")
    print("Counters are global, including background activity; missing values are not zero.\n")
    print("| Stage | CPU busy % | IO wait % | Row lock waits delta | Row lock ms delta | Log waits delta | Data fsync delta | Prepared executions delta |")
    print("|---|---:|---:|---:|---:|---:|---:|---:|")
    stages = rows(root / "stages.tsv")[1:]
    for scenario, rate, *_ in stages:
        label = f"{scenario}-{rate}"
        before = status(root / (label + ".before.status.tsv"))
        after = status(root / (label + ".after.status.tsv"))
        samples = cpu(root / (label + ".vmstat"))

        def delta(key):
            if key not in before or key not in after or after[key] < before[key]:
                return "—"
            return str(after[key] - before[key])

        busy = f"{sum(s[0] for s in samples) / len(samples):.1f}" if samples else "—"
        wait = f"{sum(s[1] for s in samples) / len(samples):.1f}" if samples else "—"
        print("| " + " | ".join([label, busy, wait, delta("Innodb_row_lock_waits"),
              delta("Innodb_row_lock_time"), delta("Innodb_log_waits"),
              delta("Innodb_data_fsyncs"), delta("Com_stmt_execute")]) + " |")
    print("\n### SQL digest deltas for order-related scenarios\n")
    print("Elapsed time is aggregate and can overlap across connections; top 5 per stage.")
    for scenario, rate, *_ in stages:
        if scenario not in {"order", "duplicate", "payment", "mixed"}:
            continue
        label = f"{scenario}-{rate}"
        prior_path = root / (label + ".before.digest.tsv")
        later_path = root / (label + ".after.digest.tsv")
        prior = digests(prior_path)
        later = digests(later_path)
        ranked = []
        for key, (count, elapsed, lock, sql) in later.items():
            old_count, old_elapsed, old_lock, _ = prior.get(key, (0, 0, 0, ""))
            n, t, l = count - old_count, elapsed - old_elapsed, lock - old_lock
            if n > 0 and t >= 0 and l >= 0:
                ranked.append((t, n, l, sql))
        print(f"\n**{label}**\n")
        if not ranked or not prior_path.exists() or not later_path.exists():
            print("No SQL digest delta available.")
            continue
        print("| Calls | Total ms | Mean ms | Lock ms | SQL digest |")
        print("|---:|---:|---:|---:|---|")
        for t, n, l, sql in sorted(ranked, reverse=True)[:5]:
            sql = sql.replace("|", " ")
            print(f"| {n} | {t/1e9:.2f} | {t/n/1e9:.3f} | {l/1e9:.2f} | {sql} |")
