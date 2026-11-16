#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

cat > "$tmp/supervisorctl" <<'SH'
#!/usr/bin/env bash
if [[ "$1" != status ]]; then
    exit 2
fi
state=$TEST_SUPERVISOR_STATE
if [[ -z "$state" ]]; then
    state=RUNNING
fi
printf '%s %s pid 123\n' "$2" "$state"
SH

cat > "$tmp/curl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$CURL_LOG"
exit "$TEST_CURL_EXIT"
SH

chmod 0755 "$tmp/supervisorctl" "$tmp/curl"
export SUPERVISORCTL="$tmp/supervisorctl"
export CURL="$tmp/curl"
export CURL_LOG="$tmp/curl.log"
TEST_CURL_EXIT=0
export TEST_CURL_EXIT

TEST_SUPERVISOR_STATE=STARTING "$script_dir/check-http-live.sh" sdk-core-1 8211
test ! -s "$CURL_LOG"

TEST_SUPERVISOR_STATE=RUNNING "$script_dir/check-http-live.sh" sdk-core-1 8211
test -s "$CURL_LOG"

# A live deployment holds the shared lock; Monit must skip its check.
exec 8>"$tmp/publish.lock"
flock -n 8
before_lines=$(wc -l < "$CURL_LOG")
PUBLISH_LOCK_FILE="$tmp/publish.lock" TEST_SUPERVISOR_STATE=RUNNING "$script_dir/check-http-live.sh" sdk-core-1 8211
after_lines=$(wc -l < "$CURL_LOG")
test "$before_lines" -eq "$after_lines"
exec 8>&-

TEST_CURL_EXIT=7
export TEST_CURL_EXIT
if TEST_SUPERVISOR_STATE=RUNNING "$script_dir/check-http-live.sh" sdk-core-1 8211; then
    printf 'Expected a failed liveness check\n' >&2
    exit 1
fi

if "$script_dir/check-http-live.sh" invalid-program 8211; then
    printf 'Expected invalid program to be rejected\n' >&2
    exit 1
fi

printf 'Monit liveness check tests passed\n'
