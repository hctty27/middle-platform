#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
    printf 'Usage: check-http-live.sh <supervisor-program> <port>\n' >&2
    exit 2
fi

program=$1
port=$2

if [[ ! "$program" =~ ^(config-center(-access)?|sdk-core|sdk-service)(-[1-9][0-9]*)?$ ]]; then
    printf 'Invalid Supervisor program name\n' >&2
    exit 2
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

status=$("$supervisorctl_bin" status "$program" 2>/dev/null) || exit 0
if [[ "$status" != "$program RUNNING "* ]]; then
    exit 0
fi

exec "$curl_bin" --fail --silent --show-error --connect-timeout 2 --max-time 3 \
    "http://127.0.0.1:$port_number/health/live" >/dev/null
