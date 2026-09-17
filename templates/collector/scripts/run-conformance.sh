#!/usr/bin/env bash
# Runs the template's conformance gate: build, vet, full tests.
set -euo pipefail
cd "$(dirname "$0")/.."
go build ./...
go vet ./...
go test ./... -count=1
