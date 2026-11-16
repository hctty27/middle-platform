#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 && $# -ne 5 && $# -ne 6 ]]; then
    printf 'Usage: publish-remote.sh <target-directory> <archive> <sha256> [restart-flag service name:port,...]\n' >&2
    exit 2
fi
root=$1
archive=$2
expected=$3
restart=${4:-0}
service=${5:-all}
plan=${6:--}
case "$service" in
    all|config-center|config-center-access|sdk-core|sdk-service|admin) ;;
    *) printf 'Invalid service selector\n' >&2; exit 2 ;;
esac
[[ "$restart" == 0 || "$restart" == 1 ]] || { printf 'Invalid restart flag\n' >&2; exit 2; }
stage=$(dirname -- "$archive")
name=$(basename -- "$archive")
if [[ ! "$root" =~ ^/[A-Za-z0-9_/-]+$ || "$root" == / || "$root" == */ || "/$root/" == *'/../'* || "/$root/" == *'/./'* ]]; then
    printf 'Invalid target directory\n' >&2
    exit 2
fi
if [[ "$(dirname -- "$stage")" != "$root" || ! "$(basename -- "$stage")" =~ ^\.publish-[a-f0-9]{32}$ || ! "$name" =~ ^middle-platform-[A-Za-z0-9][A-Za-z0-9._-]*-linux-(amd64|arm64)\.tar\.gz$ || ! "$expected" =~ ^[a-f0-9]{64}$ ]]; then
    printf 'Invalid upload arguments\n' >&2
    exit 2
fi
for command in sha256sum tar find mktemp cp mv chmod mkdir uname flock; do
    command -v "$command" >/dev/null || { printf 'Required command not found: %s\n' "$command" >&2; exit 1; }
done
if [[ ! -d "$root" || -L "$root" ]]; then
    printf 'Target directory must exist and must not be a symlink: %s\n' "$root" >&2
    exit 1
fi
if [[ -L "$root/.publish.lock" ]]; then
    printf 'Refusing symlinked publish lock: %s\n' "$root/.publish.lock" >&2
    exit 1
fi
exec 9>"$root/.publish.lock"
flock -n 9 || { printf 'Another deployment is already running for %s\n' "$root" >&2; exit 1; }
actual=$(sha256sum -- "$archive")
if [[ "${actual%% *}" != "$expected" ]]; then
    printf 'SHA-256 mismatch; application files were not changed\n' >&2
    exit 1
fi
case "$(uname -m):$name" in
    x86_64:*-linux-amd64.tar.gz|aarch64:*-linux-arm64.tar.gz) ;;
    *) printf 'Package architecture does not match the server\n' >&2; exit 1 ;;
esac

