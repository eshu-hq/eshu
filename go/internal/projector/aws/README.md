# AWS Projector Intents

## Purpose

`internal/projector/aws` groups AWS-specific reducer-intent builders under a
responsibility-first path.

## Ownership boundary

This directory is a namespace, not a runtime layer. Leaf packages select
trigger facts and return reducer-intent values. The parent
`internal/projector` package continues to own lookup construction, ordered
fan-out, projection lifecycle, queue writes, retries, and telemetry. Reducer
packages continue to own graph materialization.

## Exported surface

None. Import the leaf package that owns the required intent family.

See `doc.go` for the package contract.

## Dependencies

None. The namespace package contains documentation only.

## Telemetry

None. Leaf builders emit no telemetry; projector and reducer runtime signals
retain their existing ownership.

## Gotchas / invariants

- Do not put orchestration, shared mutable state, or provider-wide helpers in
  this namespace.
- AWS leaf builders depend on `internal/projector/intent`, not the parent
  projector package.
- Keep trigger facts, reducer domains, entity keys, reasons, source-system
  derivation, and root fan-out positions stable during path-only moves.

## Child packages

- `cloud/image` builds the AWS Lambda-to-container-image materialization
  intent.
- `ec2`, `rds`, and `s3` build service-specific posture and relationship
  intents.
- `relationship` and `resource` build provider-wide AWS graph
  materialization intents.

## Rename verification

No-Regression Evidence: for #6627, the reviewed baseline is
`eff73398a02cd82b4c4fdf9a698607f34f3b3902`; the measured implementation commit
is `37a2a9770f8b3e7a621317edb7c39b2c5c12c0e0`. The following evidence-only
commit changes this README, not the measured Go surface. This slice changes
package paths, four builder identifiers, and their references, not builder
behavior. A
normalized repository-wide Go import-graph comparison found 44,434 production,
test, and external-test edges at both revisions after applying the declared
path map. The projector test inventory likewise remained at 454 after the path
and four test-name substitutions. `go test ./internal/projector/... -count=1`
and `go test -race ./internal/projector/... -count=1` passed; the ordered
fan-out tests preserved all 44 probes in their prior order.

The exercised fixtures include present and absent trigger facts, removal of an
AWS Lambda image relationship, earliest-kind anchoring, source-system fallback,
and valid and invalid EC2 and S3 posture payloads. Backend/version: not
applicable to these in-process structural and fixture checks; no backend was
started. Terminal queue/row counts: not measured because these checks perform
no durable queue or graph writes. The unchanged cassette tree and B-12 golden
snapshot hashes provide artifact-equivalence evidence, not latency or live
convergence measurements.

Safety: trigger selection, `FirstOfKind` anchoring, returned intent fields,
shared readiness keys, dispatcher order, and dependency direction are
unchanged. The move introduces no lookup construction, I/O, allocation
strategy, worker, lock, retry, or queue boundary.

No-Observability-Change: for #6627, these namespace packages have no runtime
implementation. Existing projector enqueue/run metrics, reducer execution
metrics and spans, readiness status, and materialization logs retain their
names and owners. `bash scripts/verify-telemetry-coverage.sh` passed against the
relocated source references; no live telemetry observation is claimed.

## Related docs

- `go/internal/projector/README.md`
- `docs/internal/design/naming-remediation.md`
