#!/usr/bin/env bash
# Resolves the immutable digest for the template image after push and prints
# the manifest artifact line to pin. Usage: pin-digest.sh <repo:tag>.
set -euo pipefail
ref="${1:?usage: pin-digest.sh <repo:tag>}"
digest="$(docker inspect --format='{{index .RepoDigests 0}}' "$ref")"
echo "Pin this in manifest.yaml artifacts:"
echo "  image: ${digest}"
