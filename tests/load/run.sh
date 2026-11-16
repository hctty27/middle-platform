#!/usr/bin/env bash
set -Eeuo pipefail
: "${LOAD_DIR:?}"
[[ "${GITHUB_ACTIONS:-}" == true && "${RUNNER_ENVIRONMENT:-}" == github-hosted ]]
case "$LOAD_PROFILE" in
  smoke) rates=(5); duration=10s ;;
  baseline) rates=(25 50 100); duration=30s ;;
  stress) rates=(100 250 500 1000); duration=30s ;;
  soak) rates=(50); duration=30m ;;
  soak2h) rates=(50); duration=2h ;;
  *) exit 2 ;;
esac
if [[ ( "$LOAD_PROFILE" == soak || "$LOAD_PROFILE" == soak2h ) && "$LOAD_SCENARIO" == all ]]; then LOAD_SCENARIO=mixed; fi
requested_scenario="$LOAD_SCENARIO"
case "$LOAD_SCENARIO" in
  all) scenarios=(config-sync sdk-config user-check order duplicate payment mixed) ;;
  config-sync|sdk-config|user-check|order|duplicate|payment|mixed) scenarios=("$LOAD_SCENARIO") ;;
  *) exit 2 ;;
esac
vmstat -w 2 >> "$LOAD_DIR/results/vmstat.txt" &
vmstat_pid=$!
(
  while true; do
    date -u +%FT%TZ
    ps -eo pid,comm,pcpu,rss,nlwp --sort=-rss | head -n 30 || true
    sleep 2
  done
) >> "$LOAD_DIR/results/processes.txt" &
ps_pid=$!
trap 'kill "$vmstat_pid" "$ps_pid" 2>/dev/null || true' EXIT
if [[ ! -f "$LOAD_DIR/results/stages.tsv" ]]; then
  printf 'scenario\titeration_rate\tduration\texit_code\n' > "$LOAD_DIR/results/stages.tsv"
fi
snapshot() {
  local path="$1"
  mysql -uroot -h127.0.0.1 -N -B -e "SHOW GLOBAL STATUS WHERE Variable_name IN ('Innodb_row_lock_waits','Innodb_row_lock_time','Innodb_log_waits','Innodb_os_log_fsyncs','Innodb_data_fsyncs','Innodb_buffer_pool_reads','Com_commit','Com_insert','Com_select','Com_stmt_execute','Com_stmt_prepare','Com_stmt_close','Slow_queries');" > "$path.status.tsv" 2>"$path.status.err" || true
  mysql -uroot -h127.0.0.1 -N -B -e "SELECT DIGEST, COUNT_STAR, SUM_TIMER_WAIT, SUM_LOCK_TIME, LEFT(DIGEST_TEXT,240) FROM performance_schema.events_statements_summary_by_digest WHERE (SCHEMA_NAME='config_center' OR DIGEST_TEXT LIKE '%game_order%') AND DIGEST IS NOT NULL;" > "$path.digest.tsv" 2>"$path.digest.err" || true
}
# Separate statement-type totals can expose prepared execution even when digest attribution is incomplete.
statement_snapshot() {
  mysql -uroot -h127.0.0.1 -N -B -e "SELECT EVENT_NAME,COUNT_STAR,SUM_TIMER_WAIT FROM performance_schema.events_statements_summary_global_by_event_name WHERE EVENT_NAME LIKE 'statement/sql/%' OR EVENT_NAME LIKE 'statement/com/%';" > "$1.statement.tsv" 2>"$1.statement.err" || true
}
failed=0
export LOAD_DURATION="$duration"
for scenario in "${scenarios[@]}"; do
  stage_rates=("${rates[@]}")
  if [[ "$LOAD_PROFILE" == stress && "$scenario" == sdk-config ]]; then stage_rates=(100 250 500 1000 2000 3000 5000); fi
  for rate in "${stage_rates[@]}"; do
    export LOAD_SCENARIO="$scenario" LOAD_RATE="$rate" LOAD_RUN_ID="$scenario-$rate"
    label="$scenario-$rate"
    snapshot "$LOAD_DIR/results/$label.before"
    statement_snapshot "$LOAD_DIR/results/$label.before"
    vmstat -w 1 > "$LOAD_DIR/results/$label.vmstat" &
    stage_vm=$!
    iostat -dx 1 > "$LOAD_DIR/results/$label.iostat" &
    stage_io=$!
    python3 tests/load/process_monitor.py "$LOAD_DIR/results/$label.process.tsv" &
    stage_proc=$!
    code=0
    k6 run --summary-export "$LOAD_DIR/results/$label.json" tests/load/scenarios.js 2>&1 | tee "$LOAD_DIR/results/$label.txt" || code=$?
    kill "$stage_vm" "$stage_io" "$stage_proc" 2>/dev/null || true
    wait "$stage_vm" "$stage_io" "$stage_proc" 2>/dev/null || true
    snapshot "$LOAD_DIR/results/$label.after"
    statement_snapshot "$LOAD_DIR/results/$label.after"
    printf '%s\t%s\t%s\t%s\n' "$scenario" "$rate" "$duration" "$code" >> "$LOAD_DIR/results/stages.tsv"
    if [[ "$scenario" == payment || "$scenario" == mixed ]]; then
      # Drain this stage before increasing pressure; use unique fixture order IDs.
      stage_pending=0
      for attempt in $(seq 1 60); do
        stage_pending=$(mysql -uroot -h127.0.0.1 -N config_center -e "SELECT COUNT(*) FROM game_order WHERE game_order_id LIKE '$label-%' AND payment_status=1 AND notify_status<>1;")
        if (( stage_pending == 0 )); then break; fi
        sleep 2
      done
      mysql -uroot -h127.0.0.1 -N config_center -e "SELECT COUNT(IF(payment_status=1,1,NULL)),COUNT(IF(payment_status=1 AND notify_status=1,1,NULL)),COUNT(IF(payment_status=1 AND notify_status<>1,1,NULL)) FROM game_order WHERE game_order_id LIKE '$label-%';" | tee "$LOAD_DIR/results/$label.delivery.tsv"
      if (( stage_pending != 0 )); then
        echo "::warning::Delivery backlog remains after 120 seconds for $label"
        failed=1
        break
      fi
    fi
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
printf 'paid\tdelivered\tpending\n%s\t%s\t%s\n' "$paid" "$delivered" "$pending" | tee "$LOAD_DIR/results/delivery.tsv"
if (( pending != 0 )); then failed=1; fi
if [[ "$requested_scenario" == all || "$requested_scenario" == payment || "$requested_scenario" == mixed ]] && (( paid == 0 )); then failed=1; fi
exit "$failed"
