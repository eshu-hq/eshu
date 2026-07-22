# Remote-validation disposition log

Per-row disposition record for the #5552 burn-down of the frozen
`remote_validation` baseline (#5407). Every row here documents how one
baselined slug was closed: by committing a real evidence artifact
(**validated**) or by an explicit, reviewed decision to lower the capability's
claimed status (**downgraded**). See [README.md](README.md) for the mechanics
of the baseline/frozen file pair and the `FROZEN_MAX` ratchet.

Each entry records the disposition considered and rejected, not only the one
taken, so a later reviewer can see the option space without re-deriving it.

## TRANCHE 1 — #5336 pair (closed by #5552)

**Capabilities:** `component_extensions.inventory`,
`component_extensions.diagnostics`
**Slugs:** `prod-component-extension-inventory`,
`prod-component-extension-diagnostics`
**Disposition:** DOWNGRADED, `production` profile `supported` -> `experimental`
**Tracking:** #5336 (original finding), #5552 (systemic burn-down), #5407
(freeze that bounded the debt)

### prod-component-extension-inventory

- Before: `production: {status: supported, ..., verification:
  [{remote_validation: prod-component-extension-inventory}]}` — sole evidence
  a `remote_validation` ref resolving to no committed artifact.
- After: `production: {status: experimental, ..., verification: [{go_test:
  ./internal/query}]}` — the same committed evidence that backs all three
  local profiles.
- Remaining committed evidence: `go_test ./internal/query` proves functional
  readback (sanitized component registry list with count, total_count,
  limit, and truncated) against the query handler. It does not prove the
  production profile's deployed-scale claim: `required_runtime:
  deployed_services_component_registry`, `p95_latency_ms: 500`, and
  `max_scope_size: bounded_deployed_component_registry_page_1_500` remain
  declared-but-unproven targets on the row, now correctly scoped by
  `experimental` rather than asserted as `supported`.
- Production-specific fields (`p95_latency_ms`, `max_scope_size`,
  `required_runtime`) were kept, not stripped — they are the target contract
  for a future validation pass, not evidence of one already done.

### prod-component-extension-diagnostics

- Before: `production: {status: supported, ..., verification:
  [{remote_validation: prod-component-extension-diagnostics}]}` — sole
  evidence a `remote_validation` ref resolving to no committed artifact.
- After: `production: {status: experimental, ..., verification: [{go_test:
  ./internal/query}]}` — the same committed evidence that backs all three
  local profiles.
- Remaining committed evidence: `go_test ./internal/query` proves functional
  readback (singleton component diagnostics) against the query handler. The
  production profile's deployed-scale claim (`required_runtime:
  deployed_services_component_registry`, `p95_latency_ms: 500`,
  `max_scope_size: one_component`) is unproven and stays declared-but-unproven
  under `experimental`.

### Disposition options considered

Three dispositions were on the table for this pair (per #5552's design, ruled
by Fable as design arbiter plus the orchestrator):

- **(A) Validate** — commit a real deployed-services evidence artifact under
  `docs/internal/remote-validation/prod-component-extension-{inventory,
  diagnostics}.md` and keep `status: supported`. Not taken for TRANCHE 1: no
  deployed-registry validation run exists to record: the remaining committed
  evidence is `go_test ./internal/query`, which proves functional behavior
  against the query handler but does not prove the production profile's
  deployed-scale claim (`deployed_services_component_registry` runtime,
  multi-repo scope, prod p95).
- **(B) Downgrade** — lower `status` to `experimental`, replace the
  `remote_validation` ref with the real committed evidence, keep the
  production-specific budget/runtime fields as unproven targets. **Taken.**
  The remaining evidence proves functional correctness, which the closed
  status vocabulary (`supported`, `experimental`, `unsupported`) maps to
  `experimental`, not `unsupported` — `unsupported` derives `preview`
  ("absent in production"), which would misstate that the capability *is*
  exposed and reachable in production, just without a committed
  deployed-scale proof.
- **(C) Evidence-pending marker** — add a third verification kind (e.g. a
  `pending` marker) alongside `go_test`, keeping `status: supported`.
  **REJECTED.** This keeps the load-bearing `supported` token attached to a
  claim with no committed production evidence — exactly the gap #5407 froze
  and #5552 exists to close — and it carves a third gate exit (validated /
  downgraded / pending) that #5407's baseline-vs-frozen design forbids: the
  gate recognizes only "artifact exists" or "baselined debt," and a
  `pending` marker would let a row escape both without being honestly
  downgraded.

### Regeneration recipe used (TRANCHE 1 — reusable for remaining rows)

Run from the worktree root unless noted:

1. Edit `specs/capability-matrix/component-extensions.v1.yaml`: set the
   affected `production:` profile's `status` to `experimental`, replace its
   `verification:` list's `{remote_validation: <slug>}` entry with the
   capability's real local-profile evidence (here `{go_test:
   ./internal/query}`), update `notes:` to state the functional-vs-production
   evidence gap honestly. Keep `p95_latency_ms`, `max_scope_size`, and
   `required_runtime` — do not strip them.
   - **Gotcha:** avoid a space immediately before a `#` inside an unquoted
     flow-style YAML scalar (e.g. `(#5336, #5552)`); YAML treats
     whitespace-then-`#` as a comment start even mid-value, which truncates
     the scalar and the enclosing `{...}` and fails the parse with a
     misleading "did not find expected node content" pointing at an
     unrelated line. Reference issues as plain digits or a quoted scalar
     instead (`issue 5336`, or quote the whole `notes:` value).
2. `cd go && go run ./cmd/capability-inventory -mode generate` — regenerates
   `go/internal/capabilitycatalog/data/catalog.generated.json` (and
   `surface-inventory.generated.json`, unchanged when no surface changed).
   Confirm the diff shows exactly: `maturity`/`derived_maturity`
   `general_availability` -> `experimental`, the affected profile's `status`
   `supported` -> `experimental`, and the `remote_validation` proof-signal
   entry dropped in favor of the deduplicated `go_test` entry already present
   from the local profiles.
3. `cd go && go run ./cmd/capability-inventory -mode docs` — run this BEFORE
   assuming which doc file holds the marker. For this pair the real
   `<!-- capability-state: ... -->` markers live in
   `docs/public/reference/collector-extraction-policy.md` (both capabilities
   already had markers there), not in `capability-catalog.md` — a
   `capability-state` marker inside a fenced code example (like the one at
   `capability-catalog.md:134`, which is documentation syntax, not a live
   claim) is correctly NOT flagged by the gate. Trust the gate's file:line
   output over a guessed location. Update each flagged marker's `state=`
   from `ga` to `experimental`, and update any adjoining "generally
   available"/"GA" prose in the same doc.
4. `cd go && go run ./cmd/capability-inventory -mode remote-validation -update`
   — run this AFTER steps 1-3 land (the matrix must no longer cite the
   `remote_validation` ref). Regenerates
   `specs/remote-validation-baseline.txt`: drops the now-unbaselined slug(s)
   and ratchets `FROZEN_MAX` down to the new count. Does NOT touch
   `specs/remote-validation-frozen.txt` by design.
5. Hand-edit `specs/remote-validation-frozen.txt` to remove the same slug
   line(s) removed from the baseline in step 4, keeping `baseline ⊆ frozen`
   intact. Verify with
   `rg -n '<slug>' specs/remote-validation-baseline.txt specs/remote-validation-frozen.txt`
   (expect no matches).
6. Add or update this file's per-row disposition entry, and update
   `README.md`'s "Current state" section counts/prose if it names the closed
   slug(s) directly.
7. Run the full proof list (verify, docs, remote-validation-artifacts,
   maturity-drift-guard, focused Go tests, mkdocs strict build, `git diff
   --check`) before committing.

## TRANCHE 2 — code-intelligence cluster (closed by #5552)

**Capabilities:** the 28 code-intelligence slugs listed below (code search,
symbol graph, code flow, call graph, code quality, and the two
platform-impact code-to-infrastructure reads that share this cluster's
evidence shape).
**Disposition:** DOWNGRADED, `production` profile `supported` ->
`experimental`, for every row.
**Tracking:** #5336 (original finding), #5552 (systemic burn-down), #5407
(freeze that bounded the debt)

Each row below replaces the `production` profile's sole `remote_validation`
evidence with the real committed evidence that backs that capability's
`local_full_stack` profile — the runtime tier immediately below `production`
on the local_host -> local_host_plus_graph -> full_stack -> deployed_services
ladder. `p95_latency_ms`, `max_scope_size`, and `required_runtime` were kept
as declared-but-unproven production targets, not stripped.

For 8 of the 28 rows (marked below) the `local_full_stack` profile's
committed evidence is a `compose_e2e` scenario rather than `go_test`, and
that scenario name differs from what `local_lightweight` and
`local_authoritative` cite for the same capability (each of those three
tiers uses a different verification kind: `integration_test` for
lightweight/authoritative-only rows, `go_test` for authoritative, and
`compose_e2e` for full_stack). This is the "unexpected local evidence" case
flagged per the tranche-2 instructions: the row's three local profiles do
not share one identical proof signal the way TRANCHE 1's pair did, so the
mirrored production evidence is the nearest-tier (`local_full_stack`)
signal rather than a signal common to all three local profiles.

### Per-row disposition

- `prod-code-search-exact` (`code_search.exact_symbol`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query`
- `prod-code-search-fuzzy` (`code_search.fuzzy_symbol`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query`
- `prod-symbol-lookup` (`code_search.symbol_lookup`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query`
- `prod-variable-lookup` (`code_search.variable_lookup`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query`
- `prod-argument-names` (`symbol_graph.argument_names`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query`
- `prod-class-methods` (`symbol_graph.class_methods`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query`
- `prod-decorators` (`symbol_graph.decorators`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query`
- `prod-structural-inventory` (`code_inventory.structural`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query`
- `prod-complexity` (`code_quality.complexity`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query`
- `prod-imports` (`symbol_graph.imports`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query`
- `prod-import-dependencies` (`symbol_graph.import_dependencies`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query`
- `prod-inheritance` (`symbol_graph.inheritance`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query`
- `prod-dead-code` (`code_quality.dead_code`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `compose_e2e dead-code-reachability` (unexpected: local profiles use three different verification kinds; mirrored the local_full_stack tier)
- `prod-code-quality-refactoring` (`code_quality.refactoring`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query`
- `prod-code-flow-taint-path` (`code_flow.taint_path`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query, go_test ./internal/mcp`
- `prod-code-flow-reaching-def` (`code_flow.reaching_def`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query, go_test ./internal/mcp`
- `prod-code-flow-cfg-summary` (`code_flow.cfg_summary`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query, go_test ./internal/mcp`
- `prod-code-flow-pdg-summary` (`code_flow.pdg_summary`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query, go_test ./internal/mcp`
- `prod-direct-callees` (`call_graph.direct_callees`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `compose_e2e direct-callees` (unexpected: local profiles use three different verification kinds; mirrored the local_full_stack tier)
- `prod-direct-callers` (`call_graph.direct_callers`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `compose_e2e direct-callers` (unexpected: local profiles use three different verification kinds; mirrored the local_full_stack tier)
- `prod-transitive-callees` (`call_graph.transitive_callees`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `compose_e2e transitive-callees` (unexpected: local profiles use three different verification kinds; mirrored the local_full_stack tier)
- `prod-transitive-callers` (`call_graph.transitive_callers`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `compose_e2e transitive-callers` (unexpected: local profiles use three different verification kinds; mirrored the local_full_stack tier)
- `prod-call-chain-path` (`call_graph.call_chain_path`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `compose_e2e call-chain-path` (unexpected: local profiles use three different verification kinds; mirrored the local_full_stack tier)
- `prod-call-graph-metrics` (`call_graph.metrics`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query`
- `prod-content-search` (`code_search.content_search`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query`
- `prod-entity-map` (`platform_impact.entity_map`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `compose_e2e entity-map` (unexpected: local profiles use three different verification kinds; mirrored the local_full_stack tier)
- `prod-route-to-caller` (`call_graph.route_to_caller`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `go_test ./internal/query`
- `prod-resource-to-code` (`platform_impact.resource_to_code`) — DOWNGRADED `production` `supported` -> `experimental`; remote_validation ref dropped; remaining evidence: `compose_e2e resource-to-code` (unexpected: local profiles use three different verification kinds; mirrored the local_full_stack tier)

### Disposition options considered

Same three options as TRANCHE 1 (see above), decided the same way for all 28
rows: **(A) Validate** was not taken because no deployed-scale evidence
artifact exists for any of the 28 rows to commit. **(B) Downgrade** was
taken uniformly — the remaining local-profile evidence proves functional
correctness against the query/MCP/compose_e2e handler, which maps to
`experimental`, not `unsupported` (every one of these 28 rows has a
`supported` `local_authoritative` and `local_full_stack` profile, so there
is no case in this tranche where the edge-case `unsupported` disposition
applied). **(C) Evidence-pending marker** was rejected for the same reason
as TRANCHE 1: it would let a row escape the closed
validated/downgraded vocabulary without an honest status change.

### Regeneration recipe used (TRANCHE 2)

Same recipe as TRANCHE 1, run once across all 28 rows in
`specs/capability-matrix.v1.yaml` (not the `component-extensions.v1.yaml`
fragment): edit all 28 `production:` rows, `cd go && go run
./cmd/capability-inventory -mode generate`, `cd go && go run
./cmd/capability-inventory -mode docs` (this tranche's only marker was
`README.md:55`'s `code_search.symbol_lookup` `capability-state` comment plus
its `specs/product-claims.v1.yaml` ledger entry — `general_availability` ->
`experimental`, with a new `issues: [{number: 5552, state: open}]` block
added to satisfy the ledger's non-GA-claim-needs-a-tracking-issue check),
`cd go && go run ./cmd/capability-inventory -mode remote-validation
-update` (FROZEN_MAX 113 -> 85), then hand-edit
`specs/remote-validation-frozen.txt` to remove the same 28 slugs.
