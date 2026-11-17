#!/usr/bin/env bash
set -Eeuo pipefail
: "${LOAD_DIR:?LOAD_DIR required}"
: "${LOAD_TOPOLOGY:?LOAD_TOPOLOGY required}"
# Only disposable GitHub-hosted environments are supported.
[[ "${GITHUB_ACTIONS:-}" == true && "${RUNNER_ENVIRONMENT:-}" == github-hosted ]]
mkdir -p "$LOAD_DIR"/{bin,config,logs,results}
export GOWORK=off
for svc in config-center sdk-core sdk-service; do
  (cd "$svc" && go build -trimpath -o "$LOAD_DIR/bin/$svc" "./cmd/$svc")
  cp "$svc/configs/config.yaml" "$LOAD_DIR/config/$svc.yaml"
done
go build -trimpath -o "$LOAD_DIR/bin/mock" tests/load/mock.go
mysql -uroot -h127.0.0.1 -e "CREATE DATABASE config_center CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;"
mysql -uroot -h127.0.0.1 config_center < db/full/config_center.sql
mysql -uroot -h127.0.0.1 config_center < tests/load/seed.sql
export CONFIG_DB_DSN='root:runner-load-only@tcp(127.0.0.1:3306)/config_center?parseTime=true&loc=UTC'
export SDK_CORE_DB_DSN="$CONFIG_DB_DSN" SDK_SERVICE_DB_DSN="$CONFIG_DB_DSN"
export ADMIN_AUTH_SECRET='runner-load-admin-only-0123456789abcdef'
# Keep service defaults; only point the platform URL at our local mock.
sed -i 's|userCheckURL: ""|userCheckURL: "http://127.0.0.1:8290/cp/user/check"|' "$LOAD_DIR/config/sdk-service.yaml"
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
