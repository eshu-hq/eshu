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

## TRANCHE 2 — #5681 Cluster B, matrix-label mismatch (closes #5552 and #5681)

**Capabilities:** `secrets_iam.identity_trust_chains.list`,
`secrets_iam.posture_gaps.list`, `secrets_iam.posture_summary.read`,
`secrets_iam.privilege_posture_observations.list`,
`secrets_iam.secret_access_paths.list`, `code_to_cloud.trace_exposure_path`,
`code_search.variable_lookup`
**Slugs:** `prod-secrets-iam-identity-trust-chains`,
`prod-secrets-iam-posture-gaps`, `prod-secrets-iam-posture-summary`,
`prod-secrets-iam-privilege-posture-observations`,
`prod-secrets-iam-secret-access-paths`, `prod-trace-exposure-path`,
`prod-variable-lookup`
**Disposition:** DOWNGRADED, all seven, `production` profile `supported` ->
`experimental`
**Tracking:** #5552 (systemic burn-down), #5681 (this cluster), #5407 (freeze
that bounded the debt)

These seven were the entire remaining baseline (`FROZEN_MAX` was 7 going in).
Closing them empties the baseline, which closes #5407's burn-down tracking,
#5552, and #5681.

### Evidence found per row

For every row, the production profile's sole prior evidence was a
`remote_validation` ref resolving to no committed file. Real committed
evidence for each capability was searched for before deciding a disposition
(`rg` over `go/internal/query`, `go/internal/mcp`, `scripts/`,
`docs/internal/evidence/`, and `docs/internal/design/`):

- **`secrets_iam.*` (five capabilities):** `go_test ./internal/query` is real
  and substantial. `go/internal/query/secrets_iam.go`,
  `secrets_iam_posture_handlers.go`, `secrets_iam_summary.go`,
  `secrets_iam_trust_chain.go`, `secrets_iam_grant_posture.go`, and their
  matching `_test.go` files prove the bounded reducer read-model lookup
  functionally, against fixture data. That is not a deployed read. The
  reducer domain that populates these read models,
  `DomainSecretsIAMGraphProjection`, is gated behind
  `ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED` and stays off by default
  in every deployment (`go/cmd/reducer/secrets_iam_graph_wiring.go`). The only
  flag-on run on record is
  `docs/internal/design/1314-secrets-iam-graph-activation-record.md` (#2430,
  closed 2026-06-16): a transient proof-only enable on a single
  remote-validation target
  (`remote-amd64-validation/issue-2430-secrets-iam-proof`) that proved the
  reducer's graph *write* path (live NornicDB writer conformance,
  redaction-allowlist, scoped retract) and was torn down after capture. Its
  own status line says "repository, chart, and operator defaults remain OFF."
  No standing deployment serves these facts, and no run of any kind exercises
  the *query/list* capability (`list_secrets_iam_identity_trust_chains` and
  siblings) against a deployed API or MCP surface. The local-profile rows for
  all five already correctly cite `go_test`; there was no local-profile
  mislabel to fix here, only the production tier's dangling
  `remote_validation` ref.
- **`code_to_cloud.trace_exposure_path`:** `go_test ./internal/query` is real
  and covers the handler directly:
  `go/internal/query/exposure_path_handler_test.go`
  (`TestTraceExposurePathRendersReachableSink`,
  `TestTraceExposurePathRendersShellExecSink`,
  `TestTraceExposurePathUnresolvedWhenNoSink`, and more) plus
  `graph_read_error_impact_test.go`. The matrix's `local_authoritative` row
  cited `integration_test: local-authoritative-trace-exposure-path` and
  `local_full_stack` cited `compose_e2e: trace-exposure-path`. Neither ref
  resolves to a committed script anywhere in `scripts/` or elsewhere in the
  tree (`rg` for both ref strings outside the matrix file returns nothing).
  Corrected both to `go_test: ./internal/query`, the evidence that is
  actually committed. No deployed proof (a `scripts/run-remote-e2e-*` driver
  or a live-backend `docs/internal/evidence/*.md` record) exists for the
  production tier.
- **`code_search.variable_lookup`:** `go_test ./internal/query` and
  `./internal/mcp` are real. `go/internal/query/code_search_metadata_test.go`
  (graph-backed search results), `code_search_authz_test.go`, and
  `go/internal/mcp/dispatch_code_authz_test.go` /
  `run_readonly_test.go` cover the `find_code` dispatch path. The matrix's
  `local_authoritative` row cited
  `integration_test: local-authoritative-variable-lookup`, which does not
  resolve to any committed script. Corrected to `go_test: ./internal/query`.
  No deployed proof exists for the production tier.

### Why downgrade, not validate

