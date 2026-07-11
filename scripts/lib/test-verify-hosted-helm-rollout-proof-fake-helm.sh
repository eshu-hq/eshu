#!/usr/bin/env bash
set -euo pipefail
cmd="$1"
shift
case "${cmd}" in
  lint)
    printf 'lint ok\n'
    ;;
  template)
    # printf (a builtin, no pipe) instead of a heredoc: this fixture's body
    # is itself over 512 bytes, and a nested heredoc here would deadlock
    # under Homebrew bash >= 5.1's pipe-buffer heredoc write (#5074).
    printf '%s\n' \
      'apiVersion: apps/v1' \
      'kind: Deployment' \
      'metadata:' \
      '  name: eshu-api' \
      'spec:' \
      '  template:' \
      '    spec:' \
      '      containers:' \
      '        - name: api' \
      '          image: "ghcr.io/eshu-hq/eshu:v9.9.9"' \
      '---' \
      'apiVersion: apps/v1' \
      'kind: Deployment' \
      'metadata:' \
      '  name: eshu-mcp-server' \
      '---' \
      'apiVersion: apps/v1' \
      'kind: StatefulSet' \
      'metadata:' \
      '  name: eshu' \
      '---' \
      'apiVersion: apps/v1' \
      'kind: Deployment' \
      'metadata:' \
      '  name: eshu-resolution-engine' \
      '---' \
      'apiVersion: batch/v1' \
      'kind: Job' \
      'metadata:' \
      '  name: eshu-schema-bootstrap' \
      '  annotations:' \
      '    "helm.sh/hook": pre-install,pre-upgrade'
    ;;
  upgrade)
    printf 'DRY RUN\n'
    ;;
  *)
    printf 'unexpected helm command: %s\n' "${cmd}" >&2
    exit 1
    ;;
esac
