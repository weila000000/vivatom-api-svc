#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_dir"

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

export VIVATOM_BUILDER_TOKEN="${VIVATOM_BUILDER_TOKEN:-vivatom-local-builder}"
export VIVATOM_ARTIFACT_ROOT="${VIVATOM_ARTIFACT_ROOT:-$repo_dir/.data/artifacts}"
export VIVATOM_BUILDER_CONCURRENCY="${VIVATOM_BUILDER_CONCURRENCY:-2}"
export VIVATOM_PREVIEW_PORT="${VIVATOM_PREVIEW_PORT:-8091}"

if [[ ! -d build-worker/node_modules ]]; then
  npm ci --prefix build-worker
fi

exec npm start --prefix build-worker
