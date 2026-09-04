# awscloud

Projects AWS cloud facts into two canonical truth surfaces: the
`CloudResource -> ContainerImage` graph edge and the `aws_cloud_runtime_drift`
orphan/unmanaged finding.

This package moved out of the flat `internal/reducer` root under issue #6061.
It is a domain family: it owns two handlers and the pipeline behind them, and
nothing else in the reducer depends on its internals.

## What it owns

| piece | file | what it does |
|---|---|---|
| `AWSCloudImageMaterializationHandler` | `aws_cloud_image_materialization.go` | the cloud-image edge handler and its single-keyspace readiness gate |
| cloud-image edge extraction | `aws_cloud_image_join.go` | resolves `lambda_function_uses_image` relationships to `(CloudResource, ContainerImage)` edge rows |
| `AWSCloudRuntimeDriftHandler` | `aws_cloud_runtime_drift.go` | evaluates the drift rule pack and drives the fencing-token-ordered write |
| drift write admission | `aws_cloud_runtime_drift_admission.go` | the begin-before-mutate admission CAS (#5848) and the fencing-token issuer contract |
| drift readiness defer | `aws_cloud_runtime_drift_readiness.go` | the bounded state_snapshot-pending defer (#5837 root cause) |
| `PostgresAWSCloudRuntimeDriftWriter` | `aws_cloud_runtime_drift_writer.go` | the transactional insert + generation-authoritative retire |
| drift writer SQL | `aws_cloud_runtime_drift_writer_queries.go` | the retire statement, split out to stay under the 500-line cap |

Both handlers are deliberate under-approximations on their forward-looking
target: an unscanned `:ContainerImage` node or a not-yet-admitted evidence pass
degrades to a counted skip or a retryable defer, never a fabricated edge or a
stale finding overwriting a fresher one.

## Package boundary

Imports point strictly downward. This package reaches `reducer/contract`
(aliased `reducercontract`), `reducer/cloudjoin`, `reducer/containerimage`,
`reducer/factdecode`, `reducer/factload`, `reducer/factwrite`,
`reducer/gpphase`, `reducer/payloadcore`, `reducer/schemadecode`,
`internal/correlation/drift/cloudruntime` and its `engine`/`model`/`rules`
siblings, `internal/facts`, `internal/telemetry`, `internal/truth`, and the
factschema SDK. It never imports the parent `internal/reducer` package. The
dependency runs the other way: the root keeps compatibility aliases in
`aws_cloud_family_compat.go` for the wiring types `cmd/reducer`,
`internal/storage/postgres`, and `internal/replay/costcounting` name
(`AWSCloudImageMaterializationHandler`, `CloudResourceContainerImageEdgeWriter`,
`AWSCloudRuntimeDriftHandler`, `PostgresAWSCloudRuntimeDriftWriter`, the
evidence-loader/writer/readiness-checker/fencing-token-issuer interfaces, the
write request/result types, and the three failure-class constants).

Two shared leaves this family depends on (but does not own) were already
carved out of the reducer root by earlier moves, because their symbols have
consumers on both sides of the boundary:

- `reducer/cloudjoin` — the CloudResource join index, plus `ResolveSource` and
  `SplitAWSFactEnvelopes` (hoisted here in the same #6061 pass this family
  moved in, since the reducer root's AWS relationship materialization slice
  — which has NOT moved — calls the identical logic and a family package may
  never import the reducer root);
- `reducer/containerimage` — `ContainerImageExistenceLookup`, moved out of the
  reducer root in an earlier #6061 pass (#6507). This family imports it
  directly rather than duck-typing a local copy, since both packages are now
  leaves with no import edge between them.

## Telemetry

| signal | where | dimensions |
|---|---|---|
| `eshu_dp_aws_cloud_image_edges_total` | `aws_cloud_image_materialization.go` | `resolution_mode` |

The drift handler's admitted-candidate counts flow through
`internal/correlation/drift/cloudruntime.RecordEvaluation` onto that package's
own instruments; this family's `Handle` calls it but registers no instrument
of its own for that path. A readiness defer or a superseded write classifies
as a retryable, non-counting failure class
(`AWSCloudRuntimeDriftStatePendingFailureClass`,
`AWSCloudRuntimeDriftWriteSupersededFailureClass`) surfaced through the
existing `eshu_dp_reducer_retry_surge_total{failure_class}` and a structured
log line, never a metric of its own. Facts rejected for a malformed payload
increment the shared `eshu_dp_reducer_input_invalid_facts_total` counter
instead, and both handlers' runs stay covered by
`eshu_dp_reducer_executions_total` and `eshu_dp_reducer_run_duration_seconds`.

No-Regression Evidence: #6061 relocates this family's production logic
without changing it. Every hunk inside the seven moved production files is a
package clause, an import requalification, or an identifier
requalification: symbols the reducer root supplied as one-line forwarders are
now imported from the leaf that already owned them (`payloadcore` for the
deref/tally/payload helpers, `contract` for the intent, result, ownership and
domain vocabulary, `factload`/`factdecode`/`schemadecode`/`gpphase`/`factwrite`
for the rest, `containerimage` for the target-existence lookup). Two symbols
with a second, still-in-root caller (`resolveCloudResourceSource`,
`splitAWSFactEnvelopes`, both called from the reducer root's
`aws_relationship_join.go`/`aws_relationship_materialization.go`, which have
NOT moved) were hoisted into `cloudjoin` with their bodies unchanged, and the
root keeps a one-line forwarder at each old call site. The one behavioral
seam is the readiness gate: `AWSCloudImageMaterializationHandler.sourceNodesReady`
used to build a full phase *state* and read `.Key` off it, and now calls
`gpphase.KeyFromScope` directly — the same function the root's own state
constructor calls, so the key is byte-identical, and the discarded half was
two timestamps the gate never read. A Go import change adds no indirection at
runtime. Measured on this branch: `go build ./...` exits 0,
`go vet ./internal/reducer/...` exits 0, and
`go test ./internal/reducer/... -count=1` passes, including this package,
`cmd/reducer`, `internal/storage/postgres`, `internal/query`, and
`internal/replay/costcounting`. Binary output was not compared and no such
claim is made here.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span, or
log field. The counter and failure classes above are the same before and
after the move.

## Gotchas / invariants

- **Do not import the reducer root from here.** If this package needs a
  symbol the root defines, the symbol is in the wrong place: hoist it to a
  shared-core tier (`payloadcore` for generic helpers, `contract` for
  vocabulary, `cloudjoin` for CloudResource identity/join logic) and leave a
  root alias, rather than reaching upward.
- **The fencing token must never be a reducer host's wall clock.** See
  `aws_cloud_runtime_drift_admission.go`'s doc comment on
  `awsCloudRuntimeDriftAdmissionQuery`: `AWSCloudRuntimeDriftFencingTokenIssuer`
  exists specifically because a host-clock-derived token silently reintroduces
  a clock-skew ordering bug (#5875 P1). `AWSCloudRuntimeDriftWrite.FencingToken`
  is populated only from that issuer, at evidence-read time, never at
  write-commit time.
- **The readiness-gate check runs BEFORE the evidence load, not after.** See
  `checkAWSCloudRuntimeDriftReadinessBeforeLoad`'s doc comment
  (`aws_cloud_runtime_drift_readiness.go`) for the TOCTOU a post-load check
  would reopen (#5875 P1).
- **`splitAWSFactEnvelopes`/`resolveCloudResourceSource` live in `cloudjoin`
  now, not here.** They were hoisted (not duplicated) because the reducer
  root's still-in-root AWS relationship slice calls the same logic; do not
  re-add a local copy.
- **`ContainerImageExistenceLookup` filters AFTER extraction, before any
  metric or evidence read.** See `filterRowsToExistingContainerImageTargets`'s
  doc comment: extraction alone cannot know whether a target `:ContainerImage`
  node actually exists, so `Handle` must reclassify a resolved-but-unmaterialized
  row before `eshu_dp_aws_cloud_image_edges_total` or `CanonicalWrites` reads
  the row/tally.

## Related docs

- [Reducer package](../README.md)
- [AWS relationship edge materialization design](../../../../docs/internal/aws-relationship-edge-materialization-design.md)
- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
