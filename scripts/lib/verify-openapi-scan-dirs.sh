#!/usr/bin/env bash
# verify-openapi-scan-dirs.sh — route-source directory registry for
# verify-openapi.sh. Sourced after the caller sets repo_root. Declares the
# query, service-intelligence, and CLI-support scan directories, fails closed
# when any is absent (a missing source can hide route drift from the
# comparison), and exports the scan_dirs array.
#
# Shellcheck source directive for callers:
# shellcheck source=verify-openapi-scan-dirs.sh

# The query package is both a route source and the sole owner of the
# OpenAPI contract fragments. Without it, the verifier has no
# contract surface to compare and must fail closed even if another route
# source directory exists.
query_dir="${repo_root}/go/internal/query"
si_dir="${repo_root}/go/internal/serviceintelhttp"
support_dir="${repo_root}/go/internal/cli/support"

if [ ! -d "$query_dir" ]; then
  printf '%s\n' 'OPENAPI SCAN FAILED: required OpenAPI contract source directory not found' >&2
  printf 'Expected:\n  %s\n' "$query_dir" >&2
  exit 1
fi

# The service-intelligence package is the second declared route source. Its
# absence can hide a route from the comparison just as a missing query package
# can hide the contract surface.
if [ ! -d "$si_dir" ]; then
  printf '%s\n' 'OPENAPI SCAN FAILED: required OpenAPI route source directory not found' >&2
  printf 'Expected:\n  %s\n' "$si_dir" >&2
  exit 1
fi

# The CLI-support package is the third declared route source (8874 S4). Its
# absence hides the thin-client endpoints from the comparison the same way.
if [ ! -d "$support_dir" ]; then
  printf '%s\n' 'OPENAPI SCAN FAILED: required OpenAPI route source directory not found' >&2
  printf 'Expected:\n  %s\n' "$support_dir" >&2
  exit 1
fi

# All declared input surfaces are present. They may contain zero matching Go
# files; an existing-but-empty synthetic repository is a valid empty surface.
scan_dirs=("$query_dir" "$si_dir" "$support_dir")
