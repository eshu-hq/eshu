#!/usr/bin/env bash
# Runs the template's conformance gate: module tidiness, build, vet, full tests.
set -euo pipefail
cd "$(dirname "$0")/.."
go mod tidy -diff
go build ./...
go vet ./...
go test ./... -count=1
