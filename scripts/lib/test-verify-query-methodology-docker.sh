#!/usr/bin/env bash
set -euo pipefail
printf 'docker %s\n' "$*" >> "$SHIM_LOG"
case "$1" in logs) printf 'Started.\n' ;; port) printf '127.0.0.1:12345\n' ;; esac
