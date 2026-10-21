# AGENTS.md — internal/collector/gcpcloud/extractor/serverless guidance

## Read First

1. `README.md` — what this group owns and why it exists.
2. `doc.go` — the godoc contract: init registration, redaction boundary.
3. `../README.md` and `../AGENTS.md` — the shared extractor foundation.
4. `../../AGENTS.md` — the gcpcloud source-fact contract and invariants this
   group serves.
5. `docs/public/reference/gcp-cloud-collector-contract.md` — the provider
   source, scope/generation, fact family, payload boundary, telemetry, and
   fixture contract.

## Invariants

- Never import the parent `gcpcloud` package from non-test code: both this
  package and the parent depend on the extractor leaf, so a parent edge is an
  import cycle. Test files follow the split below.
- One extractor per file, registered from init; never grow a shared parser
  switch. An extractor receives the raw resource.data transiently and returns
  only bounded, redaction-safe attributes, correlation anchors, and typed
  relationships. Never persist the data blob.
- Never return secrets, data-plane content, raw policy JSON, IPs, or response
  bodies. Fingerprint IAM members and container names through the keyed
  `redact` posture helpers. Metric labels stay bounded enums.
- File names keep the resource word (`cloud_function.go`, not `function.go`);
  `app_engine_service.go` and `run_service.go` are distinct resources and must
  not be merged or renamed into a collision.
- Keep every file under 500 lines; split before the cap.

## Common Changes

- Add a new serverless asset type in its own `<resource>.go` file with
  `init()` registration, a `<resource>_test.go` unit test, and a
  `<resource>_fixture_test.go` offline end-to-end test with its
  `testdata/assets_list_<resource>.json` fixture. Update the extractor
  catalog in `../../AGENTS-extractors-2.md` (append at the end of part 2,
  keep the numbering ascending).
- Internal (`package serverless`) tests cover extractor behavior and use the
  leaf types directly. External (`package serverless_test`) fixture tests run
  the parent parse-to-envelope pipeline; they pin relationship types and
  full resource names as literals and carry their own boundary mirror
  (`serverlessTestBoundary`). Never import parent test helpers — replicate
  the few lines with a comment citing the source.
- Update `README.md`, `doc.go`, and this file when the exported surface or
  contract changes. Then run `scripts/verify-package-docs.sh`.

## What Not To Change Without An ADR

- Do not make this package call Google Cloud APIs directly.
- Do not add graph writes, reducer admission, or query behavior here.
- Do not add a parent-package import to work around a missing helper —
  promote the helper to the extractor leaf instead.
