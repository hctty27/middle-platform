#!/usr/bin/env bash
set -euo pipefail
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
root="$tmp/root"
bin="$tmp/bin"
mkdir -p "$root/run/nginx" "$bin"
cat > "$root/run/nginx/config-center-upstream.conf" <<'EOF'
server 127.0.0.1:8201 max_fails=1 fail_timeout=10s;
server 127.0.0.1:8202 max_fails=1 fail_timeout=10s;
EOF
cat > "$root/run/nginx/sdk-core-upstream.conf" <<'EOF'
server 127.0.0.1:8211 max_fails=1 fail_timeout=10s;
server 127.0.0.1:8212 max_fails=1 fail_timeout=10s;
EOF
cat > "$root/run/nginx/sdk-service-upstream.conf" <<'EOF'
server 127.0.0.1:8221 max_fails=1 fail_timeout=10s;
server 127.0.0.1:8222 max_fails=1 fail_timeout=10s;
server 127.0.0.1:8223 max_fails=1 fail_timeout=10s;
server 127.0.0.1:8224 max_fails=1 fail_timeout=10s;
EOF
cat > "$bin/sudo" <<'EOF'
#!/usr/bin/env bash
set -e
[[ "$1" == -n ]]
shift
exec "$@"
EOF
cat > "$bin/nginx" <<'EOF'
#!/usr/bin/env bash
set -e
case "$1" in
    -t) exit 0 ;;
    -T)
        for file in "$MIDDLE_PLATFORM_ROOT"/run/nginx/*.conf; do
            printf '# configuration file %s:\n' "$file"
            cat "$file"
        done
        printf 'upstream config_center {\nupstream sdk_core {\nupstream sdk_service {\n'
        printf 'proxy_pass http://config_center;\nproxy_pass http://sdk_core;\nproxy_pass http://sdk_service;\n'
        printf 'worker_shutdown_timeout 75s;\n'
        ;;
    *) exit 2 ;;
esac
EOF
cat > "$bin/systemctl" <<'EOF'
#!/usr/bin/env bash
set -e
case "$1" in
    is-active) exit 0 ;;
    show) printf 'MainPID=100\n' ;;
    reload)
        read -r generation < "$TEST_GENERATION"
        printf '%s\n' "$((generation + 1))" > "$TEST_GENERATION"
        printf '0\n' > "$TEST_POLLS"
        ;;
    *) exit 2 ;;
esac
EOF
cat > "$bin/ps" <<'EOF'
#!/usr/bin/env bash
set -e
read -r generation < "$TEST_GENERATION"
read -r polls < "$TEST_POLLS"
printf '%s\n' "$((polls + 1))" > "$TEST_POLLS"
# Include a lookalike command, even with the same PPID as the workers.
printf '999 100 awk /nginx: worker process is shutting down/\n'
mode=${TEST_PS_MODE:-normal}
start=${TEST_START_GENERATION:-0}
if [[ "$mode" == fail_before ]]; then exit 1; fi
if (( generation > start )); then
    case "$mode" in
        fail_after) exit 1 ;;
        sticky)
            printf '%s 100 nginx: worker process\n' "$((200 + start))"
            exit 0 ;;
        delayed)
            if (( polls == 0 )); then
                printf '%s 100 nginx: worker process\n' "$((200 + start))"
                exit 0
            elif (( polls == 1 )); then
                printf '%s 100 nginx: worker process is shutting down\n' "$((200 + start))"
            fi ;;
    esac
fi
printf '%s 100 nginx: worker process\n' "$((200 + generation))"
EOF
cat > "$bin/sleep" <<'EOF'
#!/usr/bin/env bash
printf 'wait\n' >> "$TEST_SLEEP_LOG"
EOF
chmod 0755 "$bin"/*
export MIDDLE_PLATFORM_ROOT="$root"
export NGINX_BIN="$bin/nginx"
export SYSTEMCTL_BIN="$bin/systemctl"
export SUDO_BIN="$bin/sudo"
export PS_BIN="$bin/ps"
export SLEEP_BIN="$bin/sleep"
export AWK_BIN="$(command -v awk)"
export TEST_GENERATION="$tmp/generation" TEST_POLLS="$tmp/polls" TEST_SLEEP_LOG="$tmp/sleeps"
printf '0\n' > "$TEST_GENERATION"
printf '0\n' > "$TEST_POLLS"
: > "$TEST_SLEEP_LOG"
export NGINX_DRAIN_TIMEOUT_SECONDS=3
control="$script_dir/upstream-control.sh"
"$control" validate config-center 8201
"$control" drain config-center 8201
grep -Eq '^server 127[.]0[.]0[.]1:8201 .* down;$' "$root/run/nginx/config-center-upstream.conf"
"$control" enable config-center 8201
if grep -Eq '^server 127[.]0[.]0[.]1:8201 .* down;$' "$root/run/nginx/config-center-upstream.conf"; then
    printf 'Nginx upstream restore left the instance down\n' >&2
    exit 1
fi
sed -i 's/8202 max_fails=1 fail_timeout=10s;/8202 max_fails=1 fail_timeout=10s down;/' \
    "$root/run/nginx/config-center-upstream.conf"
if "$control" validate config-center 8202 >/dev/null 2>&1; then
    printf 'Already-drained peer validation unexpectedly passed\\n' >&2
    exit 1
fi
if "$control" drain config-center 8201 >/dev/null 2>&1; then
    printf 'Refused-to-drain-last-peer test unexpectedly passed\n' >&2
    exit 1
fi
if "$control" validate sdk-core 8213 >/dev/null 2>&1; then
    printf 'Removed third SDK Core port unexpectedly validated\n' >&2
    exit 1
fi
"$control" validate sdk-service 8224
if "$control" validate sdk-service 8225 >/dev/null 2>&1; then
    printf 'Unsupported fifth SDK Service port unexpectedly validated\n' >&2
    exit 1
fi
if "$control" validate config-center 8291 >/dev/null 2>&1; then
    printf 'Invalid port test unexpectedly passed\n' >&2
    exit 1
fi
printf 'include /etc/passwd;\n' >> "$root/run/nginx/config-center-upstream.conf"
if "$control" validate config-center 8201 >/dev/null 2>&1; then
    printf 'Malformed runtime upstream test unexpectedly passed\n' >&2
    exit 1
fi
# Windows CRLF runtime files: validation is read-only, mutations normalize to LF.
file="$root/run/nginx/config-center-upstream.conf"
printf 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s;\r\nserver 127.0.0.1:8202 max_fails=1 fail_timeout=10s;\r\n' > "$file"
"$control" validate config-center 8201
grep -q $'\r$' "$file"
"$control" drain config-center 8201
grep -Fxq 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s down;' "$file"
grep -Fxq 'server 127.0.0.1:8202 max_fails=1 fail_timeout=10s;' "$file"
if grep -q $'\r$' "$file"; then echo 'CRLF was not normalized on write' >&2; exit 1; fi
if "$control" drain config-center 8202; then echo 'Drained last active instance' >&2; exit 1; fi
"$control" enable config-center 8201
grep -Fxq 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s;' "$file"
printf 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s down;\r\nserver 127.0.0.1:8202 max_fails=1 fail_timeout=10s;\r\n' > "$file"
"$control" enable config-center 8201
grep -Fxq 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s;' "$file"
printf 'server 127.0.0.1:8201 max_fails=9 fail_timeout=10s;\r\nserver 127.0.0.1:8202 max_fails=1 fail_timeout=10s;\r\n' > "$file"
if "$control" validate config-center 8201; then echo 'Accepted malformed upstream' >&2; exit 1; fi
# A reload can return before the old workers change their process titles.
reset_upstream() {
    printf 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s;\nserver 127.0.0.1:8202 max_fails=1 fail_timeout=10s;\n' > "$file"
    read -r TEST_START_GENERATION < "$TEST_GENERATION"
    export TEST_START_GENERATION
    printf '0\n' > "$TEST_POLLS"
    : > "$TEST_SLEEP_LOG"
}
reset_upstream
TEST_PS_MODE=delayed "$control" drain config-center 8201
[[ $(wc -l < "$TEST_SLEEP_LOG") -eq 2 ]]
grep -Fxq 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s down;' "$file"
"$control" enable config-center 8201

reset_upstream
if TEST_PS_MODE=sticky "$control" drain config-center 8201 >"$tmp/timeout.log" 2>&1; then
    echo 'Old worker still active but drain succeeded' >&2; exit 1
fi
grep -Fq "pending worker PIDs=$((200 + TEST_START_GENERATION))" "$tmp/timeout.log"
grep -Fxq 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s down;' "$file"

# Retrying a down instance must issue another HUP before reporting safe drain.
read -r before < "$TEST_GENERATION"
"$control" drain config-center 8201
read -r after < "$TEST_GENERATION"
[[ "$after" -eq $((before + 1)) ]]

reset_upstream
if TEST_PS_MODE=fail_before "$control" drain config-center 8201; then
    echo 'Failed ps before reload was treated as successful drain' >&2; exit 1
fi
grep -Fxq 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s;' "$file"
reset_upstream
if TEST_PS_MODE=fail_after "$control" drain config-center 8201; then
    echo 'Failed ps after reload was treated as successful drain' >&2; exit 1
fi
grep -Fxq 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s down;' "$file"
# A valid down state is distinguishable from malformed files or Nginx failures.
reset_upstream
sed -i 's/8201 max_fails=1 fail_timeout=10s;/8201 max_fails=1 fail_timeout=10s down;/' "$file"
status=0
output=$("$control" validate config-center 8201) || status=$?
[[ "$status" -eq 3 && "$output" == 'UPSTREAM_DOWN config-center 8201' ]]
TEST_PS_MODE=delayed "$control" recover config-center 8201
[[ $(wc -l < "$TEST_SLEEP_LOG") -eq 2 ]]
"$control" validate config-center 8201

reset_upstream
sed -i 's/8201 max_fails=1 fail_timeout=10s;/8201 max_fails=1 fail_timeout=10s down;/' "$file"
if TEST_PS_MODE=sticky "$control" recover config-center 8201 >"$tmp/recovery.log" 2>&1; then
    echo 'Recovery succeeded without worker rotation' >&2; exit 1
fi
grep -Fq 'restored down on disk' "$tmp/recovery.log"
grep -Fxq 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s down;' "$file"
printf 'include /etc/passwd;\n' >> "$file"
status=0
output=$("$control" validate config-center 8201) || status=$?
[[ "$status" -ne 0 && "$status" -ne 3 && -z "$output" ]]
# Batch changes are atomic at the reload boundary: one peer per distinct group.
reset_upstream
[[ $("$control" capabilities) == batch-v1 ]]
read -r before < "$TEST_GENERATION"
"$control" batch validate config-center:8201 config-center:8202 sdk-core:8211 sdk-core:8212 sdk-service:8221 sdk-service:8222 sdk-service:8223 sdk-service:8224
"$control" batch validate config-center:8201 config-center:8202 sdk-core:8211 sdk-service:8221
"$control" batch drain config-center:8201 sdk-core:8211 sdk-service:8221
read -r after < "$TEST_GENERATION"
[[ "$after" -eq $((before + 1)) ]]
for pair in config-center:8201 sdk-core:8211 sdk-service:8221; do
    grep -Fq "127.0.0.1:${pair#*:} max_fails=1 fail_timeout=10s down;" "$root/run/nginx/${pair%%:*}-upstream.conf"
done
status=0
output=$("$control" batch validate config-center:8201 sdk-core:8212) || status=$?
[[ "$status" -eq 3 ]]
grep -Fxq 'UPSTREAM_DOWN config-center 8201' <<< "$output"
grep -Fxq 'Validated sdk-core upstream port 8212' <<< "$output"
if "$control" batch drain config-center:8202 sdk-core:8212; then
    printf 'Batch drained last config-center peer\n' >&2; exit 1
fi
grep -Fxq 'server 127.0.0.1:8212 max_fails=1 fail_timeout=10s;' "$root/run/nginx/sdk-core-upstream.conf"
if "$control" batch drain sdk-core:8211 sdk-core:8212; then
    printf 'Batch accepted two peers of same upstream\n' >&2; exit 1
fi
"$control" batch enable config-center:8201 sdk-core:8211 sdk-service:8221
read -r after < "$TEST_GENERATION"
[[ "$after" -eq $((before + 2)) ]]
"$control" batch validate config-center:8201 sdk-core:8211 sdk-service:8221
reset_upstream
if TEST_PS_MODE=sticky "$control" batch drain config-center:8201 sdk-core:8211; then
    printf 'Batch drain succeeded without worker rotation\n' >&2; exit 1
fi
grep -Fxq 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s down;' "$file"
grep -Fxq 'server 127.0.0.1:8211 max_fails=1 fail_timeout=10s down;' "$root/run/nginx/sdk-core-upstream.conf"
printf 'Nginx upstream control tests passed\n'