tar -tzf "$archive" > "$stage/entries"
while IFS= read -r entry; do
    case "$entry" in
        bin/|bin/*|admin/|admin/*|sql/|sql/*|ops/|ops/*) ;;
        *) printf 'Unexpected archive entry: %s\n' "$entry" >&2; exit 1 ;;
    esac
    if [[ "/$entry/" == *'/../'* || "/$entry/" == *'/./'* ]]; then
        printf 'Unsafe archive path: %s\n' "$entry" >&2
        exit 1
    fi
done < "$stage/entries"
# package.ps1 生成的归档只允许包含普通文件和目录。
tar -tvzf "$archive" > "$stage/types"
while IFS= read -r entry; do
    case "${entry:0:1}" in
        -|d) ;;
        *) printf 'Archive contains a link or special file\n' >&2; exit 1 ;;
    esac
done < "$stage/types"
mkdir -- "$stage/unpacked"
tar --no-same-owner -xzpf "$archive" -C "$stage/unpacked"
has_binary=0
for binary in config-center config-center-access sdk-core sdk-service; do
    if [[ -e "$stage/unpacked/bin/$binary" ]]; then
        if [[ ! -f "$stage/unpacked/bin/$binary" || ! -x "$stage/unpacked/bin/$binary" ]]; then
            printf 'Missing or non-executable binary: %s\n' "$binary" >&2
            exit 1
        fi
        has_binary=1
    fi
    if [[ "$service" == all && ! -x "$stage/unpacked/bin/$binary" ]]; then
        printf 'Full package is missing binary: %s\n' "$binary" >&2
        exit 1
    fi
    if [[ "$service" != all && "$service" != "$binary" && -e "$stage/unpacked/bin/$binary" ]]; then
        printf 'Unexpected binary for selected service: %s\n' "$binary" >&2
        exit 1
    fi
done
if [[ "$service" != all && "$service" != admin && ! -x "$stage/unpacked/bin/$service" ]]; then
    printf 'Selected service executable is missing\n' >&2
    exit 1
fi
if [[ "$service" == admin && $has_binary -ne 0 ]]; then
    printf 'Admin-only package must not contain binaries\n' >&2
    exit 1
fi
if [[ "$service" == admin && ! -f "$stage/unpacked/admin/index.html" ]]; then
    printf 'Admin-only package must contain index.html\n' >&2
    exit 1
fi
if [[ -d "$stage/unpacked/admin" && ! -f "$stage/unpacked/admin/index.html" ]]; then
    printf 'Missing admin/index.html\n' >&2
    exit 1
fi

# 拒绝目标路径中的符号链接，避免意外写入配置或日志目录。
ensure_directory() {
    local path=$root part relative=$1
    IFS='/' read -r -a parts <<< "${root#/}/$relative"
    path=''
    for part in "${parts[@]}"; do
        [[ -n "$part" ]] || continue
        path="$path/$part"
        if [[ -L "$path" ]]; then
            printf 'Refusing symlinked destination directory: %s\n' "$path" >&2
            return 1
        fi
        mkdir -p -- "$path"
    done
}
find "$stage/unpacked" -type f -print0 > "$stage/files"
# 替换应用文件前，先检查并创建所有目标目录。
ensure_directory packages
while IFS= read -r -d '' source; do
    relative=${source#"$stage/unpacked/"}
    ensure_directory "$(dirname -- "$relative")"
done < "$stage/files"

# 替换应用文件前，验证所有待重启的 Supervisor 实例。
# 重启计划只包含 Supervisor 进程名和本地健康检查端口，不含密钥。
programs=()
ports=()
groups=()
if [[ "$restart" == 1 && "$service" != admin ]]; then
    for command in supervisorctl curl grep sleep; do
        command -v "$command" >/dev/null || { printf 'Required command not found: %s\n' "$command" >&2; exit 1; }
    done
    if [[ -z "$plan" || "$plan" == "-" || ${#plan} -gt 4096 ]]; then
        printf 'Missing or oversized Supervisor restart plan; no application files were changed\n' >&2
        exit 1
    fi
    declare -A seen=()
    IFS=',' read -r -a requested <<< "$plan"
    for entry in "${requested[@]}"; do
        if [[ ! "$entry" =~ ^(config-center-access|config-center|sdk-core|sdk-service)(-[1-9][0-9]*)?:([0-9]{1,5})$ ]]; then
            printf 'Invalid Supervisor plan entry: %s\n' "$entry" >&2
            exit 1
        fi
        module="${BASH_REMATCH[1]}"
        program="${module}${BASH_REMATCH[2]:-}"
        port="${BASH_REMATCH[3]}"
        if [[ "$service" != all && "$module" != "$service" ]]; then
            printf 'Supervisor plan entry outside selected service: %s\n' "$program" >&2
            exit 1
        fi
        case "$program:$port" in
            config-center-1:8201|config-center-2:8202) group=config-center ;;
            sdk-core-1:8211|sdk-core-2:8212|sdk-core-3:8213|sdk-core-4:8214) group=sdk-core ;;
            sdk-service-1:8221|sdk-service-2:8222|sdk-service-3:8223|sdk-service-4:8224) group=sdk-service ;;
            config-center-access:8291) group=none ;;
            *) printf 'Supervisor process and health port do not match: %s:%s\n' "$program" "$port" >&2; exit 1 ;;
        esac
        if [[ -n "${seen[$program]:-}" ]]; then
            printf 'Duplicate Supervisor process in plan: %s\n' "$program" >&2
            exit 1
        fi
        seen["$program"]=1
        programs+=("$program")
        ports+=("$((10#$port))")
        groups+=("$group")
    done
    if [[ ${#programs[@]} -eq 0 ]]; then
        printf 'Empty Supervisor restart plan\n' >&2
        exit 1
    fi

    required=()
    case "$service" in
        all)
            required=(config-center-1 config-center-2
                sdk-core-1 sdk-core-2 sdk-core-3 sdk-core-4
                sdk-service-1 sdk-service-2 sdk-service-3 sdk-service-4)
            ;;
        config-center) required=(config-center-1 config-center-2) ;;
        sdk-core) required=(sdk-core-1 sdk-core-2 sdk-core-3 sdk-core-4) ;;
        sdk-service) required=(sdk-service-1 sdk-service-2 sdk-service-3 sdk-service-4) ;;
        config-center-access) required=(config-center-access) ;;
    esac
    for required_program in "${required[@]}"; do
        if [[ -z "${seen[$required_program]:-}" ]]; then
            printf 'Incomplete Supervisor restart plan; missing %s\n' "$required_program" >&2
            exit 1
        fi
    done

    nginx_control="$stage/unpacked/ops/nginx/upstream-control.sh"
    if [[ ! -x "$nginx_control" ]]; then
        printf 'Nginx upstream control helper is missing from the release package\n' >&2
        exit 1
    fi
    for index in "${!programs[@]}"; do
        program="${programs[$index]}"
        group="${groups[$index]}"
        port="${ports[$index]}"
        supervisor_process="$program"
        if [[ "$group" != none ]]; then supervisor_process="$group:$program"; fi
        if ! supervisorctl status "$supervisor_process" >/dev/null 2>&1; then
            printf 'Supervisor program is unavailable: %s; no application files were changed\n' "$supervisor_process" >&2
            exit 1
        fi
        if ! curl -fsS -o /dev/null --connect-timeout 2 --max-time 3 "http://127.0.0.1:$port/health/ready"; then
            printf 'Instance is not Ready: %s on port %s; no application files were changed\\n' "$program" "$port" >&2
            exit 1
        fi
        if [[ "$group" != none ]]; then
            MIDDLE_PLATFORM_ROOT="$root" "$nginx_control" validate "$group" "$port"
        fi
    done
fi

temporary=''
trap 'if [[ -n "$temporary" ]]; then rm -f -- "$temporary"; fi' EXIT
while IFS= read -r -d '' source; do
    relative=${source#"$stage/unpacked/"}
    target="$root/$relative"
    if [[ "$relative" == admin/config.js && ( -e "$target" || -L "$target" ) ]]; then
        printf 'Preserved admin/config.js\n'
        continue
    fi
    temporary=$(mktemp "$(dirname -- "$target")/.publish-file-XXXXXXXX")
    cp -p -- "$source" "$temporary"
    # 使用同目录原子重命名，避免直接覆盖运行中的可执行文件。
    mv -fT -- "$temporary" "$target"
    temporary=''
    printf 'Updated %s\n' "$relative"
done < "$stage/files"
if [[ "${#programs[@]}" -gt 0 ]]; then
    for index in "${!programs[@]}"; do
        program="${programs[$index]}"
        port="${ports[$index]}"
        group="${groups[$index]}"
        supervisor_process="$program"
        if [[ "$group" != none ]]; then supervisor_process="$group:$program"; fi
        if [[ "$group" != none ]]; then
            printf 'Draining %s from Nginx...\n' "$program"
            MIDDLE_PLATFORM_ROOT="$root" "$root/ops/nginx/upstream-control.sh" drain "$group" "$port"
        fi
        printf 'Restarting %s...\n' "$program"
        if ! supervisorctl restart "$supervisor_process"; then
            printf 'Supervisor restart failed: %s; instance remains drained\n' "$supervisor_process" >&2
            exit 1
        fi
        ready=0
        stable=0
        for attempt in {1..60}; do
            if supervisorctl status "$supervisor_process" | grep -Eq '[[:space:]]RUNNING[[:space:]]' &&
               curl -fsS -o /dev/null --connect-timeout 2 --max-time 3 "http://127.0.0.1:$port/health/ready"; then
                stable=$((stable + 1))
                if [[ $stable -ge 3 ]]; then
                    ready=1
                    break
                fi
            else
                stable=0
            fi
            sleep 2
        done
        if [[ $ready -ne 1 ]]; then
            printf 'Health check failed: %s on port %s; instance remains drained and rollout stopped\n' "$program" "$port" >&2
            exit 1
        fi
        printf 'Healthy: %s (port %s)\n' "$program" "$port"
        if [[ "$group" != none ]]; then
            MIDDLE_PLATFORM_ROOT="$root" "$root/ops/nginx/upstream-control.sh" enable "$group" "$port"
            printf 'Restored %s to Nginx traffic\n' "$program"
        fi
    done
fi
mv -fT -- "$archive" "$root/packages/$name"
rm -rf -- "$stage"
printf 'Upload and extraction complete. SQL was NOT executed.\n'
if [[ "$restart" == 0 || "$service" == admin ]]; then
    printf 'No services were restarted.\n'
else
    printf 'Selected Supervisor programs were restarted and health-checked.\n'
fi
