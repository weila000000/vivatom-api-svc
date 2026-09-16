#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_dir"

# Let the selected Go binary resolve its matching SDK instead of inheriting a stale IDE override.
unset GOROOT

go test ./...
go vet ./...

if [[ ! -d build-worker/node_modules ]]; then
  npm ci --prefix build-worker
fi
npm test --prefix build-worker
