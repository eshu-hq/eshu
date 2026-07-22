#!/usr/bin/env bash

# Shared exact-source CLI build for the fresh-stack auth gates. The browser
# runner invokes this binary only after the build completes, so its credential
# retrieval timeout measures the local Postgres operation rather than a cold Go
# compilation.

AUTH_E2E_CLI_DIR=""
AUTH_E2E_CLI_BIN=""

auth_e2e_cli_build() {
  local repo_root="${1:?repository root is required}"
  local temp_parent="${TMPDIR:-/tmp}"
  temp_parent="${temp_parent%/}"

  if [[ -n "$AUTH_E2E_CLI_DIR" ]]; then
    echo "auth-e2e-cli: build directory is already initialized" >&2
    return 1
  fi

  AUTH_E2E_CLI_DIR="$(mktemp -d "${temp_parent}/eshu-auth-e2e-cli.XXXXXX")"
  AUTH_E2E_CLI_BIN="${AUTH_E2E_CLI_DIR}/eshu"
  if ! go -C "${repo_root}/go" build -trimpath -o "$AUTH_E2E_CLI_BIN" ./cmd/eshu; then
    auth_e2e_cli_cleanup || true
    return 1
  fi
}

auth_e2e_cli_cleanup() {
  if [[ -z "$AUTH_E2E_CLI_DIR" ]]; then
    return 0
  fi

  local temp_parent="${TMPDIR:-/tmp}"
  temp_parent="${temp_parent%/}"
  case "$AUTH_E2E_CLI_DIR" in
    "${temp_parent}"/eshu-auth-e2e-cli.*)
      rm -rf -- "$AUTH_E2E_CLI_DIR"
      AUTH_E2E_CLI_DIR=""
      AUTH_E2E_CLI_BIN=""
      ;;
    *)
      echo "auth-e2e-cli: refusing to remove non-owned directory" >&2
      return 1
      ;;
  esac
}
