#!/usr/bin/env bash
# Usage: bh.sh <bridgehost command> [args]   (a final "-" reads stdin)
# Records argv, stdin, stdout, stderr and exit code under $BRIDGE_RUN/evidence/calls/.
set -uo pipefail
source "$(dirname "$0")/_env.sh"
require_run
calls="$BRIDGE_RUN/evidence/calls"
mkdir -p "$calls"
n=$(printf '%03d' "$(find "$calls" -mindepth 1 -maxdepth 1 -type d | wc -l)")
call="$calls/$n-$1"
mkdir -p "$call"
printf '%q ' "$@" > "$call/argv.txt"
if [[ "${!#}" == "-" ]]; then cat > "$call/stdin"; else : > "$call/stdin"; fi
if [[ $# -ge 2 && -f "${!#}" ]]; then cp "${!#}" "$call/input"; fi
"$BRIDGE_RUN/bin/bridgehost" "$@" < "$call/stdin" > "$call/stdout" 2> "$call/stderr"
status=$?
echo "$status" > "$call/exit"
cat "$call/stdout"; cat "$call/stderr" >&2
exit "$status"
