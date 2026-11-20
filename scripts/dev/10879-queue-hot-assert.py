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
