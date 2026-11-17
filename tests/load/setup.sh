#!/usr/bin/env bash
set -Eeuo pipefail
: "${LOAD_DIR:?LOAD_DIR required}"
: "${LOAD_TOPOLOGY:?LOAD_TOPOLOGY required}"
# Only disposable GitHub-hosted environments are supported.
[[ "${GITHUB_ACTIONS:-}" == true && "${RUNNER_ENVIRONMENT:-}" == github-hosted ]]
mkdir -p "$LOAD_DIR"/{bin,config,logs,results}
export GOWORK=off
build_flags=()
if [[ "$LOAD_PROFILE" == capacity || "$LOAD_PROFILE" == traffic-multi ]]; then build_flags=(-tags loadtest); fi
for svc in config-center sdk-core sdk-service; do
  (cd "$svc" && go build "${build_flags[@]}" -trimpath -o "$LOAD_DIR/bin/$svc" "./cmd/$svc")
  cp "$svc/configs/config.yaml" "$LOAD_DIR/config/$svc.yaml"
done
go build -trimpath -o "$LOAD_DIR/bin/mock" tests/load/mock.go
mysql -uroot -h127.0.0.1 -e "CREATE DATABASE config_center CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;"
mysql -uroot -h127.0.0.1 config_center < db/full/config_center.sql
mysql -uroot -h127.0.0.1 config_center < tests/load/seed.sql
# Fail during setup if either required service is not explicitly enabled.
enabled=$(mysql -uroot -h127.0.0.1 -N config_center -e "SELECT COUNT(*) FROM sdk_service s JOIN sdk_service_channel ch ON ch.service_id=s.id JOIN sdk_channel_app a ON a.id=ch.channel_app_id WHERE a.code='load-app' AND a.status=1 AND s.code IN ('user_check','event_report') AND s.enabled=1 AND ch.enabled=1 AND ch.env='test';")
[[ "$enabled" == 2 ]] || { echo "::error::Missing enabled test service bindings"; exit 1; }
if [[ "$LOAD_PROFILE" == traffic-multi || "$LOAD_PROFILE" == traffic-quick ]]; then
  python3 tests/load/multi_seed.py | mysql -uroot -h127.0.0.1 config_center
  count=$(mysql -uroot -h127.0.0.1 -N config_center -e "SELECT COUNT(*) FROM sdk_service_channel ch JOIN sdk_service s ON s.id=ch.service_id JOIN sdk_channel_app a ON a.id=ch.channel_app_id WHERE a.id BETWEEN 1000 AND 1255 AND a.status=1 AND ch.enabled=1 AND ch.env='test' AND s.enabled=1 AND s.code IN ('user_check','event_report');")
  [[ "$count" == 512 ]] || { echo "::error::Missing multi-channel fixtures"; exit 1; }
fi
export CONFIG_DB_DSN='root:runner-load-only@tcp(127.0.0.1:3306)/config_center?parseTime=true&loc=UTC'
export SDK_CORE_DB_DSN="$CONFIG_DB_DSN" SDK_SERVICE_DB_DSN="$CONFIG_DB_DSN"
export ADMIN_AUTH_SECRET='runner-load-admin-only-0123456789abcdef'
# Keep service defaults; only point the platform URL at our local mock.
sed -i 's|userCheckURL: ""|userCheckURL: "http://127.0.0.1:8290/cp/user/check"|' "$LOAD_DIR/config/sdk-service.yaml"
sed -i 's|eventReportURL: ""|eventReportURL: "http://127.0.0.1:8290/report/event"|' "$LOAD_DIR/config/sdk-service.yaml"
sed -i 's|subscribeMessageURL: ""|subscribeMessageURL: "http://127.0.0.1:8290/game/subscribe/send-message"|' "$LOAD_DIR/config/sdk-service.yaml"
python3 tests/load/topology.py "$LOAD_DIR" "$LOAD_TOPOLOGY"
# Supervisor inherits the disposable local DB settings above.
supervisord -c "$LOAD_DIR/supervisor.conf"
nginx -p "$LOAD_DIR" -e "$LOAD_DIR/logs/nginx-error.log" -t -c "$LOAD_DIR/nginx.conf"
nginx -p "$LOAD_DIR" -e "$LOAD_DIR/logs/nginx-error.log" -c "$LOAD_DIR/nginx.conf"
while read -r port; do
  curl --noproxy '*' --retry 30 --retry-connrefused --retry-delay 1 --max-time 5 -fsS "http://127.0.0.1:$port/health/ready" >/dev/null
done < "$LOAD_DIR/ports.txt"
supervisorctl -c "$LOAD_DIR/supervisor.conf" status
lscpu > "$LOAD_DIR/results/lscpu.txt"
free -m > "$LOAD_DIR/results/memory.txt"
git rev-parse HEAD > "$LOAD_DIR/results/revision.txt"
printf '%s\n' "$LOAD_TOPOLOGY" > "$LOAD_DIR/results/topology.txt"
