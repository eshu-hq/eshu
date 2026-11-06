#!/usr/bin/env python3
# THROWAWAY PROOF SHIM for eshu#10269 design note. NOT production code.
# Never import, never wire, never ship. Delete freely once #10269 lands
# its real Go/Postgres proof.
#
# What this models (semantics read from code at origin/main fcc196d1d1
# plus the unmerged #10106 lane f700db8d0d, see the design note):
#   - One crossplane_satisfied_by_materialization row per
#     (scope, generation): reducerWorkItemID embeds generation_id and
#     enqueue is ON CONFLICT (work_item_id) DO NOTHING.
#   - Post-#10106, every such row that claims executes the handler
#     against the scope's ACTIVE snapshot (rebind), so per-generation
#     rows for one scope are functionally identical executions.
#   - The (scope, scope_id) conflict fence serializes executions per
#     scope; claim order does not change execution COUNT.
#   - Design A (prune-in-sweep): an unleased row (pending, retrying,
#     failed, dead_letter) is superseded iff a sibling row for the same
#     (scope, domain) exists on a strictly newer generation with status
#     in {pending, retrying} (the conservative carrier set).
#   - Alternative B (candidate newest-only filter, non-terminalizing):
#     stale rows are skipped while a newer claimable sibling exists,
#     but become claimable again once the survivor succeeds.
# Counts handler executions and sweep writes only. No wall time.

CLAIMABLE = {"pending", "retrying"}
UNLEASED = {"pending", "retrying", "failed", "dead_letter"}
CARRIER = {"pending", "retrying"}  # conservative: excludes claimed/running


class Row:
    def __init__(self, scope, gen, status="pending"):
        self.scope, self.gen, self.status = scope, gen, status

    def __repr__(self):
        return f"{self.scope}:g{self.gen}:{self.status}"


def prune_pass(rows):
    """One claim-statement sweep under Design A. Returns rows pruned."""
    newest_carrier = {}
    for r in rows:
        if r.status in CARRIER:
            newest_carrier[r.scope] = max(newest_carrier.get(r.scope, -1), r.gen)
    pruned = []
    for r in rows:
        if r.status in UNLEASED and newest_carrier.get(r.scope, -1) > r.gen:
            r.status = "superseded"
            pruned.append(r)
    return pruned


def drain(rows, mode):
    """Run claim/execute cycles until no claimable row remains.

    Returns (executions, supersede_writes). One execution = one full
    handler run: active-snapshot fact load + extract + retract/write.
    """
    executions = writes = 0
    while True:
        if mode == "prune":
            writes += len(prune_pass(rows))
        # Conflict fence: at most one execution per scope per cycle;
        # scopes proceed in parallel. Count is order-independent.
        by_scope = {}
        for r in rows:
            if r.status in CLAIMABLE:
                by_scope.setdefault(r.scope, []).append(r)
        if not by_scope:
            return executions, writes
        for scope_rows in by_scope.values():
            if mode == "filter":
                nxt = max(scope_rows, key=lambda r: r.gen)  # newest only
            else:
                nxt = min(scope_rows, key=lambda r: r.gen)  # oldest first
            nxt.status = "succeeded"
            executions += 1


def fresh(scenario):
    return [Row(s, g, st) for (s, g, st) in scenario]


def table():
    print("== Backlog shapes: S scopes x G queued generations, all pending ==")
    print(f"{'S x G':>10} {'rows':>7} {'no-coalesce':>12} {'prune(A)':>9} "
          f"{'sweep writes':>13} {'filter(B)':>10}")
    for (s_count, g_count) in [(1, 10), (10, 10), (100, 10), (900, 25)]:
        scen = [(f"s{i}", g, "pending") for i in range(s_count) for g in range(g_count)]
        e0, _ = drain(fresh(scen), "none")
        e1, w1 = drain(fresh(scen), "prune")
        e2, _ = drain(fresh(scen), "filter")
        print(f"{s_count:>4} x {g_count:<4} {len(scen):>7} {e0:>12} {e1:>9} "
              f"{w1:>13} {e2:>10}")
        assert e0 == s_count * g_count and e1 == s_count and e2 == e0


def guards():
    print("\n== Guard cases (Design A must preserve #10106 rebind debt) ==")
    cases = [
        # name, scenario, want executions under prune, note
        ("quiet-delta sole stale row",
         [("s", 0, "pending")], 1,
         "no sibling: row survives and rebinds (#10106 core case)"),
        ("newest dead_letter, older pending",
         [("s", 0, "pending"), ("s", 1, "dead_letter")], 1,
         "dead_letter is not a carrier: older row still runs"),
        ("newest failed, older pending",
         [("s", 0, "pending"), ("s", 1, "failed")], 1,
         "failed is not claimable/carrier: older row still runs"),
        ("stale failed + newer pending",
         [("s", 0, "failed"), ("s", 1, "pending")], 1,
         "stale failed row pruned (prior_failure fold), survivor runs"),
        ("in-flight overlap bound",
         [("s", 0, "running"), ("s", 1, "pending"), ("s", 2, "pending")], 2,
         "running row finishes + newest survivor: <=2, independent of G"),
        ("retrying carrier",
         [("s", 0, "pending"), ("s", 1, "retrying")], 1,
         "retrying will execute: older pruned"),
        ("cross-scope isolation",
         [("a", 0, "pending"), ("b", 5, "pending")], 2,
         "newer generation in another scope is not a carrier"),
    ]
    for name, scen, want, note in cases:
        rows = fresh(scen)
        # A running row completes its execution first (already leased).
        pre = sum(1 for r in rows if r.status == "running")
        for r in rows:
            if r.status == "running":
                r.status = "succeeded"
        got, _ = drain(rows, "prune")
        total = pre + got
        print(f"{name:<38} executions={total} want={want}  {note}")
        assert total == want, (name, total, want)


if __name__ == "__main__":
    table()
    guards()
    print("\nAll shim assertions passed. Units are handler executions")
    print("(each = 1 active-snapshot load + extract + retract/write);")
    print("no wall-time claim is made or implied.")
