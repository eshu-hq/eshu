# AGENTS.md — internal/reducer/cloudinventory

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`,
`reducer/admissiondecision`, `reducer/factwrite`, `internal/correlation/cloudinventory`,
`internal/facts`, `internal/telemetry`, and `internal/truth`. It must **never**
import the parent `internal/reducer` package or any sibling family package,
directly or transitively.

The admission handler gates on the generation-freshness check before any load
or write, but none of that is a Go dependency on the root: the check resolves
through the `reducercontract.GenerationFreshnessCheck` func type, never through
an import. If a new symbol here gains a second consumer outside this package,
hoist it to the owning leaf the way `cloudjoin`, `factdecode`, and `gpphase`
were hoisted — do not export it for lateral import, and do not duplicate
production logic across families. (Test-only loader/writer fakes are the
exception: Go test files cannot share unexported symbols across a package
boundary, so those are duplicated verbatim with a comment, never exported.)
`sortedKeys` is the deliberate single-family helper that moved here with the
admission slice: its only consumer in this package is `admitCloudInventoryRecords`,
while `candidate_loader.go`, `code_import_repo_edge.go`, and
`package_consumption_repo_edge.go` keep calling the root original, so hoisting
it would drag staying callers along for no multi-family reason.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper (a stringer, a payload accessor, a source-uid extractor)
  goes to `reducer/payloadcore` or `reducer/factwrite`, with a one-line forwarder left in root;
- vocabulary (a domain name, an intent, an outcome value, a ledger) goes to
  `reducer/contract`, with a root alias;
- the `reducer_cloud_resource_identity` batch insert goes to
  `reducer/factwrite`;
- readiness phases and keyspaces go to `reducer/gpphase`;
- a symbol the root genuinely owns as logic stays in root, and this package
  does not use it.

Read the declaration before deciding. A body of
`return factwrite.BatchInsertFacts(...)` is a forwarder and costs nothing to
bypass; a real implementation with consumers on both sides needs a deliberate
hoist to a shared leaf, which is how `cloudjoin`, `factdecode`, and `gpphase`
came to exist.

## What must stay conservative

- The handler MUST reject a stale generation before any load or write. A
  superseded scan publishing canonical rows is fabricated truth, not a retry.
- Evidence layers MUST stay distinct: declared outranks applied, which outranks
  observed. A later observed record MUST NEVER demote a declared management
  origin, and `foldAdmittedRecord` raising the origin only toward stronger
  evidence is what enforces it.
- Tag, identity-policy, and resource-change evidence MUST NEVER admit a
  resource. A record whose uid was not admitted from resource evidence is
  dropped; evidence decorates, never fabricates.
- Tag value text MUST NEVER be persisted — only keyed fingerprint markers.
  Identity-policy evidence MUST NEVER carry raw GUIDs or raw assignment
  scopes — only keyed fingerprints. Resource-change evidence MUST NEVER carry
  provider locators, raw actor ids, or before/after values.
- The canonical payload MUST keep `account_id` present unconditionally,
  including when blank: its presence-vs-absence is the pre-#5238 rollout-gap
  signal `cloudInventoryRolloutGapWarningFlags` probes for. Dropping the key
  on blank reintroduces the gap.
- The writer MUST stay idempotent by canonical uid within the scope
  generation: the fact id hashes the stable fact key so retries and concurrent
  workers upsert one row through ON CONFLICT instead of duplicating truth.
- Blank, malformed, ambiguous, and unsupported identities MUST be counted
  (`ambiguous` / `unsupported` / `unresolved`) and surfaced, never fabricated
  into a uid.

## Telemetry honesty

- The `eshu_dp_cloud_inventory_admissions_total` counter is the only instrument
  this package emits, dimensioned by `provider` (admitted) and `outcome`
  (non-admitted, no provider label since the token may be unsupported). Never
  record a zero count to "prove" a quiet generation — `addOutcomeCount`
  returns early on zero, and the completion-adjacent `EvidenceSummary` is the
  always-present record.
- The admission summary (`admitted=%d ambiguous=%d unsupported=%d
  unresolved=%d canonical_writes=%d`) keeps the pre-move key set. Renaming a
  key orphans every dashboard and alert that reads it.
- Every metric name cited here and in `telemetry-coverage.md` must exist in
  `internal/telemetry/instruments.go`. A name that does not resolve is a
  fabrication, not a gap.

## House rules for this directory

- Keep this package at the admission slice (handler + decisions + writer) plus
  the three evidence-attach slices (tag, identity-policy, resource-change),
  the `doc.go` / `README.md` / `AGENTS.md` triad, and the family test files.
  A fourth evidence source is a new family proposal, not an addition.
- Name a compatibility shim for its subject (the family's snake_case name
  plus `_compat.go`), never for the package directory. A file whose stem
  equals a sibling package name or starts with `cloudinventory_` is a shim
  candidate on sight.
- Do not suppress `dirgate` with `//nolint`.
- Do not export a root test helper to use here. Go test files cannot share
  unexported symbols across a package boundary; this package keeps its own
  local stub loader/writer copies rather than reaching into the reducer
  root's test files.
