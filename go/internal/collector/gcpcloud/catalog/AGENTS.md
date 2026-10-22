# AGENTS.md — internal/collector/gcpcloud/catalog guidance

## Read First

1. `README.md` — what this leaf owns and why it exists.
2. `doc.go` — the godoc contract: leaf invariant, export rule, value
   contract.
3. `gcp_compute_net.go` — the largest family-consolidation shape to copy;
   `gcp_shared.go` — the shared/root/runtime shape (slice L, includes the
   type-alias pattern for typed constants).
4. `../AGENTS.md` — the gcpcloud source-fact contract and invariants this
   leaf serves.
5. `docs/public/reference/gcp-cloud-collector-contract.md` — the provider
   contract these values feed.

## Invariants

- MUST keep this package a leaf. Never import `gcpcloud`,
  `gcpcloud/extractor`, `gcpcloud/gcpruntime`, `gcpcloud/freshness`, any
  extractor group package, a cloud SDK, or any collector runtime package
  here. Either direction reintroduces the import cycle this package exists
  to break; the build, not a review, is the backstop, but do not rely on
  that — keep the import list at standard-library-only by construction
  (`time` for the L-slice live-request and poll bounds is the only
  exception; every other value is a string, int, or duration literal).
- MUST NOT change a constant value in a consolidation move. New code reads
  `catalog.X`; old code read the package-private `x` (or `extractor.X`,
  `gcpruntime.X`, `freshness.X` for slice L); the value must be identical.
  The value-pinning tests (`gcp_hk_test.go` for the HK families, a
  per-category pin test such as `gcp_l_test.go` for later slices) pin every
  value.
- MUST export by the mechanical rule: uppercase the first letter, preserve
  everything else. Do not "fix" initialisms (`ApiKeyAssetType` stays) — the
  rule is what maps each catalog identifier back to exactly one
  pre-consolidation gcpcloud identifier.
- MUST land each follow-up family as `gcp_<area>.go` grouped by the service
  areas in #6696, consolidated verbatim with doc comments adapted only for
  declaration-location claims (slice L capitalizes unexported names at the
  new home), plus value-pinning rows in `gcp_hk_test.go` (HK families) or a
  per-category pin test (later slices). Repoint every code and test
  reference to `catalog.X` and remove the emptied `const` declarations from
  the root extractor files in the same commit (most GCP origins keep types
  and functions; remove just the moved const blocks). Retire emptied compat
  aliases only at zero remaining references (compiler-verified).
- MUST extend the shared-helpers row in
  `docs/public/observability/telemetry-coverage.md` in place when a new leaf
  file lands (that file is grandfather-capped: the gate fails a new stage
  file with no row, and the cap fails a new line — follow the in-place row
  edit, never add a row), adding `No-Regression Evidence:` /
  `No-Observability-Change:` markers to this package's README (the perf
  gate reads added lines).

- Do not redeclare the cross-group constants already owned by
  `gcpcloud/extractor` (KMS, Compute, Storage, Pub/Sub topic, GKE cluster
  asset types and their prefixes).
- MUST NOT rename an exported identifier once another slice or lane
  references it; the pin test names every export.

## Common Changes

- Add a family: copy the `gcp_compute_net.go` shape (one `// <Family>
  constants consolidated from <file>.go (#6696).` section per source file,
  one `const` block per source block with the block doc carried, dedicated
  per-const docs preserved, shared docs kept on the block), write the
  value-pinning rows from the pre-move file contents, repoint every
  lower-case code reference to `catalog.X`, remove the emptied root `const`
  declarations, extend the telemetry shared-helpers row in place (no new
  rows), and run the affected package tests plus
  `scripts/verify-package-docs.sh`.
- Keep every file under 500 lines; split an oversized family before the cap.
  Keep the SPDX header on every new file.

## What Not To Change Without An ADR

- Do not add helpers, parsers, or builders here — this leaf owns constants
  (plus the named types and their predicates that typed constants require:
  `ParentScopeKind`/`EventKind`/`TriggerStatus` and `Valid` live here
  because a type cannot be declared beside its use without a cycle) only.
  Shared code lives in `gcpcloud/extractor`.
- Do not reintroduce a folded duplicate: `versionsMarker` folds into
  `SecretVersionVersionsMarker` by identical value, documented in the pin
  test.
