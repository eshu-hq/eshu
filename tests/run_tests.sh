#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
GO_DIR="$REPO_ROOT/go"

UNIT_PACKAGES=(
    ./internal/parser
    ./internal/query
    ./internal/runtime
    ./internal/reducer
    ./internal/projector
)

INTEGRATION_PACKAGES=(
    ./cmd/eshu
    ./cmd/api
    ./cmd/mcp-server
    ./cmd/bootstrap-index
    ./cmd/ingester
    ./cmd/reducer
)

EXTENDED_PACKAGES=(
    ./internal/terraformschema
    ./internal/relationships
    ./internal/status
    ./internal/storage/postgres
)

run() {
    echo
    echo "==> $*"
    "$@"
}

run_go_test() {
    local packages=("$@")
    (
        cd "$GO_DIR"
        go test "${packages[@]}" -count=1
    )
}

run_docs() {
    run uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
        mkdocs build --strict --clean --config-file "$REPO_ROOT/docs/mkdocs.yml"
}

run_diff_check() {
    (
        cd "$REPO_ROOT"
        git diff --check
    )
}

run_e2e() {
    (
        cd "$REPO_ROOT"
        ./scripts/verify_relationship_platform_compose.sh
    )
}

run_fixtures() {
    (
        cd "$REPO_ROOT"
        ./scripts/verify_product_truth_fixtures.sh
    )
}

print_usage() {
    cat <<'EOF'
Usage: ./tests/run_tests.sh [option]

Options:
  unit         Run focused Go package tests
  integration  Run Go CLI/runtime integration package tests
  fixtures     Run the fast product-truth fixture contract check
  e2e          Run the relationship-platform compose proof
  docs         Run the strict MkDocs build
  fast         Run unit + integration Go tests
  all          Run fast + extended Go tests + docs + diff check + e2e
  help         Show this message
EOF
}

main() {
    local mode="${1:-all}"

    case "$mode" in
        unit|1)
            run_go_test "${UNIT_PACKAGES[@]}"
            ;;
        integration|int|2)
            run_go_test "${INTEGRATION_PACKAGES[@]}"
            ;;
        fixtures)
            run_fixtures
            ;;
        e2e|3)
            run_e2e
            ;;
        docs)
            run_docs
            ;;
        fast)
            run_go_test "${UNIT_PACKAGES[@]}"
            run_go_test "${INTEGRATION_PACKAGES[@]}"
            ;;
        all)
            run_go_test "${UNIT_PACKAGES[@]}"
            run_go_test "${INTEGRATION_PACKAGES[@]}"
            run_go_test "${EXTENDED_PACKAGES[@]}"
            run_fixtures
            run_docs
            run_diff_check
            run_e2e
            ;;
        help|-h|--help)
            print_usage
            ;;
        *)
            echo "Unknown option: $mode" >&2
            print_usage >&2
            exit 1
            ;;
    esac

    echo
    echo "Verification slice completed."
}

main "$@"
