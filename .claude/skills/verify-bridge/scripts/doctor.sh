#!/usr/bin/env bash
# Read-only: is the instance at $BRIDGE_RUN worth driving?
set -uo pipefail
source "$(dirname "$0")/_env.sh"
require_run
ok=1
check() { if eval "$2"; then echo "ok   $1"; else echo "FAIL $1"; ok=0; fi; }
journal="$BRIDGE_RUN/instance/journal.sqlite3"
check "harness binary built" '[[ -x "$BRIDGE_RUN/bin/bridgehost" ]]'
check "host state present" 'jq -e .scenario "$BRIDGE_RUN/instance/host.json" >/dev/null 2>&1'
check "journal migrated to v3" '[[ "$(sqlite3 -readonly "$journal" "SELECT MAX(version_id) FROM goose_db_version" 2>/dev/null)" == 3 ]]'
built=$(sed -n 's/^commit=//p' "$BRIDGE_RUN/evidence/build.txt" 2>/dev/null)
check "binary built from current HEAD" '[[ "$built" == "$(git -C "$REPO_ROOT" rev-parse HEAD)" ]]'
check "no uncommitted core changes since build" '[[ "$(sed -n "s/^dirty=//p" "$BRIDGE_RUN/evidence/build.txt")" == "$(git -C "$REPO_ROOT" status --porcelain -- bridge db internal go.mod go.sum | wc -l | tr -d " ")" ]]'
if [[ $ok == 1 ]]; then
  echo "scenario=$(jq -r .scenario "$BRIDGE_RUN/instance/host.json") now=$(jq -r .now "$BRIDGE_RUN/instance/host.json") deny_exports=$(jq -r .deny_exports "$BRIDGE_RUN/instance/host.json")"
  echo "receipts=$(sqlite3 -readonly "$journal" 'SELECT COUNT(*) FROM receipts') runs=$(sqlite3 -readonly "$journal" 'SELECT COUNT(*) FROM task_runs')"
fi
[[ $ok == 1 ]]
