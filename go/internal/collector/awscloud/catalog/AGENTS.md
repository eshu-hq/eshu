# AGENTS.md - internal/collector/awscloud/catalog guidance

## Read First

1. `README.md` - why this leaf exists and what it owns.
2. `doc.go` - the godoc contract, including the no-cycle invariant.
3. `storage.go` - the first consolidated category (shape to copy).
4. `../AGENTS.md` - the awscloud root conventions the constants came from.
5. `docs/public/services/collector-aws-cloud.md` - the provider contract
   these values feed.

## Hard Rules

- MUST keep this package a leaf. Never import `awscloud`, any
  `awscloud/service` package, the AWS SDK, or any collector runtime package
  here. Either direction reintroduces the import cycle this package exists
  to break; the build, not a review, is the backstop, but do not rely on
  that — keep the import list empty by construction, with one recorded
  exception: the published fact-schema contract
  (`sdk/go/factschema/aws/v1`), which owns the shared EC2 resource-type
  tokens and imports nothing back (`compute.go` keeps the `awsv1.*`
  references verbatim, #6696).
- MUST NOT change a constant value in a consolidation move. New code reads
  `catalog.X`; old code read `awscloud.X`; the string must be identical.
  The per-category test pins every value.
- MUST land each follow-up category as one `<category>.go` file grouped by
  the service categories in #6696 (`compute.go`, `database.go`,
  `network.go`, `security.go`, `analytics.go`, and so on), consolidated
  verbatim with doc comments intact, plus its per-category value-pinning
  test. Delete the emptied `constants_<service>.go` files in the same
  commit so the root count keeps falling toward the 40-file cap.
- MUST extend the `go/cmd/collector-aws-cloud` redaction-derivation test's
  constants walk when a category carrying `Service*` constants lands. It
  resolves every `runtimebind` `ServiceKind` through the parsed table and
  fails loudly on an unknown constant.

## Common Changes

- Add a category: copy the `storage.go` shape (one `Service*` block, one
  `ResourceType*` block, one `Relationship*` block per service, doc comments
  verbatim), write the value-pinning test from the pre-move file contents,
  repoint every `awscloud.<Sym>` code reference to `catalog.<Sym>`, remove
  the emptied root files, extend the catalog coverage row in
  `docs/public/observability/telemetry-coverage.md` in place (that file is
  grandfather-capped: the gate fails a new stage file with no row, and the
  cap fails a new line — follow the in-place `2 +-` row-edit pattern), add
  the `No-Regression Evidence:` / `No-Observability-Change:` markers to this
  package's README (the perf gate reads added lines), and run the affected
  package tests plus `scripts/verify-package-docs.sh`.
- Keep every file under 500 lines; split an oversized category before the
  cap. Keep the SPDX header on every new file.
