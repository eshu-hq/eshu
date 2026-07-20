# Evidence: #5376 code-root verdicts (prove-theory-first + runner cost)

Repo-wide Rails-controller dead-code-root verdicts. This note records the
prove-theory-first proof for the new per-partition SQL loads and the query-side
downgrade lookup, plus the measured downgrade set, before the runner wiring
landed.

## Theory under test

The reducer's CodeReachability projection runner gains, per partition, one extra
repo-scoped load (Ruby class registry: name + qualified_bases) and one extra
projected column on the existing roots load (class_context). The dead-code query
gains one per-investigation batch lookup of `downgraded` verdict rows. The
theory: all three are index-backed and cheap, and the downgrade set is small and
never flips a real controller.

## Setup (representative-volume synthetic corpus)

No local 20-repo corpus is available, so a representative Rails-shaped dataset
was seeded into a throwaway Postgres 16: `content_entities` = 50,929 rows across
5 repos; the `ruby-big` repo carries 505 Ruby classes (300 correctly-based
`App{i}Controller < ApplicationController`, 3 mis-based
`Legacy{i}Controller < ApplicationRecord < ActiveRecord::Base`, 200 POROs, 2
base classes) and 2,424 `ruby.rails_controller_action` root methods, plus 8,000
ruby filler functions; four `other-*` repos add 40,000 rows so the `repo_id`
filter is exercised against a large table. `code_root_verdicts` seeded with the
2,424 rows the runner would write (24 downgraded).

## EXPLAIN (ANALYZE, BUFFERS) — query shapes

Q1 — repo-wide class registry load
(`repo_id + entity_type='Class' + language='ruby'`):
- Index Scan on `content_entities_type_idx`, 505 rows, **Execution Time 0.459 ms**.

Q2 — roots + class_context load (the existing roots query with one added
projected column `metadata->>'class_context'`):
- Bitmap Index Scan on `content_entities_repo_idx` → filter, 2,424 rows,
  **Execution Time 8.98 ms**. This is the pre-existing `listCodeReachabilityRootsSQL`
  plan; the class_context projection does not change it (the column is read from
  the already-fetched JSONB heap tuple). No new cost attributable to #5376.

Q3 — dead-code query downgraded-verdict batch lookup
(`repository_id + entity_id IN (...) + verdict='downgraded'`, joined to active
generation):
- Index Scan using `code_root_verdicts_repo_entity_verdict_idx`
  (Index Cond on repository_id + entity_id ANY + verdict), 3 rows,
  **Execution Time 0.094 ms**. The new composite index is used exactly as
  intended; the active-generation join is a nested loop over the two tiny
  scope/generation rows.

## Measured downgrade set (real decision function)

A throwaway shim loaded the seeded classes + roots via Q1/Q2 and ran the real
`reducer.BuildCodeRootVerdicts` (which calls the shared `rubycontroller.Decide`):

```
classes=505 roots=2424 verdictRows=2424 confirmed=2400 downgraded=24 inconclusive_missing_ctx=0
downgraded-set size=24, real-controller-downgrades=0
sample downgraded: Legacy1Controller:action1 chain=[Legacy1Controller ApplicationRecord ActiveRecord::Base] terminal=unresolved_base:ActiveRecord::Base reason=unresolved_non_controller
sample confirmed: App100Controller:action1 chain=[App100Controller ApplicationController] terminal=accepted:ApplicationController reason=accepted
```

Finding: the downgrade set is ~1% of controller-action roots (24/2424), and
**zero correctly-based controllers were downgraded** — only the mis-based
`Legacy*Controller` actions the parser over-kept flipped. This is the
false-positive guard the design demands, proven on representative data. On a
real Rails corpus where nearly every controller is correctly based, the
downgrade count is expected to be near zero; a near-zero count is a recorded
finding, not a defect (the behavior proof is the failing-then-green fixtures and
the reducer verdict tests, not a large downgrade volume).

## P1 fix (F1/F2/F3) live re-proof

The initial implementation keyed the repo-wide registry by simple entity_name,
which downgraded a genuine `OrdersController < Admin::Base` (Admin::Base <
ActionController::Base) as dead — a proven P1 false positive. The Fable-locked
fix adds an F1 keep-biased floor for unresolvable qualified bases, an F2 curated
reject-set, and an F3 qualified_name registry with segment-aligned suffix
resolution. Re-seeded representative corpus (508 Ruby classes, 2,432 controller
roots) including the namespaced regression (`NamespacedController < Admin::Base`,
`module Admin; class Base < ActionController::Base`) and a same-last-segment
impostor (`Reporting::Base < ActiveRecord::Base`):

- Q1 class-registry load with the added `metadata->>'qualified_name'` column:
  same Index Scan plan, 508 rows, **0.368 ms** — the column is free.
- Real `BuildCodeRootVerdicts` over the loaded emissions: **2,408 confirmed /
  24 downgraded**; the 8 `NamespacedController` actions are **CONFIRMED**
  (chain `NamespacedController -> Admin::Base -> ActionController::Base`,
  terminal `accepted:ActionController::Base`) — the P1 regression is fixed live;
  the impostor `Reporting::Base` did not mis-resolve it. The 24 `Legacy*`
  actions **DOWNGRADE** (chain `-> ApplicationRecord -> ActiveRecord::Base`,
  terminal `rejected_base:ActiveRecord::Base`, reason `rejected_framework_base`).
  **Zero correctly-based (`App*`) controllers downgraded.**
- Red-then-green: on the pre-fix commit the same namespaced controller is
  DOWNGRADED (flagged dead); post-fix it is CONFIRMED.

## Runner-wiring performance / observability

Performance Evidence: the runner adds one index-backed repo-scoped SQL load per
partition (Q1, 0.46 ms at 505 classes / 50k-row table) and an in-memory memoized
walk (O(#classes + #roots), MaxWalkDepth 10, cycle-safe) — no recursive CTE. The
verdict rows are written inside the existing partition transaction alongside the
reachability rows (delete verdicts → insert verdicts, same conflict key), so the
concurrency conflict domain is unchanged. No new lock, lease, or queue path.

No-Regression Evidence: Q2's plan is byte-identical to the pre-#5376 roots load
(only an added projected column); Q1 and Q3 are new but sub-10 ms and
index-backed on representative volume.

Observability Evidence: the runner logs verdict counts
(`verdicts_confirmed`, `verdicts_downgraded`, `verdicts_inconclusive_missing_context`)
and reuses the existing `eshu_dp_reducer_*` projection-cycle metrics; the
CodeRootVerdictStats surface lets an operator see how many roots were downgraded
per cycle and how many lacked a class_context bridge.
