#!/usr/bin/env bash
set -euo pipefail
set -m

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
web_dir="${VIVATOM_WEB_DIR:-$repo_dir/../vivatom-web}"

if [[ ! -x "$web_dir/scripts/dev.sh" ]]; then
  echo "vivatom-web was not found at $web_dir" >&2
  exit 1
fi

children=()
cleanup() {
  trap - INT TERM EXIT
  for pid in "${children[@]}"; do
		kill -TERM -- "-$pid" 2>/dev/null || true
  done
  wait 2>/dev/null || true
}
trap cleanup INT TERM EXIT

"$repo_dir/scripts/worker.sh" &
children+=("$!")
"$repo_dir/scripts/api.sh" &
children+=("$!")
"$web_dir/scripts/dev.sh" &
children+=("$!")

while kill -0 "${children[0]}" 2>/dev/null && kill -0 "${children[1]}" 2>/dev/null && kill -0 "${children[2]}" 2>/dev/null; do
  sleep 1
done
