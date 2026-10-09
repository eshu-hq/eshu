#!/usr/bin/env bash
# Exercise the workflow's real scale step without Docker or a live gate.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/scripts" "$work/bin"
workflow="$repo_root/.github/workflows/read-api-latency-gate.yml"
start="$(rg -n -m1 '^      - name: Run scale and concurrent HTTP/MCP proof$' "$workflow" | cut -d: -f1)"
end="$(rg -n -m1 '^      - name: Archive scale proof$' "$workflow" | cut -d: -f1)"
[[ -n "$start" && -n "$end" && "$end" -gt "$start" ]] || { printf 'scale workflow step missing\n' >&2; exit 1; }
offset="$(sed -n "${start},${end}p" "$workflow" | rg -n -m1 '^        run: \|$' | cut -d: -f1)"
[[ -n "$offset" ]] || { printf 'scale workflow run block missing\n' >&2; exit 1; }
run_line=$((start + offset - 1))
sed -n "$((run_line + 1)),$((end - 1))p" "$workflow" | sed 's/^          //' > "$work/step.sh"
cat > "$work/scripts/verify-read-api-latency-gate.sh" <<'SH'
#!/usr/bin/env bash
if [[ "$DOCKER_MODE" == fail-after-sample ]]; then
 for _ in {1..100}; do
  [[ ! -e "$RUNNER_TEMP/stats-failed" ]] || break
  /bin/sleep 0.01
 done
 [[ -e "$RUNNER_TEMP/stats-failed" ]] || exit 24
else
 /bin/sleep 0.15
fi
[[ "${GATE_MODE:-pass}" != fail ]] || exit 23
SH
cat > "$work/bin/docker" <<'SH'
#!/usr/bin/env bash
[[ "$1" == stats ]] || exit 99
case "$DOCKER_MODE" in
 valid) printf 'eshu-api CPU=1.00%% memory=10MiB / 1GiB\n' ;;
 empty) ;;
 fail-after-sample)
  if [[ -e "$RUNNER_TEMP/stats-seen" ]]; then touch "$RUNNER_TEMP/stats-failed"; exit 7; fi
  touch "$RUNNER_TEMP/stats-seen"
  printf 'eshu-api CPU=1.00%% memory=10MiB / 1GiB\n' ;;
esac
SH
cat > "$work/bin/sleep" <<'SH'
#!/usr/bin/env bash
/bin/sleep 0.005
SH
chmod +x "$work/scripts/verify-read-api-latency-gate.sh" "$work/bin/docker" "$work/bin/sleep"
run_case() {
 local name="$1" docker_mode="$2" gate_mode="$3" expected="$4" status=0
 mkdir -p "$work/$name"
 (cd "$work" && RUNNER_TEMP="$work/$name" DOCKER_MODE="$docker_mode" GATE_MODE="$gate_mode" PATH="$work/bin:$PATH" bash --noprofile --norc -e -o pipefail "$work/step.sh") > "$work/$name.out" 2>&1 || status=$?
 [[ -s "$work/$name/methodology-resources.txt" ]] || { printf '%s lost resource report\n' "$name" >&2; exit 1; }
 if [[ "$expected" == pass && "$status" -ne 0 || "$expected" == fail && "$status" -eq 0 ]]; then
  printf '%s: unexpected exit %s (expected %s)\n' "$name" "$status" "$expected" >&2
  exit 1
 fi
 printf '%s: %s (exit %s)\n' "$name" "$expected" "$status"
}
run_case valid valid pass pass
run_case missing-sample empty pass fail
run_case observer-failure fail-after-sample pass fail
rg -q 'eshu-api CPU=1.00% memory=' "$work/observer-failure/methodology-resources.txt" || { printf 'observer failure fixture did not sample first\n' >&2; exit 1; }
run_case gate-failure valid fail fail
printf 'test-methodology-scale-resource-observer: pass\n'
