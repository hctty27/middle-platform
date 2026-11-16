#!/usr/bin/env bash
set -euo pipefail

root=/data/middle-platform
if [[ $# -ne 1 || ! "$1" =~ ^[A-Za-z_][A-Za-z0-9_-]*$ ]]; then
    printf 'Usage: install-runtime.sh <ssh-deploy-user>\n' >&2
    exit 2
fi
if [[ "$EUID" -ne 0 ]]; then
    printf 'Run this installer with sudo\n' >&2
    exit 1
fi

deploy_user=$1
if ! id "$deploy_user" >/dev/null 2>&1; then
    printf 'Deployment user does not exist: %s\n' "$deploy_user" >&2
    exit 1
fi
deploy_group=$(id -gn "$deploy_user")
source_dir="$root/ops/nginx/runtime"
target_dir="$root/run/nginx"
if [[ ! -d "$source_dir" || -L "$source_dir" || -L "$root" || -L "$root/run" || -L "$target_dir" ]]; then
    printf 'Missing or unsafe Nginx runtime template directory\n' >&2
    exit 1
fi

install -d -o "$deploy_user" -g "$deploy_group" -m 0750 "$target_dir"
chown "$deploy_user:$deploy_group" "$target_dir"
chmod 0750 "$target_dir"
publish_lock="$root/.publish.lock"
if [[ -L "$publish_lock" ]]; then
    printf 'Refusing symlinked publish lock: %s\n' "$publish_lock" >&2
    exit 1
fi
touch "$publish_lock"
chown "$deploy_user:$deploy_group" "$publish_lock"
chmod 0660 "$publish_lock"

for name in config-center sdk-core sdk-service; do
    source="$source_dir/$name-upstream.conf"
    target="$target_dir/$name-upstream.conf"
    if [[ ! -f "$source" || -L "$source" ]]; then
        printf 'Missing or unsafe runtime template: %s\n' "$source" >&2
        exit 1
    fi
    if [[ -e "$target" || -L "$target" ]]; then
        if [[ ! -f "$target" || -L "$target" ]]; then
            printf 'Refusing to overwrite unsafe runtime file: %s\n' "$target" >&2
            exit 1
        fi
        chown "$deploy_user:$deploy_group" "$target"
        chmod 0640 "$target"
        printf 'Preserved existing upstream state: %s\n' "$target"
    else
        install -o "$deploy_user" -g "$deploy_group" -m 0640 "$source" "$target"
        printf 'Installed %s\n' "$target"
    fi
done
