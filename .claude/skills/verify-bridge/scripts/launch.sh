#!/usr/bin/env bash
# Usage: launch.sh <run-id> [scenario] [--deny-exports]
set -euo pipefail
source "$(dirname "$0")/_env.sh"
run_id="${1:?usage: launch.sh <run-id> [scenario] [--deny-exports]}"; shift
export BRIDGE_RUN="$VERIFY_ROOT/$run_id"
if [[ -e "$BRIDGE_RUN" ]]; then echo "$BRIDGE_RUN already exists; pick a new run id" >&2; exit 2; fi
mkdir -p "$BRIDGE_RUN/bin" "$BRIDGE_RUN/evidence"
touch "$BRIDGE_RUN/.verify-bridge-run"
(cd "$REPO_ROOT" && go_cmd build -o "$BRIDGE_RUN/bin/bridgehost" "$HARNESS")
{
  echo "commit=$(git -C "$REPO_ROOT" rev-parse HEAD)"
  echo "dirty=$(git -C "$REPO_ROOT" status --porcelain -- bridge db internal go.mod go.sum | wc -l | tr -d ' ')"
  echo "go=$(cd "$REPO_ROOT" && go_cmd version)"
} > "$BRIDGE_RUN/evidence/build.txt"
"$(dirname "$0")/bh.sh" init "$@"
echo "export BRIDGE_RUN=$BRIDGE_RUN"
