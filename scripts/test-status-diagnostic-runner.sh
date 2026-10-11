#!/usr/bin/env bash
# Exercise owned-child shutdown and artifact validation ordering.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
. "${repo_root}/scripts/lib/status-diagnostic-runner.sh"
work="$(mktemp -d)"
child=""
cleanup() {
 [[ -z "$child" ]] || kill "$child" 2>/dev/null || true
 rm -rf "$work"
}
trap cleanup EXIT
python3 - "$work" <<'PY' &
import pathlib
import signal
import sys
import time
root = pathlib.Path(sys.argv[1])
def stop(_signal, _frame):
    time.sleep(0.1)
    (root / "flushed").write_text("complete", encoding="utf-8")
    raise SystemExit(0)
signal.signal(signal.SIGTERM, stop)
(root / "ready").touch()
while True:
    time.sleep(0.05)
PY
child=$!
for _ in {1..100}; do
 [[ ! -f "$work/ready" ]] || break
 sleep 0.01
done
[[ -f "$work/ready" ]]
status_diagnostic_stop "$child" 3
child=""
[[ -f "$work/flushed" ]]
if status_diagnostic_validate "$repo_root" "$work" 20 >/dev/null; then
 printf 'FAIL: missing artifacts accepted\n' >&2
 exit 1
fi
# A child that ignores TERM must be bounded and reported as a failure.
python3 - "$work" <<'PYCHILD' &
import pathlib
import signal
import sys
import time
signal.signal(signal.SIGTERM, signal.SIG_IGN)
(pathlib.Path(sys.argv[1]) / "ignore-ready").touch()
while True:
    time.sleep(0.05)
PYCHILD
child=$!
for _ in {1..100}; do
 [[ ! -f "$work/ignore-ready" ]] || break
 sleep 0.01
done
[[ -f "$work/ignore-ready" ]]
if status_diagnostic_stop "$child" 1; then
 printf 'FAIL: shutdown timeout accepted\n' >&2
 exit 1
fi
if kill -0 "$child" 2>/dev/null; then
 printf 'FAIL: timed out child remains alive\n' >&2
 exit 1
fi
child=""
status_diagnostic_capacity 20 3 20
status_diagnostic_capacity 234 1 20
if status_diagnostic_capacity 235 1 20 >/dev/null; then
 printf 'FAIL: collector overflow configuration accepted\n' >&2
 exit 1
fi
status_diagnostic_prepare "$work/report"
printf 'immutable' > "$work/report/marker"
if status_diagnostic_prepare "$work/report" 2>/dev/null; then
 printf 'FAIL: preexisting report directory accepted\n' >&2
 exit 1
fi
[[ "$(cat "$work/report/marker")" == immutable ]]
ln -s "$work/report" "$work/report-link"
if status_diagnostic_prepare "$work/report-link" 2>/dev/null; then
 printf 'FAIL: symlink report directory accepted\n' >&2
 exit 1
fi
printf 'PASS: owned child flush, missing artifact rejection, and bounded timeout\n'
