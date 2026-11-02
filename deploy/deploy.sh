#!/usr/bin/env bash
# Deploy one already-drained application node; the operator handles gateway draining.
set -euo pipefail

usage() {
  echo "Usage: $0 sha-<40-character-git-commit> [config-center|sdk-core|admin]" >&2
  exit 2
}

[[ $# -ge 1 && $# -le 2 ]] || usage
IMAGE_TAG="$1"
[[ "$IMAGE_TAG" =~ ^sha-[0-9a-f]{40}$ ]] || usage

COMPOSE_FILE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/compose.prod.yml"
ENV_FILE="${ENV_FILE:-/etc/middle-platform/compose.env}"
[[ -r "$ENV_FILE" ]] || { echo "Missing environment file: $ENV_FILE" >&2; exit 1; }
command -v docker >/dev/null || { echo "Docker is not installed" >&2; exit 1; }

services=(config-center sdk-core admin)
if [[ $# -eq 2 ]]; then
  case "$2" in
    config-center|sdk-core|admin) services=("$2");;
    *) usage;;
  esac
fi

export IMAGE_TAG
dc() { docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"; }

dc config --quiet
dc pull "${services[@]}"
dc up -d --no-deps --wait --wait-timeout 120 "${services[@]}"
dc ps
echo "Deployed $IMAGE_TAG. Verify external routes and webhook delivery before restoring gateway traffic."
