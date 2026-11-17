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
server 127.0.0.1:8213 max_fails=1 fail_timeout=10s;
server 127.0.0.1:8214 max_fails=1 fail_timeout=10s;
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
    reload) exit 0 ;;
    *) exit 2 ;;
esac
EOF
cat > "$bin/ps" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
cat > "$bin/sleep" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod 0755 "$bin"/*
export MIDDLE_PLATFORM_ROOT="$root"
export NGINX_BIN="$bin/nginx"
export SYSTEMCTL_BIN="$bin/systemctl"
export SUDO_BIN="$bin/sudo"
export PS_BIN="$bin/ps"
export SLEEP_BIN="$bin/sleep"
export AWK_BIN="$(command -v awk)"
export NGINX_DRAIN_TIMEOUT_SECONDS=2
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
if "$control" validate sdk-core 8215 >/dev/null 2>&1; then
    printf 'Removed fifth SDK Core port unexpectedly validated\n' >&2
    exit 1
fi
if "$control" validate sdk-service 8225 >/dev/null 2>&1; then
    printf 'Removed fifth SDK Service port unexpectedly validated\n' >&2
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
grep -q 
\r
 "$file"
"$control" drain config-center 8201
grep -Fxq 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s down;' "$file"
grep -Fxq 'server 127.0.0.1:8202 max_fails=1 fail_timeout=10s;' "$file"
if grep -q 
\r
 "$file"; then echo 'CRLF was not normalized on write' >&2; exit 1; fi
if "$control" drain config-center 8202; then echo 'Drained last active instance' >&2; exit 1; fi
"$control" enable config-center 8201
grep -Fxq 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s;' "$file"
printf 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s down;\r\nserver 127.0.0.1:8202 max_fails=1 fail_timeout=10s;\r\n' > "$file"
"$control" enable config-center 8201
grep -Fxq 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s;' "$file"
printf 'server 127.0.0.1:8201 max_fails=9 fail_timeout=10s;\r\nserver 127.0.0.1:8202 max_fails=1 fail_timeout=10s;\r\n' > "$file"
if "$control" validate config-center 8201; then echo 'Accepted malformed upstream' >&2; exit 1; fi
printf 'Nginx upstream control tests passed\n'
