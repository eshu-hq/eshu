#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
GO_DIR="$REPO_ROOT/go"

resolve_install_dir() {
    if [[ -n "${GOBIN:-}" ]]; then
        printf '%s\n' "$GOBIN"
        return
    fi

    local gopath_value
    local first_gopath
    gopath_value="$(go env GOPATH)"
    if [[ -z "$gopath_value" ]]; then
        echo "go env GOPATH returned an empty value; set GOBIN and retry." >&2
        exit 1
    fi

    IFS=':' read -r first_gopath _ <<< "$gopath_value"
    if [[ -z "$first_gopath" ]]; then
        echo "go env GOPATH did not include a usable first path; set GOBIN and retry." >&2
        exit 1
    fi

    printf '%s/bin\n' "$first_gopath"
}

main() {
    INSTALL_DIR="$(resolve_install_dir)"

    mkdir -p "$INSTALL_DIR"

    VERSION="${ESHU_VERSION:-dev}"
    LDFLAGS="-X github.com/eshu-hq/eshu/go/internal/buildinfo.Version=${VERSION}"
    ESHU_BUILD_TAGS="${ESHU_LOCAL_OWNER_BUILD_TAGS-nolocalllm}"
    ESHU_BUILD_TAG_ARGS=()
    if [[ -n "$ESHU_BUILD_TAGS" ]]; then
        ESHU_BUILD_TAG_ARGS=(-tags "$ESHU_BUILD_TAGS")
    fi

    cd "$GO_DIR"

    go build "${ESHU_BUILD_TAG_ARGS[@]}" -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu" ./cmd/eshu
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-api" ./cmd/api
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-mcp-server" ./cmd/mcp-server
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-bootstrap-index" ./cmd/bootstrap-index
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-ingester" ./cmd/ingester
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-reducer" ./cmd/reducer
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-workflow-coordinator" ./cmd/workflow-coordinator
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-projector" ./cmd/projector
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-collector-git" ./cmd/collector-git
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-collector-confluence" ./cmd/collector-confluence
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-collector-terraform-state" ./cmd/collector-terraform-state
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-bootstrap-data-plane" ./cmd/bootstrap-data-plane
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-admin-status" ./cmd/admin-status

    echo "Installed Eshu binaries to $INSTALL_DIR"
    if [[ -n "$ESHU_BUILD_TAGS" ]]; then
        echo "Built local owner eshu with Go tags: $ESHU_BUILD_TAGS"
    fi
    echo "Make sure this directory is on PATH before running eshu graph start or eshu doctor."
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
