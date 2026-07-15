#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="$repo_root/scripts/run-console-live-e2e.sh"
workflow="$repo_root/.github/workflows/frontend.yml"
registry="$repo_root/specs/ci-gates.v1.yaml"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

command -v rg >/dev/null 2>&1 || {
  echo "run-console-live-e2e contract requires rg" >&2
  exit 1
}

rg -q --fixed-strings 'sudo apt-get install --yes ripgrep' "$workflow" || {
  echo "frontend console job must install rg before running harness contracts" >&2
  exit 1
}
install_line="$(rg -n -m 1 --fixed-strings 'sudo apt-get install --yes ripgrep' "$workflow")"
harness_line="$(rg -n -m 1 --fixed-strings 'bash scripts/test-run-console-live-e2e.sh' "$workflow")"
if (( ${install_line%%:*} >= ${harness_line%%:*} )); then
  echo "frontend console job must install rg before running harness contracts" >&2
  exit 1
fi

marker="$tmp_dir/env-file-executed"
env_file="$tmp_dir/compose.env"
fake_bin="$tmp_dir/bin"
mkdir -p "$fake_bin"
printf 'ESHU_API_KEY=$(touch %s)\nESHU_HTTP_PORT=9999\n' "$marker" >"$env_file"

for command in npx node; do
  printf '#!/usr/bin/env bash\nexit 0\n' >"$fake_bin/$command"
  chmod +x "$fake_bin/$command"
done
printf '#!/usr/bin/env bash\nprintf 200\n' >"$fake_bin/curl"
chmod +x "$fake_bin/curl"

PATH="$fake_bin:$PATH" \
  ESHU_CONSOLE_E2E_ENV_FILE="$env_file" \
  ESHU_E2E_AUTH_MODE=browser_session \
  ESHU_E2E_API_BASE=http://127.0.0.1:9080 \
  bash "$target" >/dev/null

[[ ! -e "$marker" ]] || {
  echo "run-console-live-e2e executed command substitution from Compose env input" >&2
  exit 1
}

rg -q --fixed-strings 'source "$env_file"' "$target" && {
  echo "run-console-live-e2e must not source Compose env input" >&2
  exit 1
}

for harness_input in \
  'scripts/run-console-live-e2e.sh' \
  'scripts/run-console-retained-e2e.sh' \
  'scripts/test-run-console-live-e2e.sh' \
  'scripts/test-run-console-retained-e2e.sh' \
  'scripts/console-live-e2e-runtime.mjs'; do
  rg -q --fixed-strings "$harness_input" "$workflow" || {
    echo "frontend workflow does not watch live harness input: $harness_input" >&2
    exit 1
  }
  rg -q --fixed-strings "$harness_input" "$registry" || {
    echo "frontend gate registry does not watch live harness input: $harness_input" >&2
    exit 1
  }
done

for harness_test in \
  'bash scripts/test-run-console-live-e2e.sh' \
  'bash scripts/test-run-console-retained-e2e.sh'; do
  rg -q --fixed-strings "$harness_test" "$workflow" || {
    echo "frontend workflow does not execute live harness test: $harness_test" >&2
    exit 1
  }
  rg -q --fixed-strings "$harness_test" "$registry" || {
    echo "frontend gate registry does not execute live harness test: $harness_test" >&2
    exit 1
  }
done

echo "run-console-live-e2e env isolation contract: PASS"
