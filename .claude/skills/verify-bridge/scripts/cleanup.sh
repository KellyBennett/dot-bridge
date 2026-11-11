#!/usr/bin/env bash
# Snapshots the journal into evidence, then removes the instance, binary and
# work files for $BRIDGE_RUN. $BRIDGE_RUN/evidence is never removed.
set -euo pipefail
source "$(dirname "$0")/_env.sh"
require_run
run="${BRIDGE_RUN:?}"
journal="$run/instance/journal.sqlite3"
if [[ -f "$journal" ]]; then
  for table in receipts approvals task_runs run_lifecycle run_events stub_dispatches; do
    sqlite3 -readonly -json "$journal" "SELECT * FROM $table" > "$run/evidence/final-$table.json"
  done
fi
for scratch in instance bin work; do
  rm -rf -- "${run:?}/$scratch"
done
echo "evidence kept at $run/evidence"
