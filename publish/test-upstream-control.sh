#!/usr/bin/env bash
set -euo pipefail
repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
export MIDDLE_PLATFORM_ROOT="$tmp/root"
mkdir -p "$MIDDLE_PLATFORM_ROOT/run/nginx" "$tmp/bin"
file="$MIDDLE_PLATFORM_ROOT/run/nginx/config-center-upstream.conf"
printf 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s;\r\nserver 127.0.0.1:8202 max_fails=1 fail_timeout=10s;\r\n' > "$file"

# Mock only external Nginx/systemd operations; real awk and file mutations are exercised.
cat > "$tmp/bin/nginx" <<'EOF'
#!/usr/bin/env bash
case "$1" in
    -t) exit 0 ;;
    -T)
        printf 'upstream config_center {\nproxy_pass http://config_center;\nworker_shutdown_timeout 75s;\n# configuration file %s/run/nginx/config-center-upstream.conf:\n' "$MIDDLE_PLATFORM_ROOT"
        ;;
    *) exit 2 ;;
esac
EOF
cat > "$tmp/bin/sudo" <<'EOF'
#!/usr/bin/env bash
[[ "$1" == -n ]] || exit 2
shift
exec "$@"
EOF
printf '#!/usr/bin/env bash\nexit 0\n' > "$tmp/bin/systemctl"
printf '#!/usr/bin/env bash\nexit 0\n' > "$tmp/bin/ps"
chmod +x "$tmp/bin/"*
export NGINX_BIN="$tmp/bin/nginx" SUDO_BIN="$tmp/bin/sudo" SYSTEMCTL_BIN="$tmp/bin/systemctl" PS_BIN="$tmp/bin/ps"
helper="$repo/ops/nginx/upstream-control.sh"
bash "$helper" validate config-center 8201
# Validation is read-only.
grep -q $'\r$' "$file"
bash "$helper" drain config-center 8201
grep -Fxq 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s down;' "$file"
grep -Fxq 'server 127.0.0.1:8202 max_fails=1 fail_timeout=10s;' "$file"
if grep -q $'\r$' "$file"; then echo 'CRLF was not normalized on write' >&2; exit 1; fi
if bash "$helper" drain config-center 8202; then echo 'Drained last active instance' >&2; exit 1; fi
bash "$helper" enable config-center 8201
grep -Fxq 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s;' "$file"
# CRLF with an existing down entry must also be accepted when enabling.
printf 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s down;\r\nserver 127.0.0.1:8202 max_fails=1 fail_timeout=10s;\r\n' > "$file"
bash "$helper" enable config-center 8201
grep -Fxq 'server 127.0.0.1:8201 max_fails=1 fail_timeout=10s;' "$file"
printf 'server 127.0.0.1:8201 max_fails=9 fail_timeout=10s;\r\nserver 127.0.0.1:8202 max_fails=1 fail_timeout=10s;\r\n' > "$file"
if bash "$helper" validate config-center 8201; then echo 'Accepted malformed upstream' >&2; exit 1; fi
echo 'Upstream LF/CRLF regression checks passed.'
