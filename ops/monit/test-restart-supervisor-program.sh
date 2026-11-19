#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT

cat > "$tmp/supervisorctl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$SUPERVISOR_LOG"
[[ "$1" == restart ]]
SH
chmod 0755 "$tmp/supervisorctl"

export SUPERVISORCTL="$tmp/supervisorctl"
export SUPERVISOR_LOG="$tmp/supervisor.log"

"$script_dir/restart-supervisor-program.sh" config-center-1
"$script_dir/restart-supervisor-program.sh" sdk-core-2
"$script_dir/restart-supervisor-program.sh" sdk-service-4
"$script_dir/restart-supervisor-program.sh" sdk-service-3
"$script_dir/restart-supervisor-program.sh" config-center-access

grep -Fxq 'restart config-center:config-center-1' "$SUPERVISOR_LOG"
grep -Fxq 'restart sdk-core:sdk-core-2' "$SUPERVISOR_LOG"
grep -Fxq 'restart sdk-service:sdk-service-4' "$SUPERVISOR_LOG"
grep -Fxq 'restart sdk-service:sdk-service-3' "$SUPERVISOR_LOG"
grep -Fxq 'restart config-center-access' "$SUPERVISOR_LOG"

if "$script_dir/restart-supervisor-program.sh" sdk-core-3 >/dev/null 2>&1; then
    printf 'Expected removed third SDK Core instance to be rejected\n' >&2
    exit 1
fi

touch "$tmp/publish.lock"
exec 8>"$tmp/publish.lock"
flock -n 8
before=$(wc -l < "$SUPERVISOR_LOG")
PUBLISH_LOCK_FILE="$tmp/publish.lock" "$script_dir/restart-supervisor-program.sh" sdk-core-1
after=$(wc -l < "$SUPERVISOR_LOG")
test "$before" -eq "$after"
exec 8>&-

printf 'Monit Supervisor group restart tests passed\n'
