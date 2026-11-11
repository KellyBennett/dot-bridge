# Sourced by the verify-bridge scripts.
VERIFY_ROOT="${TMPDIR:-/tmp}"
VERIFY_ROOT="${VERIFY_ROOT%/}/dot-bridge-verify"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
HARNESS="$REPO_ROOT/.claude/skills/verify-bridge/harness/main.go"

go_cmd() {
  if command -v go >/dev/null 2>&1; then go "$@"; else mise exec go -- go "$@"; fi
}

require_run() {
  if [[ -z "${BRIDGE_RUN:-}" ]]; then echo "BRIDGE_RUN is not set" >&2; exit 2; fi
  case "$BRIDGE_RUN" in "$VERIFY_ROOT"/*) ;; *) echo "BRIDGE_RUN must live under $VERIFY_ROOT" >&2; exit 2;; esac
  if [[ ! -f "$BRIDGE_RUN/.verify-bridge-run" ]]; then echo "$BRIDGE_RUN was not created by launch.sh" >&2; exit 2; fi
}
