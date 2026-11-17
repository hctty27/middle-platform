#!/usr/bin/env bash
set -Eeuo pipefail
: "$LOAD_DIR"
[[ "$GITHUB_ACTIONS" == true && "$RUNNER_ENVIRONMENT" == github-hosted ]]
[[ "$LOAD_PROFILE" == api-coverage ]] || { echo "api-coverage profile required"; exit 2; }

# The optional config-center-access replica has its own disposable database.
(cd config-center-access && GOWORK=off go build -trimpath -o "$LOAD_DIR/bin/config-center-access" ./cmd/config-center-access)
cp config-center-access/configs/config.yaml "$LOAD_DIR/config/config-center-access.yaml"
mysql -uroot -h127.0.0.1 -e \
  "CREATE DATABASE config_access CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;"
mysql -uroot -h127.0.0.1 config_access < db/full/config_access.sql

cat >> "$LOAD_DIR/supervisor.conf" <<'EOF'

[program:config-center-access]
command=__LOAD_DIR__/bin/config-center-access -config __LOAD_DIR__/config/config-center-access.yaml
directory=__LOAD_DIR__
autostart=true
autorestart=unexpected
startsecs=1
stopasgroup=true
killasgroup=true
redirect_stderr=true
stdout_logfile=__LOAD_DIR__/logs/config-center-access.log
environment=CONFIG_ACCESS_ADDR="127.0.0.1:8081",ACCESS_DB_DSN="root:runner-load-only@tcp(127.0.0.1:3306)/config_access?parseTime=true&loc=UTC",CONFIG_CENTER_URL="http://127.0.0.1:8201",CONFIG_SUBSCRIBER_CODE="load-subscriber",CONFIG_SUBSCRIBER_SECRET="runner-load-subscriber-only-0123456789"
EOF
sed -i "s|__LOAD_DIR__|$LOAD_DIR|g" "$LOAD_DIR/supervisor.conf"
supervisorctl -c "$LOAD_DIR/supervisor.conf" reread
supervisorctl -c "$LOAD_DIR/supervisor.conf" update
curl --noproxy '*' --retry 30 --retry-connrefused --retry-delay 1 \
  --max-time 5 -fsS 'http://127.0.0.1:8081/health/ready' >/dev/null
supervisorctl -c "$LOAD_DIR/supervisor.conf" status config-center-access
