#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 || ! "$1" =~ ^(config-center-[12]|sdk-core-[1-5]|sdk-service-[1-5])$ ]]; then
    printf 'Usage: restart-supervisor-program.sh <supervisor-program>\n' >&2
    exit 2
fi

lock_file=${PUBLISH_LOCK_FILE:-/data/middle-platform/.publish.lock}
if [[ -L "$lock_file" ]]; then
    printf 'Refusing symlinked publish lock: %s\n' "$lock_file" >&2
    exit 1
fi
if [[ -f "$lock_file" ]]; then
    command -v flock >/dev/null || { printf 'flock is required for coordinated deployment restarts\n' >&2; exit 1; }
    exec 9>"$lock_file"
    if ! flock -n 9; then
        printf 'Skipping Monit restart because a deployment is in progress\n'
        exit 0
    fi
fi

supervisorctl_bin=${SUPERVISORCTL:-$(command -v supervisorctl || true)}
if [[ -z "$supervisorctl_bin" || ! -x "$supervisorctl_bin" ]]; then
    printf 'supervisorctl is unavailable\n' >&2
    exit 1
fi

exec "$supervisorctl_bin" restart "$1"
