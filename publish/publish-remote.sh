#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
    printf 'Usage: publish-remote.sh <target-directory> <archive> <sha256>\n' >&2
    exit 2
fi
root=$1
archive=$2
expected=$3
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
for command in sha256sum tar find mktemp cp mv chmod mkdir uname; do
    command -v "$command" >/dev/null || { printf 'Required command not found: %s\n' "$command" >&2; exit 1; }
done
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
        bin/|bin/*|admin/|admin/*|sql/|sql/*) ;;
        *) printf 'Unexpected archive entry: %s\n' "$entry" >&2; exit 1 ;;
    esac
    if [[ "/$entry/" == *'/../'* || "/$entry/" == *'/./'* ]]; then
        printf 'Unsafe archive path: %s\n' "$entry" >&2
        exit 1
    fi
done < "$stage/entries"
# Packages produced by package.ps1 contain regular files and directories only.
tar -tvzf "$archive" > "$stage/types"
while IFS= read -r entry; do
    case "${entry:0:1}" in
        -|d) ;;
        *) printf 'Archive contains a link or special file\n' >&2; exit 1 ;;
    esac
done < "$stage/types"
mkdir -- "$stage/unpacked"
tar --no-same-owner -xzpf "$archive" -C "$stage/unpacked"
for binary in config-center config-center-access sdk-core sdk-service; do
    if [[ ! -f "$stage/unpacked/bin/$binary" || ! -x "$stage/unpacked/bin/$binary" ]]; then
        printf 'Missing or non-executable binary: %s\n' "$binary" >&2
        exit 1
    fi
done
if [[ -d "$stage/unpacked/admin" && ! -f "$stage/unpacked/admin/index.html" ]]; then
    printf 'Missing admin/index.html\n' >&2
    exit 1
fi

# Refuse symlinked destination directories instead of following them into config/logs.
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
# Check all destination directories before replacing any application file.
ensure_directory packages
while IFS= read -r -d '' source; do
    relative=${source#"$stage/unpacked/"}
    ensure_directory "$(dirname -- "$relative")"
done < "$stage/files"

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
    # Same-directory rename avoids writing into an executable used by a live process.
    mv -fT -- "$temporary" "$target"
    temporary=''
    printf 'Updated %s\n' "$relative"
done < "$stage/files"
mv -fT -- "$archive" "$root/packages/$name"
rm -rf -- "$stage"
printf 'Upload and extraction complete. Services were NOT restarted. SQL was NOT executed.\n'
