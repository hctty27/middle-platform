#!/usr/bin/env bash
set -Eeuo pipefail
: "${LOAD_DIR:?}"
[[ "${GITHUB_ACTIONS:-}" == true && "${RUNNER_ENVIRONMENT:-}" == github-hosted ]]
case "$LOAD_PROFILE" in
  smoke) rates=(5); duration=10s ;;
  baseline) rates=(25 50 100); duration=30s ;;
  stress) rates=(50 100 200 300); duration=30s ;;
  *) exit 2 ;;
esac
requested_scenario="$LOAD_SCENARIO"
case "$LOAD_SCENARIO" in
  all) scenarios=(config-sync sdk-config user-check order duplicate payment mixed) ;;
  config-sync|sdk-config|user-check|order|duplicate|payment|mixed) scenarios=("$LOAD_SCENARIO") ;;
  *) exit 2 ;;
esac
vmstat -w 2 > "$LOAD_DIR/results/vmstat.txt" &
vmstat_pid=$!
(
  while true; do
    date -u +%FT%TZ
    ps -eo pid,comm,pcpu,rss,nlwp --sort=-rss | head -n 30 || true
    sleep 2
  done
) > "$LOAD_DIR/results/processes.txt" &
ps_pid=$!
trap 'kill "$vmstat_pid" "$ps_pid" 2>/dev/null || true' EXIT
printf 'scenario\titeration_rate\tduration\texit_code\n' > "$LOAD_DIR/results/stages.tsv"
failed=0
export LOAD_DURATION="$duration"
for scenario in "${scenarios[@]}"; do
  for rate in "${rates[@]}"; do
    export LOAD_SCENARIO="$scenario" LOAD_RATE="$rate" LOAD_RUN_ID="$scenario-$rate"
    label="$scenario-$rate"
    code=0
    k6 run --summary-export "$LOAD_DIR/results/$label.json" tests/load/scenarios.js 2>&1 | tee "$LOAD_DIR/results/$label.txt" || code=$?
    printf '%s\t%s\t%s\t%s\n' "$scenario" "$rate" "$duration" "$code" >> "$LOAD_DIR/results/stages.tsv"
    if (( code != 0 )); then
      failed=1
      break
    fi
  done
done
# Delivery is asynchronous. Check the durable business outcome, not just HTTP ACK.
# Duplicate register requests use the same order ID; the unique index enforces one row.
for i in $(seq 1 60); do
  read -r paid delivered pending < <(mysql -uroot -h127.0.0.1 -N config_center -e "SELECT COUNT(IF(payment_status=1,1,NULL)),COUNT(IF(payment_status=1 AND notify_status=1,1,NULL)),COUNT(IF(payment_status=1 AND notify_status<>1,1,NULL)) FROM game_order;")
  if (( pending == 0 )); then break; fi
  sleep 2
done
printf 'paid\tdelivered\tpending\n%s\t%s\t%s\n' "$paid" "$delivered" "$pending" > "$LOAD_DIR/results/delivery.tsv"
if (( pending != 0 )); then failed=1; fi
if [[ "$requested_scenario" == all || "$requested_scenario" == payment || "$requested_scenario" == mixed ]] && (( paid == 0 )); then failed=1; fi
exit "$failed"
