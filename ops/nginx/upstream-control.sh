#!/usr/bin/env bash
set -euo pipefail
if [[ $# -eq 1 && "$1" == capabilities ]]; then
    printf 'batch-v1\n'
    exit 0
fi
if [[ $# -ge 3 && "$1" == batch ]]; then
    action=$2
    shift 2
    peers=("$@")
    case "$action" in validate|drain|enable) ;; *) exit 2 ;; esac
elif [[ $# -eq 3 ]]; then
    action=$1
    peers=("$2:$3")
else
    printf 'Usage: upstream-control.sh <validate|drain|enable|recover> <group> <port> | batch <validate|drain|enable> <group:port> ...\n' >&2
    exit 2
fi
root=${MIDDLE_PLATFORM_ROOT:-/data/middle-platform}
case "$action" in validate|drain|enable|recover) ;; *) printf 'Invalid upstream action\n' >&2; exit 2 ;; esac
[[ ${#peers[@]} -le 12 ]] || exit 2
declare -A selected_peers=() selected_groups=()
groups=() ports=() files=() upstream_names=() targets=() downs=()
for peer in "${peers[@]}"; do
    if [[ ! "$peer" =~ ^(config-center|sdk-core|sdk-service):([0-9]+)$ ]]; then
        printf 'Unsupported upstream group or port: %s\n' "$peer" >&2
        exit 2
    fi
    group=${BASH_REMATCH[1]}
    port=${BASH_REMATCH[2]}
    if [[ -n "${selected_peers[$peer]:-}" || ( "$action" != validate && -n "${selected_groups[$group]:-}" ) ]]; then
        printf 'Duplicate peer or multiple mutations of the same upstream: %s\n' "$peer" >&2
        exit 2
    fi
    selected_peers[$peer]=1
    selected_groups[$group]=1
    case "$group:$port" in
        config-center:8201|config-center:8202)
            expected_count=2; upstream_name=config_center; file="$root/run/nginx/config-center-upstream.conf" ;;
        sdk-core:8211|sdk-core:8212|sdk-core:8213|sdk-core:8214)
            expected_count=4; upstream_name=sdk_core; file="$root/run/nginx/sdk-core-upstream.conf" ;;
        sdk-service:8221|sdk-service:8222|sdk-service:8223|sdk-service:8224|sdk-service:8225|sdk-service:8226)
            expected_count=6; upstream_name=sdk_service; file="$root/run/nginx/sdk-service-upstream.conf" ;;
        *) printf 'Unsupported upstream group or port: %s:%s\n' "$group" "$port" >&2; exit 2 ;;
    esac
    for directory in "$root" "$root/run" "$root/run/nginx"; do
        if [[ -L "$directory" ]]; then printf 'Refusing symlinked directory: %s\n' "$directory" >&2; exit 1; fi
    done
    if [[ ! -f "$file" || -L "$file" ]]; then
        printf 'Missing or unsafe Nginx runtime upstream file: %s\n' "$file" >&2; exit 1
    fi
    target="127.0.0.1:$port"
    if ! counts=$(awk -v target="$target" '
        { sub(/\r$/, "", $0) }
        NF == 0 { next }
        {
            if ($1 != "server" || $2 !~ /^127[.]0[.]0[.]1:[0-9]+$/ ||
                $3 != "max_fails=1" ||
                !((NF == 4 && $4 == "fail_timeout=10s;") ||
                  (NF == 5 && $4 == "fail_timeout=10s" && $5 == "down;"))) {
                invalid = 1
                next
            }
            total++
            down = (NF == 5)
            if (!down) active++
            if ($2 == target) { matches++; if (down) target_down = 1 }
        }
        END {
            if (invalid) exit 2
            printf "%d %d %d %d\n", total, active, matches, target_down
        }
    ' "$file"); then
        printf 'Unsafe or malformed Nginx upstream file: %s\n' "$file" >&2
        exit 1
    fi
    read -r total active matches target_down <<< "$counts"
    if [[ "$total" -ne "$expected_count" || "$matches" -ne 1 ]]; then
        printf 'Unexpected upstream membership in %s (total=%s matches=%s expected=%s)\n' \
            "$file" "$total" "$matches" "$expected_count" >&2
        exit 1
    fi
    if [[ "$action" == drain && "$target_down" -eq 0 && "$active" -le 1 ]]; then
        printf 'Refusing to drain the last active %s instance\n' "$group" >&2
        exit 1
    fi
    groups+=("$group")
    ports+=("$port")
    files+=("$file")
    upstream_names+=("$upstream_name")
    targets+=("$target")
    downs+=("$target_down")
done
nginx_bin=${NGINX_BIN:-$(command -v nginx || true)}
systemctl_bin=${SYSTEMCTL_BIN:-$(command -v systemctl || true)}
sudo_bin=${SUDO_BIN:-$(command -v sudo || true)}
ps_bin=${PS_BIN:-$(command -v ps || true)}
sleep_bin=${SLEEP_BIN:-$(command -v sleep || true)}
awk_bin=${AWK_BIN:-$(command -v awk || true)}
for item in "$nginx_bin" "$systemctl_bin" "$sudo_bin" "$ps_bin" "$sleep_bin" "$awk_bin"; do
    if [[ -z "$item" || ! -x "$item" ]]; then printf 'Required Nginx control command is unavailable\n' >&2; exit 1; fi
done
if ! "$systemctl_bin" is-active --quiet nginx; then
    printf 'Nginx is not active under systemd\n' >&2; exit 1
fi
validate_nginx_config() {
    "$sudo_bin" -n "$nginx_bin" -t || return 1
    local dump
    dump=$("$sudo_bin" -n "$nginx_bin" -T 2>&1) || {
        printf 'nginx -T failed; inspect the Nginx configuration as root\n' >&2
        return 1
    }
    local index file upstream_name
    for index in "${!files[@]}"; do
        file=${files[$index]}
        upstream_name=${upstream_names[$index]}
        if ! grep -Fq "# configuration file $file:" <<< "$dump"; then
        printf 'Nginx does not load runtime include: %s\n' "$file" >&2; return 1
    fi
    if ! grep -Eq "^[[:space:]]*upstream[[:space:]]+$upstream_name[[:space:]]*\\{" <<< "$dump"; then
        printf 'Nginx upstream block is missing: %s\n' "$upstream_name" >&2; return 1
    fi
    if ! grep -Eq "^[[:space:]]*proxy_pass[[:space:]]+http://$upstream_name;" <<< "$dump"; then
        printf 'Nginx proxy location is missing for upstream: %s\n' "$upstream_name" >&2; return 1
    fi
    done
    if ! grep -Eq "^[[:space:]]*worker_shutdown_timeout[[:space:]]+75s;" <<< "$dump"; then
        printf 'Nginx main context must set worker_shutdown_timeout 75s\n' >&2; return 1
    fi
}
reload_nginx() { "$sudo_bin" -n "$systemctl_bin" reload nginx; }
# Track the systemd-managed master and snapshot real worker PIDs before HUP.
# Capture ps output before awk starts; exact fields and PPID exclude helper commands.
nginx_master_pid() {
    local value
    if ! value=$("$systemctl_bin" show -p MainPID nginx); then
        printf 'Could not query Nginx MainPID\n' >&2; return 1
    fi
    if [[ ! "$value" =~ ^MainPID=([1-9][0-9]*)$ ]]; then
        printf 'Invalid Nginx MainPID: %s\n' "$value" >&2; return 1
    fi
    printf '%s\n' "${BASH_REMATCH[1]}"
}
read_nginx_workers() {
    local master=$1 listing
    if ! listing=$("$ps_bin" -ww -eo pid=,ppid=,args=); then
        printf 'Could not query Nginx worker processes\n' >&2; return 1
    fi
    "$awk_bin" -v master="$master" '
        $2 == master && $3 == "nginx:" && $4 == "worker" && $5 == "process" {
            if ($1 !~ /^[1-9][0-9]*$/) { invalid=1; next }
            if (NF == 5 || (NF == 6 && $6 == "(nginx)")) {
                print $1, "active"
            } else if ($6 == "is" && $7 == "shutting" && $8 == "down" &&
                       (NF == 8 || (NF == 9 && $9 == "(nginx)"))) {
                print $1, "shutting"
            } else { invalid=1 }
        }
        END { if (invalid) exit 2 }
    ' <<< "$listing" || { printf 'Could not parse Nginx worker processes\n' >&2; return 1; }
}
snapshot_old_workers() {
    local workers pid state
    master_pid=$(nginx_master_pid) || return 1
    workers=$(read_nginx_workers "$master_pid") || return 1
    old_workers=' '
    local active_count=0
    while read -r pid state; do
        [[ -n "$pid" ]] || continue
        old_workers+="$pid "
        if [[ "$state" == active ]]; then active_count=$((active_count + 1)); fi
    done <<< "$workers"
    if [[ "$active_count" -eq 0 ]]; then
        printf 'No active Nginx workers found under master PID %s; refusing to drain\n' "$master_pid" >&2
        return 1
    fi
}
wait_for_old_workers() {
    local timeout=${NGINX_DRAIN_TIMEOUT_SECONDS:-90}
    if [[ ! "$timeout" =~ ^[1-9][0-9]*$ ]]; then
        printf 'Invalid NGINX_DRAIN_TIMEOUT_SECONDS value\n' >&2
        return 1
    fi
    local elapsed=0 current_master workers pid state pending new_active
    while (( elapsed < timeout )); do
        current_master=$(nginx_master_pid) || return 1
        if [[ "$current_master" != "$master_pid" ]]; then
            printf 'Nginx master PID changed from %s to %s while draining\n' "$master_pid" "$current_master" >&2
            return 1
        fi
        workers=$(read_nginx_workers "$master_pid") || return 1
        pending=''
        new_active=0
        while read -r pid state; do
            [[ -n "$pid" ]] || continue
            if [[ "$old_workers" == *" $pid "* || "$state" == shutting ]]; then
                pending+="$pid "
            elif [[ "$state" == active ]]; then
                new_active=$((new_active + 1))
            fi
        done <<< "$workers"
        # HUP is asynchronous: require both old PIDs gone and a new active generation.
        if [[ -z "$pending" && "$new_active" -gt 0 ]]; then return 0; fi
        "$sleep_bin" 1 || return 1
        elapsed=$((elapsed + 1))
    done
    printf 'Timed out waiting for old Nginx workers to drain (%s seconds); master PID=%s; pending worker PIDs=%s; new active workers=%s\n' \
        "$timeout" "$master_pid" "${pending:-none}" "$new_active" >&2
    return 1
}
validate_nginx_config
if [[ "$action" == validate ]]; then
    result=0
    for index in "${!groups[@]}"; do
        group=${groups[$index]}
        port=${ports[$index]}
        if [[ "${downs[$index]}" -eq 1 ]]; then
            printf 'UPSTREAM_DOWN %s %s\n' "$group" "$port"
            printf 'Upstream instance is already marked down: %s port %s\n' "$group" "$port" >&2
            result=3
        else
            printf 'Validated %s upstream port %s\n' "$group" "$port"
        fi
    done
    exit "$result"
fi

# Prepare every peer file before replacing any of them. Only one mutation per
# upstream is accepted. The publish/Monit lock is held by the caller.
backups=() temporaries=() changes=() applied=()
cleanup() {
    local index
    for index in "${!files[@]}"; do
        if [[ -n "${temporaries[$index]:-}" ]]; then rm -f -- "${temporaries[$index]}"; fi
        if [[ -n "${backups[$index]:-}" ]]; then rm -f -- "${backups[$index]}"; fi
    done
}
trap cleanup EXIT
for index in "${!files[@]}"; do
    file=${files[$index]}
    target=${targets[$index]}
    if [[ ( "$action" == drain && "${downs[$index]}" -eq 1 ) ||
          ( "$action" != drain && "${downs[$index]}" -eq 0 ) ]]; then
        continue
    fi
    directory=$(dirname -- "$file")
    backup=$(mktemp "$directory/.upstream-backup-XXXXXXXX")
    backups[$index]=$backup
    temporary=$(mktemp "$directory/.upstream-update-XXXXXXXX")
    temporaries[$index]=$temporary
    cp -p -- "$file" "$backup"
    if ! awk -v target="$target" -v action="$action" '
        { sub(/\r$/, "", $0) }
        $1 == "server" && $2 == target {
            matched++
            down = 0
            for (i = 3; i <= NF; i++) if ($i == "down;") down = 1
            if (action == "drain" && !down) sub(/;[[:space:]]*$/, " down;")
            if ((action == "enable" || action == "recover") && down) {
                $4 = "fail_timeout=10s;"
                $5 = ""
                line = $1 " " $2
                for (i = 3; i <= NF; i++) if ($i != "") line = line " " $i
                $0 = line
            }
        }
        { print }
        END { if (matched != 1) exit 2 }
    ' "$file" > "$temporary"; then
        printf 'Could not update Nginx upstream state\n' >&2; exit 1
    fi
    chmod --reference="$file" "$temporary"
    changes+=("$index")
done

restore_files() {
    local index
    if [[ -n "${applied[0]:-}" ]]; then
        for index in "${applied[@]}"; do
            if ! mv -fT -- "${backups[$index]}" "${files[$index]}"; then
                printf 'Could not restore upstream file: %s\n' "${files[$index]}" >&2
                # Preserve its backup for manual recovery.
                unset 'backups[$index]'
            fi
        done
    fi
}
rollback_reload() {
    restore_files
    "$sudo_bin" -n "$nginx_bin" -t >/dev/null 2>&1 || true
    reload_nginx >/dev/null 2>&1 || true
}

if [[ -n "${changes[0]:-}" ]]; then
    for index in "${changes[@]}"; do
        if ! mv -fT -- "${temporaries[$index]}" "${files[$index]}"; then
            restore_files
            exit 1
        fi
        applied+=("$index")
    done
    if ! validate_nginx_config; then restore_files; exit 1; fi
elif [[ "$action" != drain ]]; then
    printf 'Requested peers are already enabled\n'
    exit 0
fi
# A previously down peer may not yet have been applied to running workers.
# Always rotate workers before returning a successful drain.
if [[ "$action" == drain || "$action" == recover ]] && ! snapshot_old_workers; then
    restore_files
    exit 1
fi
if ! reload_nginx; then
    rollback_reload
    printf 'Nginx reload failed; upstream state was rolled back\n' >&2
    exit 1
fi
if [[ "$action" == drain ]] && ! wait_for_old_workers; then
    printf 'Instance remains drained; refusing to restart it before Nginx workers exit\n' >&2
    exit 1
fi
if [[ "$action" == recover ]] && ! wait_for_old_workers; then
    rollback_reload
    printf 'Nginx recovery could not confirm worker rotation; restored down on disk; inspect Nginx runtime before retrying\n' >&2
    exit 1
fi
for index in "${!groups[@]}"; do
    printf '%s %s upstream port %s\n' "$action" "${groups[$index]}" "${ports[$index]}"
done
