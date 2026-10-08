# Agent instructions: decode/workitem

Scope: `go/internal/query/decode/workitem/` (package `workitem`). Read
`doc.go` and `README.md` first. This leaf was created for #6623 on
arbiter-eshu verdict 1, which is the "ask before adding a new shared home"
authorization `workitem/AGENTS.md` requires.

## Invariants

- Keep the one-argument seam shape. Every wrapper takes exactly one
  `DecodeInput` value and returns `(workitemv1.<Struct>, error); the
  payload-usage manifest gate's seam parser recognizes that shape, and a
  multi-argument wrapper is silently ungated.
- Keep the seam file named `factschema_decode*.go`. The gate globs that
  pattern under `go/internal/query`; any other name drops all nine seams.
- One import spelling everywhere: `workitemdecode
  "…/query/decode/workitem"`. The gate attributes qualified decode calls
  only through `KnownDecodeQualifiers`, which carries exactly that string.
  A bare `workitem` import or any other alias silently unattributed call
  sites; both drift tests (`TestNoDecodeSeamIsReachedThroughAnUnknownQualifier`,
  `TestDecodePackagesAreImportedWithoutAnAlias`) fail on it.
- No per-consumer forwarders. An unexported `decodeX` wrapper in a consumer
  does not match the seam `FuncName`, so its bindings are unattributed dead
  weight. Consumers call this leaf directly.
- This package MUST NOT import the query root, `incident/`, the workitem
  family (`internal/query/workitem`), or `querycontract`. Leaf imports are
  stdlib + `log/slog` + `internal/query/decode` + the factschema SDKs only.
- `LogEvidenceDecodeDrop` is a helper, never self-emitted. Callers decide
  to call it; do not add package-level logging, metrics, or spans here.

## Common changes

Adding a tenth `work_item.*` wrapper: match the exact seam shape above,
reference its `factschema.FactKind*` constant in the body, and confirm the
manifest gate attributes it (run the gate; a forgotten schema mapping fails
loudly via `UnmappedSeamFactKinds`).

## Verification

From `go/`: `go test ./internal/query/... ./internal/payloadusage/...
-count=1`, then `bash scripts/verify-payload-usage-manifest.sh` and `bash
scripts/verify-factschema-diff.sh` from the repo root. After any logic
touch, re-run the wiring mutation (force `DecodeRecord` to return an
error, confirm the leaf and both consumers' suites fail RED, revert to
green).
