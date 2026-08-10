# Remote-validation disposition log

Historical per-row disposition record for the frozen `remote_validation`
pointer baseline (#5407). A file closing that existence baseline is not, by
itself, proof that its production claim is substantiated. #5552 separately
requalifies each artifact for a matching deployed tier, dated run, exact
command, direct exit capture, and capability-specific observed result. Entries
here record only the tranches that already received an explicit validate-or-
downgrade disposition. See [README.md](README.md) for the baseline mechanics.

Each entry records the disposition considered and rejected, not only the one
taken, so a later reviewer can see the option space without re-deriving it.

## TRANCHE 1 — #5336 pair (closed by #5552)

**Capabilities:** `component_extensions.inventory`,
`component_extensions.diagnostics`
**Slugs:** `prod-component-extension-inventory`,
`prod-component-extension-diagnostics`
**Disposition:** DOWNGRADED, `production` profile `supported` -> `experimental`
**Superseded:** both rows are back at `production: supported`. Later deployed
API readback artifacts proved the inventory and diagnostics surfaces against a
claimed component. The text below remains the historical decision record, not
the current matrix state.
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

## TRANCHE 2 — #5681 Cluster B, validated on deployed services

**Capabilities:** `secrets_iam.identity_trust_chains.list`,
`secrets_iam.posture_gaps.list`, `secrets_iam.posture_summary.read`,
`secrets_iam.privilege_posture_observations.list`,
`secrets_iam.secret_access_paths.list`,
`code_to_cloud.trace_exposure_path`, and `code_search.variable_lookup`

**Disposition:** VALIDATED. All seven production rows retain or return to
`supported`; no downgrade was requested or approved. A second fresh run under
`ESHU_QUERY_PROFILE=local_authoritative` exercised variable lookup and trace
exposure against the real graph stack, so their local_authoritative rows also
retain `supported`. Their local_full_stack rows cite the same Compose driver.

These were the final seven slugs in the remote-validation baseline. The earlier
draft downgrade rested on an incomplete evidence search and a false assumption
that the optional secrets/IAM graph-projection flag controlled the read models.
It did not land. The read-model reducer is always registered; only graph
projection is optional.

The accepted proof uses public-safe synthetic Kubernetes, AWS, and Vault
cassettes on a fresh, uniquely named Compose project. The golden-corpus driver
rebuilt every host binary, replayed all credential-free collectors, drained the
reducer and projector queues to terminal, and exercised the HTTP and MCP
surfaces. The final run completed with 532 passes, zero required failures, zero
advisory warnings, and exit code 0.

### Per-row deployed result

| Slug | Capability | Observed deployed result | Artifact |
| --- | --- | --- | --- |
| `prod-secrets-iam-identity-trust-chains` | `secrets_iam.identity_trust_chains.list` | MCP returned one exact chain with the expected workload identity and state. | `docs/internal/remote-validation/prod-secrets-iam-identity-trust-chains.md` |
| `prod-secrets-iam-privilege-posture-observations` | `secrets_iam.privilege_posture_observations.list` | MCP returned one wildcard-trust observation with high severity and partial state. | `docs/internal/remote-validation/prod-secrets-iam-privilege-posture-observations.md` |
| `prod-secrets-iam-secret-access-paths` | `secrets_iam.secret_access_paths.list` | MCP returned one exact, fingerprinted access path with the read capability and no secret value. | `docs/internal/remote-validation/prod-secrets-iam-secret-access-paths.md` |
| `prod-secrets-iam-posture-gaps` | `secrets_iam.posture_gaps.list` | MCP preserved one unsupported-policy-layer gap and its unsupported state. | `docs/internal/remote-validation/prod-secrets-iam-posture-gaps.md` |
| `prod-secrets-iam-posture-summary` | `secrets_iam.posture_summary.read` | MCP returned four non-zero buckets covering the chain, observation, access path, and gap. | `docs/internal/remote-validation/prod-secrets-iam-posture-summary.md` |
| `prod-trace-exposure-path` | `code_to_cloud.trace_exposure_path` | MCP resolved the source as an HTTP handler and returned the documented bounded unresolved state without inventing a path. | `docs/internal/remote-validation/prod-trace-exposure-path.md` |
| `prod-variable-lookup` | `code_search.variable_lookup` | MCP `find_code` and HTTP code search each returned four Variable-labeled matches. | `docs/internal/remote-validation/prod-variable-lookup.md` |

### Exact validation command

```bash
GATE_COMPOSE_PROJECT=eshu-5681-claim-honesty-20260808-7 ESHU_POSTGRES_PORT=36542 NEO4J_BOLT_PORT=36687 NEO4J_HTTP_PORT=36474 GATE_API_PORT=36080 GATE_MCP_PORT=36091 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh >/tmp/eshu-5681-b7-postrebase2.log 2>&1; echo $?
```

Captured output: `0`. Validation date: 2026-08-08.

The local-authoritative profile cross-check used a second fresh project:

```bash
ESHU_QUERY_PROFILE=local_authoritative GATE_COMPOSE_PROJECT=eshu-5681-local-authoritative-20260808-4 ESHU_POSTGRES_PORT=37542 NEO4J_BOLT_PORT=37687 NEO4J_HTTP_PORT=37474 GATE_API_PORT=37080 GATE_MCP_PORT=37091 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh >/tmp/eshu-5681-local-authoritative-postrebase2.log 2>&1; echo $?
```

Captured output: `0`. The log records `query profile: local_authoritative`
before staging or build work. The run completed in 118 seconds with 532 passes,
zero required failures, and zero advisory warnings.

## TRANCHE 3 — #5552 legacy production requalification

**Disposition:** VALIDATED. All 103 legacy slugs retain `production:
supported`; no downgrade was proposed or approved. Together with the seven
#5681 artifacts, the generated inventory now maps 110 supported slugs to 115
production row-occurrences, each backed by a machine-valid deployed artifact.

The canonical per-row table is
[`inventory.generated.json`](inventory.generated.json). Every entry gives the
slug, artifact path, and all `capability/production` subjects sharing that
artifact. The artifact itself records the exact command, captured exit code,
run date, matching tier, committed driver, and capability-specific assertion.
This keeps the full 115-row disposition machine-checkable without duplicating
the generated mapping in prose.

### Validation families

| Slugs | Action | Fresh deployed proof | Result |
| --- | --- | --- | --- |
| 100 legacy slugs | Validate and retain `supported` | `scripts/verify-golden-corpus-gate.sh` on project `eshu5552-ask-aggregate-20260809c` | 550 passed, 0 required failures, 0 advisory warnings; independent persisted-graph aggregate oracle; direct exit 0 |
| `prod-component-extension-inventory`, `prod-component-extension-diagnostics` | Validate and retain `supported` | `scripts/run-remote-e2e-component-extension.sh` on project `eshu-5552-component-20260809-3` from `bab7e38e6f` | Installed, enabled, trusted registry readback; completed workflow; non-empty fact families; positive authenticated HTTP and MCP inventory and diagnostics assertions; direct exit 0 |
| `prod-dead-iac` | Validate and retain `supported` | `scripts/verify_dead_iac_compose.sh` on project `eshu-5552-dead-iac-20260809-5` | Ten materialized findings across five IaC families through API and MCP, with Postgres and NornicDB truth checks; direct exit 0 |

### Recurrence proof

`scripts/verify-remote-validation-artifacts.sh` accepts the real tree with zero
baseline entries and zero findings. Its hermetic mirror rejects placeholder,
lower-tier, malformed, source-missing, and command-masked evidence. The BITES
case adds a supported production row with no matching artifact and observes
RED, then reverts the matrix and observes GREEN. CI path selection covers the
matrix, artifact directory, generated inventory, allowed evidence sources, and
the verifier implementation, so a new unbacked claim or a broken source cannot
bypass the PR gate.

### Runtime and operator evidence

The tracked [#5552 runtime record](../evidence/5552-capability-matrix-claim-honesty.md)
binds the performance baseline, deployed result, terminal queue counts, and
existing operator signals to the hot paths changed during requalification. The
per-slug artifacts remain the authority for capability-specific assertions.
