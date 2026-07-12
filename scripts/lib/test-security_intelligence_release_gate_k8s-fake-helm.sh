#!/usr/bin/env bash
set -euo pipefail
if [ "$*" != "get values eshu -n eshu" ]; then
    if [ "$*" = "get manifest eshu -n eshu" ]; then
        # printf, not a heredoc: this 516-byte body sits in the 512B-64KB
        # Homebrew-bash-5.1+ pipe-deadlock zone (#5074). It has no variable
        # expansion, so every line is single-quoted.
        printf '%s\n' \
        'apiVersion: apps/v1' \
        'kind: Deployment' \
        'metadata:' \
        '  name: private-release-api' \
        'spec:' \
        '  template:' \
        '    spec:' \
        '      containers:' \
        '        - name: api' \
        '          image: registry.private.example.com/eshu/api:dev' \
        '---' \
        'apiVersion: monitoring.coreos.com/v1' \
        'kind: ServiceMonitor' \
        'metadata:' \
        '  name: private-release-api' \
        'spec:' \
        '  endpoints:' \
        '    - path: /metrics' \
        '---' \
        'apiVersion: policy/v1' \
        'kind: PodDisruptionBudget' \
        'metadata:' \
        '  name: private-release-api' \
        '---' \
        'apiVersion: batch/v1' \
        'kind: Job' \
        'metadata:' \
        '  name: private-release-schema-bootstrap'
        exit 0
    fi
    printf 'unexpected helm args: %s\n' "$*" >&2
    exit 1
fi
cat <<'YAML'
api:
  env:
    PROVIDER_URL: https://provider.private.example.com/path
    TOKEN: github_pat_secret
    WORKSPACE: /Users/alice/private/repo
    PASSWORD: hunter2
    AWS_SECRET_ACCESS_KEY: aws-secret
    apiKey: api-key-secret
    client_secret: client-secret
    Authorization: Bearer bearer-secret
    ROLE_ARN: arn:aws:iam::123456789012:role/private-role
    ACCOUNT_ID: "123456789012"
YAML
