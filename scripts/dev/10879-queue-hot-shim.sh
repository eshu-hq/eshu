#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# #10879 cheapest-shim runner: generates per-row heartbeat workloads (one row
# per autocommitted transaction, round-robin, exactly like worker heartbeats),
# executes the queue-hot SQL shim against the disposable local database
# eshu_10879_shim (never shared infra), saves the raw psql log, and enforces:
# RED  = BEFORE heartbeat HOT share < 10% (the reported bug reproduces).
# GREEN = AFTER lease HOT share > 50% with zero new main-table updates during
# the heartbeat window (the theory holds on the shim).
set -euo pipefail

DB="${DB:-eshu_10879_shim}"
N="${N:-2000}"
H="${H:-25}"
OUT_DIR="${OUT_DIR:-/tmp/10879-shim}"
mkdir -p "$OUT_DIR"
LOG="$OUT_DIR/shim-$(date +%Y%m%dT%H%M%S).log"
HB_BEFORE="$OUT_DIR/hb_before.sql"
HB_AFTER="$OUT_DIR/hb_after.sql"

python3 scripts/dev/10879-queue-hot-gen-hb.py "$HB_BEFORE" "$HB_AFTER" "$N" "$H"

START_MS=$(date +%s%3N)
sudo -u postgres psql -d "$DB" -v ON_ERROR_STOP=1 \
  -v hb_before="$HB_BEFORE" -v hb_after="$HB_AFTER" \
  -f scripts/dev/10879-queue-hot-shim.sql 2>&1 | tee "$LOG"
END_MS=$(date +%s%3N)
echo "total wall ms: $((END_MS - START_MS))"
echo "raw log: $LOG"

python3 scripts/dev/10879-queue-hot-assert.py "$LOG"
