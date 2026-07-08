# Fact Envelope

## Purpose
`factenvelope` centralizes adapter code between the public collector SDK fact
record, the durable internal fact envelope, and the factschema decode envelope.
It exists so extensionhost, reducer, and projector code do not each maintain a
separate field-by-field copy of the same envelope mapping.

## Ownership boundary
This package owns only in-memory conversion between envelope representations. It
does not validate payload schemas, write facts, claim workflow items, project
graph truth, or decide reducer behavior.

## Exported surface
See `doc.go` for the godoc contract. Callers use generated helpers to map a
validated `sdk/go/collector.Fact` into `facts.Envelope`, and to adapt
`facts.Envelope` into the `factschema.Envelope` accepted by Decode functions.

## Dependencies
The package imports `go/internal/facts`, `sdk/go/collector`, and
`sdk/go/factschema`. It intentionally stays below collector runtime packages so
it can be reused by extensionhost, reducer, and projector without pulling in
storage, graph, queue, or runtime dependencies.

## Telemetry
No telemetry is emitted. Runtime diagnostics remain owned by extensionhost,
projector, reducer, and storage callers that know the stage and failure class.

## Evidence
No-Regression Evidence: #4803 centralizes existing envelope field copies in a
generated in-memory adapter. Baseline is `origin/main` before this branch, where
extensionhost, reducer, and projector each carried local field mapping; after is
this branch head, where `factenvelope` owns the shared SDK-to-internal and
internal-to-factschema adapters. The measured input shapes are one SDK collector
fact with host-owned claim fields, payload, source ref, and tombstone metadata;
one fully populated durable `facts.Envelope`; and
version-less codegraph repository/file facts carrying both empty schema version
and the persisted `0.0.0` sentinel. The change adds no database backend call,
graph write, queue claim, lease, retry, worker, batch, or transaction shape, so
terminal queue counts and graph row counts are unchanged by construction.
Focused local proof passed with
`GOCACHE=/tmp/eshu-4803-gocache go test ./cmd/fact-envelope-adapter ./cmd/capability-inventory ./internal/factenvelope ./internal/collector/extensionhost ./internal/reducer ./internal/projector -count=1`.
The final rebased `make pre-pr` run passed gofumpt, whole-module
`golangci-lint`, go build, go vet, changed-package tests, package docs,
capability inventory, telemetry coverage, replay coverage, generated code
coverage, performance evidence, and race graph-writes.

No-Observability-Change: the adapter package emits no metrics, spans, logs, or
status rows. Invalid facts continue to surface through the existing caller-owned
signals: extensionhost result validation, reducer dead-letter/classification
paths, and projector `input_invalid` quarantine metrics/logs for the stage that
consumes the decoded fact. Operators still diagnose this path from the same
generation fact counts, projector/reducer stage metrics, dead-letter rows, and
structured quarantine logs; no telemetry contract or label cardinality changes.

## Gotchas / invariants
The public SDK wire names are not the same as the durable internal names:
`kind` maps to `FactKind`, `stable_key` maps to `StableFactKey`, and
`tombstone` maps to `IsTombstone`. Host-owned fields such as `FactID`,
`ScopeID`, `GenerationID`, `CollectorKind`, and `FencingToken` come from the
claim boundary, not from the SDK fact.

The persisted `0.0.0` schema version means "the collector emitted no version."
Adapters normalize that sentinel and the empty string to `1.0.0` for the
factschema decode seam, but they preserve a genuine unsupported major so decode
classification can fail loudly.

## Related docs
- `docs/internal/design/contract-system-v1.md`
- `docs/public/reference/fact-schema-versioning.md`
- `sdk/go/collector/README.md`
- `sdk/go/factschema/README.md`
- `go/internal/facts/README.md`
