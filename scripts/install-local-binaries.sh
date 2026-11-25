#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
GO_DIR="$REPO_ROOT/go"
# #9066: cgo needs -std=gnu17 on GCC 15 + glibc 2.43; see scripts/lib/cgo-cflags.sh.
# shellcheck source=lib/cgo-cflags.sh
source "$REPO_ROOT/scripts/lib/cgo-cflags.sh"
eshu_export_cgo_cflags

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
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-bootstrap-index" ./cmd/bootstrap-index
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-core-api" ./cmd/core-api
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-ingester" ./cmd/ingester
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-reducer" ./cmd/reducer
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-workflow-coordinator" ./cmd/workflow-coordinator
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-projector" ./cmd/projector
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-collector-git" ./cmd/collector-git
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-collector-component-extension" ./cmd/collector-component-extension
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-collector-cicd-run" ./cmd/collector-cicd-run
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-webhook-intake" ./cmd/webhook-intake
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-bootstrap-data-plane" ./cmd/bootstrap-data-plane
    go build -trimpath -ldflags="$LDFLAGS" -o "$INSTALL_DIR/eshu-admin-status" ./cmd/admin-status

    # #9670: the in-tree mcp-server moved to the eshu-mcp satellite. Local
    # launchers (`eshu mcp start`, the local supervisor, `eshu doctor`) still
    # resolve `eshu-mcp-server` from PATH, so the installer builds the pinned
    # satellite release under that name. Same tag pin as the gates
    # (scripts/lib/golden-corpus-host-helpers.sh); override with ESHU_MCP_REF
    # only to measure another satellite release.
    : "${ESHU_MCP_REF:=v0.2.14}"
    MCP_SRC_DIR="$(mktemp -d)"
    git clone --depth 1 --branch "$ESHU_MCP_REF" https://github.com/eshu-hq/eshu-mcp.git "$MCP_SRC_DIR"
    echo "eshu-mcp $ESHU_MCP_REF resolved to $(git -C "$MCP_SRC_DIR" rev-parse HEAD)"
    CGO_ENABLED=0 go -C "$MCP_SRC_DIR" build -trimpath -o "$INSTALL_DIR/eshu-mcp-server" ./cmd/eshu-mcp
    rm -rf "$MCP_SRC_DIR"

    # #11453: the in-tree api server moved to the eshu-api satellite. Local
    # launchers still resolve `eshu-api` from PATH, so the installer builds
    # the pinned satellite release under that name. ESHU_API_REF pins the
    # promoted build commit (a sha tag has no git ref, so fetch it by SHA);
    # override only to measure another satellite build. The satellite release
    # builds with CGO_ENABLED=0.
    : "${ESHU_API_REF:=4565809840f689d552d2674a771e28a35326359c}"
    API_SRC_DIR="$(mktemp -d)"
    git init -q "$API_SRC_DIR"
    git -C "$API_SRC_DIR" remote add origin https://github.com/eshu-hq/eshu-api.git
    git -C "$API_SRC_DIR" fetch --depth 1 origin "$ESHU_API_REF"
    git -C "$API_SRC_DIR" checkout -q FETCH_HEAD
    echo "eshu-api $ESHU_API_REF resolved to $(git -C "$API_SRC_DIR" rev-parse HEAD)"
    CGO_ENABLED=0 go -C "$API_SRC_DIR" build -trimpath -o "$INSTALL_DIR/eshu-api" ./cmd/eshu-api
    rm -rf "$API_SRC_DIR"

    echo "Installed Eshu binaries to $INSTALL_DIR"
    if [[ -n "$ESHU_BUILD_TAGS" ]]; then
        echo "Built local owner eshu with Go tags: $ESHU_BUILD_TAGS"
    fi
    echo "Make sure this directory is on PATH before running eshu graph start or eshu doctor."
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
