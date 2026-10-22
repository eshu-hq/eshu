# AGENTS.md — internal/collector/gcpcloud/catalog guidance

## Read First

1. `README.md` — what this leaf owns and why it exists.
2. `doc.go` — the godoc contract: leaf invariant, export rule, value
   contract.
3. `gcp_compute_net.go` — the largest consolidated family (shape to copy).
4. `../AGENTS.md` — the gcpcloud source-fact contract and invariants this
   leaf serves.
5. `docs/public/reference/gcp-cloud-collector-contract.md` — the provider
   contract these values feed.

## Invariants

- MUST keep this package a leaf. Never import `gcpcloud`,
  `gcpcloud/extractor`, any extractor group package, a cloud SDK, or any
  collector runtime package here. Either direction reintroduces the import
  cycle this package exists to break; the build, not a review, is the
  backstop, but do not rely on that — keep the import list at stdlib-only by
  construction. Every value here is a string or int literal, so no import is
  ever needed.
- MUST NOT change a constant value in a consolidation move. New code reads
  `catalog.X`; old code read the package-private `x`; the string must be
  identical. The value-pinning test (`gcp_hk_test.go`) pins every value.
- MUST export by the mechanical rule: uppercase the first letter, preserve
  everything else. Do not "fix" initialisms (`ApiKeyAssetType` stays) — the
  rule is what maps each catalog identifier back to exactly one
  pre-consolidation gcpcloud identifier.
- MUST land each follow-up family as `gcp_<area>.go` grouped by the service
  areas in #6696, consolidated verbatim with doc comments adapted only for
  declaration-location claims, plus value-pinning rows in `gcp_hk_test.go`.
  Repoint every code and test reference to `catalog.X` and remove the emptied
  `const` declarations from the root extractor files in the same commit.
- MUST extend the shared-helpers row in
  `docs/public/observability/telemetry-coverage.md` in place when a new leaf
  file lands (that file is grandfather-capped: the gate fails a new stage
  file with no row, and the cap fails a new line — follow the in-place row
  edit, never add a row).

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

- Do not add helpers, parsers, or builders here — this leaf owns constant
  strings only. Shared code lives in `gcpcloud/extractor`.
- Do not reintroduce a folded duplicate: `versionsMarker` folds into
  `SecretVersionVersionsMarker` by identical value, documented in the pin
  test.
