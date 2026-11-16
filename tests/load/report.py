"""Produce a Markdown Actions summary from k6 JSON, including failed stages."""
import csv
import json
import pathlib
import sys

from db_report import print_diagnostics

root = pathlib.Path(sys.argv[1])
print("## Runner-local load test\n")
for name in ("revision", "topology"):
    path = root / (name + ".txt")
    if path.exists():
        print(f"- {name}: {path.read_text().strip()}")
print("\nThe application, database, mock and load generator share the same runner. "
      "These results are not production capacity measurements.\n")
stages = root / "stages.tsv"
if not stages.exists():
    print("No completed stage: inspect setup logs.")
    sys.exit(0)

def metric(data, key, field):
    item = data.get("metrics", {}).get(key, {})
    return item.get("values", item).get(field)

def number(v, digits=2):
    return "—" if v is None else f"{v:.{digits}f}"

print("| Scenario | Target iterations/s | Actual HTTP req/s | P50 ms | P95 ms | P99 ms | Business failures % | Dropped | Exit |")
print("|---|---:|---:|---:|---:|---:|---:|---:|---:|")
with stages.open() as file:
    for row in csv.DictReader(file, delimiter="\t"):
        path = root / (row["scenario"] + "-" + row["iteration_rate"] + ".json")
        data = json.loads(path.read_text()) if path.exists() else {}
        failure = metric(data, "business_failed", "rate")
        if failure is None:
            failure = metric(data, "business_failed", "value")
        print("| " + " | ".join([
            row["scenario"], row["iteration_rate"],
            number(metric(data, "http_reqs", "rate")),
            number(metric(data, "http_req_duration", "med")),
            number(metric(data, "http_req_duration", "p(95)")),
            number(metric(data, "http_req_duration", "p(99)")),
            number(None if failure is None else failure * 100),
            number(metric(data, "dropped_iterations", "count"), 0),
            row["exit_code"],
        ]) + " |")
print_diagnostics(root)
delivery = root / "delivery.tsv"
if delivery.exists():
    print("\n### Asynchronous order delivery\n")
    with delivery.open() as f:
        for row in csv.DictReader(f, delimiter="\t"):
            print(f"Paid: {row['paid']}; delivered: {row['delivered']}; pending: {row['pending']}.")
