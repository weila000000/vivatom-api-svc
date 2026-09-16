#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_dir"

# Let the selected Go binary resolve its matching SDK instead of inheriting a stale IDE override.
unset GOROOT

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

export VIVATOM_HTTP_ADDRESS="${VIVATOM_HTTP_ADDRESS:-:8080}"
export VIVATOM_RUNTIME_DATABASE_PATH="${VIVATOM_RUNTIME_DATABASE_PATH:-.data/vivatom-runtime.db}"
export VIVATOM_BUILDER_URL="${VIVATOM_BUILDER_URL:-http://localhost:8090}"
export VIVATOM_BUILDER_TOKEN="${VIVATOM_BUILDER_TOKEN:-vivatom-local-builder}"
export VIVATOM_AI_PROVIDER="${VIVATOM_AI_PROVIDER:-auto}"
export VIBE_BASE_URL="${VIBE_BASE_URL:-https://vibe.linux008.com/v1}"
export VIBE_MODEL="${VIBE_MODEL:-gpt-6-astra}"

exec go run ./cmd/api
