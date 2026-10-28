#!/bin/sh
# tests/fixtures/tfstate_drift_tier2/v25/minio-init-gen2.sh
#
# Tier-2 v2.5 gen-2 loader: invoked between collector passes by the v2.5
# verifier (typically via `docker compose run --rm minio-init-gen2`). Overwrites
# the same `prod/terraform.tfstate` keys in eshu-drift-c and eshu-drift-f with
# the serial=2 gen-2 documents.
#
# Bucket C gen-2 keeps the same lineage as gen-1 with serial bumped to 2 and
# the aws_s3_bucket.cached resource removed; bucket F state stays empty across
# both generations because bucket F drift is config-side only.

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

mc cp "${STATE_DIR}/drift-c.tfstate" "local/eshu-drift-c/prod/terraform.tfstate"
mc cp "${STATE_DIR}/drift-f.tfstate" "local/eshu-drift-f/prod/terraform.tfstate"

echo "minio-init-gen2: overwrote 2 terraform.tfstate objects (gen-2)"
