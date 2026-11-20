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

python3 - "$HB_BEFORE" "$HB_AFTER" "$N" "$H" <<'PY'
import sys
hb_before, hb_after, n, h = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
with open(hb_before, "w") as f:
    f.write("\\timing off\n")
    for _ in range(h):
        for g in range(1, n + 1):
            wid = f"'w{g:07d}'"
            f.write(
                "UPDATE q_before SET claim_until = clock_timestamp() + interval '5 minutes',"
                f" updated_at = clock_timestamp() WHERE work_item_id = {wid};\n"
            )
with open(hb_after, "w") as f:
    f.write("\\timing off\n")
    for _ in range(h):
        for g in range(1, n + 1):
            wid = f"'w{g:07d}'"
            f.write(
                "UPDATE q_lease SET claim_until = clock_timestamp() + interval '5 minutes'"
                f" WHERE work_item_id = {wid};\n"
            )
print(f"generated {n * h} renewals per shape")
PY

START_MS=$(date +%s%3N)
sudo -u postgres psql -d "$DB" -v ON_ERROR_STOP=1 \
  -v hb_before="$HB_BEFORE" -v hb_after="$HB_AFTER" \
  -f scripts/dev/10879-queue-hot-shim.sql 2>&1 | tee "$LOG"
END_MS=$(date +%s%3N)
echo "total wall ms: $((END_MS - START_MS))"
echo "raw log: $LOG"

python3 - "$LOG" <<'PY'
import re, sys
log = open(sys.argv[1]).read()

def stat_row(phase, cols=2):
    m = re.search(rf"{phase}\s*\|([^\n]+)", log)
    if not m:
        print(f"ASSERT-FAIL: no stat row for {phase}")
        sys.exit(1)
    nums = [int(x) for x in re.findall(r"\d+", m.group(1))]
    if len(nums) < cols:
        print(f"ASSERT-FAIL: short stat row for {phase}: {m.group(1)!r}")
        sys.exit(1)
    return nums[0], nums[1]

bb_upd, bb_hot = stat_row("after_before_hb")
bc_upd, bc_hot = stat_row("after_before_claim")
l_upd, l_hot = stat_row("after_after_hb")
am_upd, am_hot = stat_row("after_after_hb_main")
ac_upd, _ = stat_row("after_after_claim")

# Heartbeat-window deltas (subtract the claim-phase cumulative counts).
hb_upd, hb_hot = bb_upd - bc_upd, bb_hot - bc_hot
before_share = hb_hot / hb_upd if hb_upd else 0
lease_share = l_hot / l_upd if l_upd else 0
main_new = am_upd - ac_upd
print(f"BEFORE hb window: upd={hb_upd} hot={hb_hot} share={before_share:.4f}")
print(f"AFTER lease hb: upd={l_upd} hot={l_hot} share={lease_share:.4f}")
print(f"AFTER main new updates during hb window: {main_new}")

ok = True
if not (before_share < 0.10):
    print(f"RED-FAIL: expected BEFORE HOT share < 0.10, got {before_share:.4f}")
    ok = False
else:
    print("RED-PASS: BEFORE shape reproduces the non-HOT bug")
if not (lease_share > 0.50):
    print(f"GREEN-FAIL: expected AFTER lease HOT share > 0.50, got {lease_share:.4f}")
    ok = False
else:
    print("GREEN-PASS: AFTER lease renewals go HOT")
if not (main_new == 0):
    print(f"GREEN-FAIL: expected 0 new main updates in hb window, got {main_new}")
    ok = False
else:
    print("GREEN-PASS: heartbeat touches no main-table row")
sys.exit(0 if ok else 1)
PY
