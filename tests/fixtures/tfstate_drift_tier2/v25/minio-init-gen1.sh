#!/bin/sh
# tests/fixtures/tfstate_drift_tier2/v25/minio-init-gen1.sh
#
# Tier-2 v2.5 gen-1 loader: creates the bucket-C and bucket-F buckets in MinIO
# and uploads the gen-1 .tfstate documents at `prod/terraform.tfstate`. Runs
# during the first compose `up` so collector-instance-1 sees serial=1 state
# for bucket C (resource present) and empty state for bucket F.
#
# Sibling minio-init-gen2.sh overwrites the same object keys with serial=2
# state between collector passes.

set -eu

MINIO_ENDPOINT="${MINIO_ENDPOINT:-http://minio:9000}"
MINIO_ROOT_USER="${MINIO_ROOT_USER:-minioadmin}"
MINIO_ROOT_PASSWORD="${MINIO_ROOT_PASSWORD:-minioadmin}"
STATE_DIR="${STATE_DIR:-/state}"

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

for bucket in eshu-drift-c eshu-drift-f; do
    mc mb --ignore-existing "local/${bucket}"
done

mc cp "${STATE_DIR}/drift-c.tfstate" "local/eshu-drift-c/prod/terraform.tfstate"
mc cp "${STATE_DIR}/drift-f.tfstate" "local/eshu-drift-f/prod/terraform.tfstate"

echo "minio-init-gen1: uploaded 2 terraform.tfstate objects (gen-1)"