Validating (option a: run the real deployed proof, keep `supported`) was the
first disposition considered for every row, matching how Cluster A closed two
of its seven (`prod-transitive-callers` / `prod-transitive-callees`) by
running the deployed-services stack and committing a readback artifact. That
path was attempted here. The remote validation host named in this
repository's `eshu-remote-validation` skill was unreachable this session:
`ssh` to the configured target timed out with `Operation timed out` on port
22. With no reachable deployed stack, and no existing committed deployed
evidence found anywhere in the tree for these seven capabilities' *read*
surfaces, option (a) was not available honestly. Writing a
`docs/internal/remote-validation/<slug>.md` artifact without having actually
run anything against a deployed stack is exactly the fabrication this epic
exists to prevent.

Option (b) as a pure relabel (keep `production: supported`, just cite
`go_test` instead of `remote_validation`) was rejected for the same reason
TRANCHE 1 rejected it. CLAUDE.md's "Claim Evidence Lives In Known Locations"
guardrail is explicit that a `production`/deployed-tier `supported` claim
needs deployed evidence, and that retaining a top-tier claim on a local-tier
test is not licensed. `go_test ./internal/query` proves the query handler
functionally; it does not prove the row's `required_runtime:
deployed_services`, its p95 budget, or its `multi_repo_platform` /
`deployed_services_component_registry`-class scope claim.

Downgrade (option c) is therefore the only disposition that matches what is
actually proven: `production: supported` -> `production: experimental`,
citing the real `go_test` evidence in place of the dangling
`remote_validation` ref. `p95_latency_ms`, `max_scope_size`, and
`required_runtime` were kept on every row, not stripped — per the TRANCHE 1
precedent, they are the target contract for a future deployed validation
pass, not evidence of one already done.

### Disposition options considered

- **(A) Validate** — run the real deployed proof and commit
  `docs/internal/remote-validation/<slug>.md` per row, keeping `supported`.
  **Not taken.** No reachable deployed stack this session (remote host
  connection timed out), and no existing committed deployed evidence for any
  of the seven read surfaces.
- **(B) Relabel only** — keep `status: supported`, replace the dangling
  `remote_validation` ref with `go_test`. **REJECTED** for the production
  tier per CLAUDE.md's evidence-tier guardrail: a local-tier test cannot
  license a deployed-tier claim. **Taken** only for the three local-profile
  rows (`variable_lookup` local_authoritative; `trace_exposure_path`
  local_authoritative and local_full_stack) whose cited `integration_test` /
  `compose_e2e` ref never resolved to a script — those rows' `status` was
  already `supported` on real `go_test` evidence, so relabeling them to the
  ref that actually backs them is an honest correction, not a claim change.
- **(C) Downgrade** — lower `production` `status` to `experimental`, replace
  the `remote_validation` ref with the real `go_test` evidence, keep the
  declared production budget/runtime fields as unproven targets. **Taken**
  for all seven production rows.

### Follow-up

Restoring `production: supported` for any of these seven requires a real
deployed-services run: for the five `secrets_iam.*` rows, a target deployment
with the graph-projection flag enabled plus seeded IAM/secrets fixtures,
queried through the API/MCP list tools (not only the reducer write path
`docs/internal/design/1314-secrets-iam-graph-activation-record.md` already
proved); for `trace_exposure_path` and `variable_lookup`, a deployed
`compose_e2e` or `run-remote-e2e-*` run against a corpus with a reachable
cloud-sink chain and durable semantic facts, respectively. That work is tracked
in #5958. It needs its own issue because #5552 and #5681 both close when this
lands, so neither can carry a pointer anyone will find afterward.

### Gating overlay for the five secrets/IAM rows

Downgrading the tier records that no deployed evidence exists. It does not record
the separate, also-true fact that these reads are off by default: the reducer only
registers the secrets/IAM graph projection domain when an operator sets
`ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED`, so a stock deploy serves an
empty page regardless of how much proof we later attach.

Those are two different axes, and the matrix tier only carries the first. The
repository already has a place for the second — the `maturity: gated` overlay in
`specs/capability-catalog.v1.yaml`, used for the `ESHU_EMIT_DATAFLOW` reachability
rows and the collector-gated supply-chain rows. All five secrets/IAM capabilities
now carry that overlay, so the generated catalog reports `maturity: gated` with
`derived_maturity: experimental` instead of leaving a consumer to infer
reachability from prose alone.

`supply_chain.impact_findings.list` shows the two axes are independent: it holds
`status: supported` with a committed production artifact and `maturity: gated` at
the same time. If deployed evidence for the secrets/IAM reads lands later, the
tier can rise on its own merit while the gating overlay stays.
