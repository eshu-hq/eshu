#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="$repo_root/scripts/run-console-live-e2e.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

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

echo "run-console-live-e2e env isolation contract: PASS"
