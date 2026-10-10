#!/usr/bin/env bash
# Verify the failed fault matrix left exactly one nonempty completion marker.
set -euo pipefail
shopt -s nullglob
completeness=(/tmp/ifa-fault-injection.*/diagnostics-complete)
[[ ${#completeness[@]} -eq 1 && -s "${completeness[0]}" ]]
