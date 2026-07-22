#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
frontend_workflow="$repo_root/.github/workflows/frontend.yml"
tmp_root="$(mktemp -d)"
trap 'rm -rf -- "$tmp_root"' EXIT

mkdir -p "$tmp_root/bin" "$tmp_root/tmp" "$tmp_root/repo/go"

cat >"$tmp_root/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >"${FAKE_GO_LOG:?}"
output=""
while (($# > 0)); do
  if [[ "$1" == "-o" ]]; then
    output="${2:?missing output path}"
    break
  fi
  shift
done
[[ -n "$output" ]]
printf '#!/usr/bin/env bash\nexit 0\n' >"$output"
chmod +x "$output"
EOF
chmod +x "$tmp_root/bin/go"

export PATH="$tmp_root/bin:$PATH"
export TMPDIR="$tmp_root/tmp"
export FAKE_GO_LOG="$tmp_root/go.log"

# shellcheck source=scripts/lib/auth_e2e_cli.sh
source "$repo_root/scripts/lib/auth_e2e_cli.sh"

auth_e2e_cli_build "$tmp_root/repo"

[[ -x "$AUTH_E2E_CLI_BIN" ]]
[[ "$AUTH_E2E_CLI_BIN" == "$AUTH_E2E_CLI_DIR/eshu" ]]
[[ "$AUTH_E2E_CLI_DIR" == "$TMPDIR"/eshu-auth-e2e-cli.* ]]

expected="-C $tmp_root/repo/go build -trimpath -o $AUTH_E2E_CLI_BIN ./cmd/eshu"
actual="$(<"$FAKE_GO_LOG")"
[[ "$actual" == "$expected" ]] || {
  printf 'test-auth-e2e-cli: go args mismatch\nwant: %s\n got: %s\n' "$expected" "$actual" >&2
  exit 1
}

build_dir="$AUTH_E2E_CLI_DIR"
auth_e2e_cli_cleanup
[[ ! -e "$build_dir" ]]

for runner in scripts/run-auth-e2e.sh scripts/run-auth-mcp-e2e.sh; do
  rg -q 'auth_e2e_cli_build "\$repo_root"' "$repo_root/$runner"
  rg -q 'ESHU_E2E_ESHU_BINARY="\$AUTH_E2E_CLI_BIN"' "$repo_root/$runner"
  if rg -Fq 'auth_e2e_cli_cleanup || true' "$repo_root/$runner"; then
    printf 'test-auth-e2e-cli: %s ignores CLI cleanup failures\n' "$runner" >&2
    exit 1
  fi
  rg -Fq 'trap - EXIT' "$repo_root/$runner" || {
    printf 'test-auth-e2e-cli: %s does not preserve EXIT status during cleanup\n' "$runner" >&2
    exit 1
  }
done

for gate_path in scripts/lib/auth_e2e_cli.sh scripts/test-auth-e2e-cli.sh; do
  rg -Fq -- "- \"$gate_path\"" "$frontend_workflow" || {
    printf 'test-auth-e2e-cli: frontend workflow does not watch %s\n' "$gate_path" >&2
    exit 1
  }
done

rg -Fq 'run: bash scripts/test-auth-e2e-cli.sh' "$frontend_workflow" || {
  echo "test-auth-e2e-cli: frontend workflow does not run this contract test" >&2
  exit 1
}

AUTH_E2E_CLI_DIR="$tmp_root/not-owned"
if auth_e2e_cli_cleanup 2>/dev/null; then
  echo "test-auth-e2e-cli: cleanup accepted a non-owned directory" >&2
  exit 1
fi

echo "test-auth-e2e-cli: PASS"
