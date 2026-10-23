#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
# Throwaway #8617 measurement shim; never a replacement for a CI gate.
set -euo pipefail
phase="${1:?phase required}"
proof_dir=/tmp/eshu-lint-proof
mkdir -p "$proof_dir"
case "$phase" in
  measure)
    cd go
    git rev-parse HEAD > "$proof_dir/head.txt"
    go version > "$proof_dir/versions.txt"
    golangci-lint version >> "$proof_dir/versions.txt"
    nproc > "$proof_dir/cpus.txt"
    for sample in seed cold-control; do
      cache="$proof_dir/seed"
      if [[ "$sample" = cold-control ]]; then cache="$proof_dir/control"; fi
      cat /proc/loadavg > "$proof_dir/$sample.load-start"
      GOLANGCI_LINT_CACHE="$cache" /usr/bin/time -v -o "$proof_dir/$sample.time" \
        golangci-lint run -v ./... > "$proof_dir/$sample.log" 2>&1
      cat /proc/loadavg > "$proof_dir/$sample.load-end"
    done
    du -sb "$proof_dir/seed" > "$proof_dir/cache-bytes.txt"
    ;;
  clear)
    test -d "$proof_dir/seed"
    rm -rf -- "$proof_dir/seed"
    ;;
  restored)
    cd go
    test -d "$proof_dir/seed"
    GOLANGCI_LINT_CACHE="$proof_dir/seed" /usr/bin/time -v -o "$proof_dir/restored.time" \
      golangci-lint run -v ./... > "$proof_dir/restored.log" 2>&1
    ;;
  invalidation)
    cd go
    fixture=internal/query/entity/cache_proof.go
    config=.golangci-cache-proof.yml
    test ! -e "$fixture" && test ! -e "$config"
    trap 'rm -f -- "$fixture" "$config"' EXIT
    printf '%s\n' 'package entity' 'import "fmt"' 'func CacheProofViolation() { fmt.Printf("%d", "wrong") }' > "$fixture"
    export GOLANGCI_LINT_CACHE="$proof_dir/seed"
    if golangci-lint run ./internal/query/entity > "$proof_dir/source-invalid.log" 2>&1; then
      echo 'source violation was hidden by cache' >&2; exit 1
    fi
    rg -qi 'printf|wrong type' "$proof_dir/source-invalid.log"
    printf '%s\n' 'version: "2"' 'linters:' '  default: none' '  enable: [unused]' > "$config"
    golangci-lint run --config "$config" ./internal/query/entity > "$proof_dir/config-disabled.log" 2>&1
    printf '%s\n' 'version: "2"' 'linters:' '  default: none' '  enable: [govet]' > "$config"
    if golangci-lint run --config "$config" ./internal/query/entity > "$proof_dir/config-enabled.log" 2>&1; then
      echo 'configuration change was hidden by cache' >&2; exit 1
    fi
    rg -qi 'printf|wrong type' "$proof_dir/config-enabled.log"
    ;;
  *) echo "unknown proof phase: $phase" >&2; exit 2 ;;
esac
