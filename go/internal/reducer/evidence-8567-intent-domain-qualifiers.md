# Evidence: qualify retired reducer Intent/Domain aliases (#8567)

## What changed

Bucket 6 of the #6626 alias retirement deleted the reducer-root `Intent` and
`Domain` aliases, but five use sites still referenced them bare and broke the
main build at 8a8032768f. This note accompanies the qualifier hotfix:

- `cloud_relationship_resolution.go`: `intent Intent` qualified to
  `intent reducercontract.Intent` (contract import added).
- `documentation_workload_resolution.go`: same qualification.
- `defaults_workload_handle_wiring_test.go`: three `Domain` refs qualified to
  `reducercontract.Domain` (contract import added).
- The `DomainWorkload*` constants are untouched; they still resolve through
  the re-exports in `intent.go`.

## Why no regression

No-Regression Evidence: each qualified name denotes the identical type the
bare alias denoted, so every edit is spelling-only; no signature, envelope,
row, or ordering change. Baseline is main at 8a8032768f (RED: whole-module
build fails, reducer vet reports 5 `undefined:` lines). After measurement on
this head: `go build ./...` exit 0; `go vet ./internal/reducer/...` exit 0
with no output; `gofumpt -l` and `gofmt -l` on the three Go files clean; 24
covering test executions (`TestResolveDocumentationWorkloadTargets`,
`TestWorkloadCloudRelationship*`, `TestExtractWorkloadRelationshipRows*`,
`TestDocumentationMaterializationHandler*`,
`TestHandleLadderRegistrationCarriesSiblingLookup`) with 0 failures. No graph
backend, corpus, or topology is involved: the change never reaches a backend,
so no backend/version, input-shape, or row-count measurement applies.

No-Observability-Change: no span, metric, log line, status field, or handler
path changes; operator signals are byte-identical before and after.
