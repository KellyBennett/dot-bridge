#!/usr/bin/env bash
# Usage: probe.sh "<read-only SQL>"  -> JSON rows from the journal, saved under evidence/probes/.
set -euo pipefail
source "$(dirname "$0")/_env.sh"
require_run
mkdir -p "$BRIDGE_RUN/evidence/probes"
out="$BRIDGE_RUN/evidence/probes/$(date +%H%M%S)-$RANDOM.json"
printf '%s\n' "$1" > "${out%.json}.sql"
sqlite3 -readonly -json "$BRIDGE_RUN/instance/journal.sqlite3" "$1" | tee "$out"
