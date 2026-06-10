#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

workflow=".github/workflows/docker-publish.yml"

if ! rg -F -q "IMAGE_PLATFORMS: \${{ github.event_name == 'pull_request' && 'linux/amd64' || 'linux/amd64,linux/arm64' }}" "$workflow"; then
	printf 'docker publish workflow must limit pull_request image builds to linux/amd64\n' >&2
	exit 1
fi

if ! rg -F -q 'platforms: ${{ env.IMAGE_PLATFORMS }}' "$workflow"; then
	printf 'docker publish build step must use IMAGE_PLATFORMS\n' >&2
	exit 1
fi

printf 'docker publish pull request platform guard passed\n'
