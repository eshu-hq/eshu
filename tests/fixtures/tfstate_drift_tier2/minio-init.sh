#!/bin/sh
# tests/fixtures/tfstate_drift_tier2/minio-init.sh
#
# One-shot bucket and object loader for the Tier-2 tfstate drift verifier.
# Runs inside a minio/mc container against the sibling minio service. Creates
# the four buckets the canonical drift adapter expects to see referenced by
# Git-emitted terraform_backends facts (A, B, D, E) and uploads the matching
# .tfstate JSON document to each bucket's `prod/terraform.tfstate` key.
#
# Buckets and locators mirror the address strings in
# tests/fixtures/tfstate_drift/seed.sql so the counter labels fire on the same
# fixture intent the Tier-1 verifier already asserts.

set -eu

MINIO_ENDPOINT="${MINIO_ENDPOINT:-http://minio:9000}"
MINIO_ROOT_USER="${MINIO_ROOT_USER:-minioadmin}"
MINIO_ROOT_PASSWORD="${MINIO_ROOT_PASSWORD:-minioadmin}"
STATE_DIR="${STATE_DIR:-/state}"

# Wait for minio to respond to /minio/health/live via the mc client. mc
# alias set does probe in recent releases, so loop on it directly until it
# succeeds and capture the last error if it never does.
attempts=0
last_err=""
until last_err="$(mc alias set local "$MINIO_ENDPOINT" "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" 2>&1)"; do
    attempts=$((attempts + 1))
    if [ "$attempts" -ge 90 ]; then
        echo "minio not reachable at $MINIO_ENDPOINT after $attempts attempts" >&2
        echo "last mc error: $last_err" >&2
        exit 1
    fi
    sleep 1
done

for bucket in eshu-drift-a eshu-drift-b eshu-drift-d eshu-drift-e; do
    mc mb --ignore-existing "local/${bucket}"
done

mc cp "${STATE_DIR}/drift-a.tfstate" "local/eshu-drift-a/prod/terraform.tfstate"
mc cp "${STATE_DIR}/drift-b.tfstate" "local/eshu-drift-b/prod/terraform.tfstate"
mc cp "${STATE_DIR}/drift-d.tfstate" "local/eshu-drift-d/prod/terraform.tfstate"
mc cp "${STATE_DIR}/drift-e.tfstate" "local/eshu-drift-e/prod/terraform.tfstate"

echo "minio-init: uploaded 4 terraform.tfstate objects"
