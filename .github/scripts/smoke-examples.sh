#!/usr/bin/env bash
#
# smoke-examples.sh — boots each examples/apps/*/server.yaml, hits
# /healthz, and reports pass/fail. Skips demos marked _external in
# their server.yaml (those need third-party credentials or plugin
# binaries; covered by a separate workflow).
#
# Used by .github/workflows/ci.yml. Designed to run on a fresh
# checkout. Requires `wave` binary at $WAVE_BIN (defaults to ./wave).

set -uo pipefail

WAVE_BIN="${WAVE_BIN:-./wave}"
EXAMPLES_DIR="examples/apps"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-15}"

if [ ! -x "$WAVE_BIN" ]; then
  echo "wave binary not found at $WAVE_BIN" >&2
  exit 2
fi

declare -a passed=()
declare -a failed=()
declare -a skipped=()

# Pick an ephemeral port that's free.
pick_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("",0)); print(s.getsockname()[1]); s.close()'
}

probe_health() {
  local port="$1"
  for _ in $(seq 1 "$TIMEOUT_SECONDS"); do
    if curl -fsS -o /dev/null "http://127.0.0.1:${port}/healthz"; then
      return 0
    fi
    sleep 1
  done
  return 1
}

for yaml in "$EXAMPLES_DIR"/*/server.yaml; do
  demo="$(basename "$(dirname "$yaml")")"

  # Skip demos that explicitly mark themselves as needing external services.
  if grep -q '^# *_external: *true' "$yaml" 2>/dev/null; then
    skipped+=("$demo (external)")
    continue
  fi

  port="$(pick_port)"
  echo "--- $demo on :$port ---"

  "$WAVE_BIN" serve "$yaml" --port "$port" >"/tmp/wave-$demo.log" 2>&1 &
  pid=$!

  if probe_health "$port"; then
    passed+=("$demo")
    kill "$pid" 2>/dev/null
    wait "$pid" 2>/dev/null
  else
    failed+=("$demo")
    echo "::group::Log for failed $demo"
    tail -50 "/tmp/wave-$demo.log" || true
    echo "::endgroup::"
    kill "$pid" 2>/dev/null
    wait "$pid" 2>/dev/null
  fi
done

echo
echo "============================================"
echo "PASSED : ${#passed[@]}"
echo "FAILED : ${#failed[@]}"
echo "SKIPPED: ${#skipped[@]}"
echo "============================================"

if [ "${#failed[@]}" -gt 0 ]; then
  printf '  ✗ %s\n' "${failed[@]}" >&2
  exit 1
fi

exit 0
