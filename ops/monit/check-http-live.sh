#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
    printf 'Usage: check-http-live.sh <supervisor-program> <port>\n' >&2
    exit 2
fi

program=$1
port=$2
case "$program" in
    config-center-[12]) supervisor_process="config-center:$program" ;;
    sdk-core-[1-2]) supervisor_process="sdk-core:$program" ;;
    sdk-service-[1-4]) supervisor_process="sdk-service:$program" ;;
    config-center-access) supervisor_process=$program ;;
    *) printf 'Invalid Supervisor program name\n' >&2; exit 2 ;;
esac

lock_file=${PUBLISH_LOCK_FILE:-/data/middle-platform/.publish.lock}
if [[ -L "$lock_file" ]]; then
    printf 'Refusing symlinked publish lock: %s\n' "$lock_file" >&2
    exit 2
fi
if [[ -f "$lock_file" ]]; then
    command -v flock >/dev/null || { printf 'flock is required for coordinated deployment checks\n' >&2; exit 2; }
    exec 9>"$lock_file"
    if ! flock -n 9; then
        exit 0
    fi
fi

if [[ ! "$port" =~ ^[0-9]{1,5}$ ]]; then
    printf 'Invalid health-check port\n' >&2
    exit 2
fi
port_number=$((10#$port))
if (( port_number < 1 || port_number > 65535 )); then
    printf 'Health-check port out of range\n' >&2
    exit 2
fi

supervisorctl_bin=$(printenv SUPERVISORCTL || true)
if [[ -z "$supervisorctl_bin" ]]; then
    supervisorctl_bin=$(command -v supervisorctl || true)
fi
curl_bin=$(printenv CURL || true)
if [[ -z "$curl_bin" ]]; then
    curl_bin=$(command -v curl || true)
fi
if [[ -z "$supervisorctl_bin" || ! -x "$supervisorctl_bin" || -z "$curl_bin" || ! -x "$curl_bin" ]]; then
    printf 'supervisorctl or curl is unavailable; liveness check skipped\n' >&2
    exit 0
fi

status=$("$supervisorctl_bin" status "$supervisor_process" 2>/dev/null) || exit 0
if [[ "$status" != "$supervisor_process RUNNING "* ]]; then
    exit 0
fi

exec "$curl_bin" --fail --silent --show-error --connect-timeout 2 --max-time 3 \
    "http://127.0.0.1:$port_number/health/live" >/dev/null
