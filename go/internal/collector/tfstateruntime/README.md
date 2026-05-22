# Terraform State Runtime Adapter

## Purpose

`internal/collector/tfstateruntime` connects the Terraform-state reader stack to
workflow claims. It does not discover Git backends itself, call cloud SDKs
directly, or commit facts. It resolves an exact candidate, opens one source,
reads serial and lineage, parses with the workflow fencing token, and returns a
`collector.CollectedGeneration` for `collector.ClaimedService`.

## Ownership boundary

The package owns the workflow-claimed adapter around Terraform-state readers.
Discovery policy, S3 credential routing, workflow claim storage, fact commits,
and graph projection stay outside this package. Approved Git-local state is
already exact and policy-checked before the runtime sees it.

## Exported surface

See `doc.go` and `go doc ./internal/collector/tfstateruntime` for the godoc
contract. Callers use `ClaimedSource`, `SourceFactory`, `SourceFactoryFunc`,
and `DefaultSourceFactory`.

## Dependencies

- `internal/collector` for claimed-source and generation contracts.
- `internal/collector/terraformstate` for discovery, source, parser, and warning
  facts.
- `internal/workflow` for current fencing tokens and work-item shape.
- `internal/telemetry` for Terraform-state spans and counters.

## Telemetry

`ClaimedSource` records Terraform-state reader metrics when instruments are
provided: snapshot observations, source size, parser duration, resource/output/
module/warning counts, redactions, safe drops, S3 not-modified reads, and
unknown-composite drops. It also starts Terraform-state source-open and
parser-stream spans. Raw locators, bucket names, local paths, and work item IDs
must stay out of metric labels; the composite-capture warning log may carry
high-cardinality diagnostic fields.

## Gotchas / invariants

- Raw state bytes stay inside `terraformstate.StateSource` readers and parser
  streams.
- Graph-backed discovery must already be gated by Git generation readiness.
- A claimed work item only produces a generation when scope ID, generation ID,
  and source run ID all match the state snapshot identity.
- Claim fencing comes from `workflow.WorkItem.CurrentFencingToken` and is passed
  into every emitted Terraform-state fact.
- S3 access stays behind the existing consumer-side `S3ObjectClient` interface;
  SDK-specific adapters and target-scope credential selection belong outside
  this package.
- `terraformstate.ErrStateMissing` and oversized state produce warning-only
  generations, not retryable collect errors. Transient source-open errors stay
  on the retry path.

## Focused tests

```bash
cd go
go test ./internal/collector/tfstateruntime -count=1
go test ./internal/collector/tfstateruntime \
  -run 'TestClaimedSourceEmitsWarningGenerationFor(MissingS3State|OversizedState)' \
  -count=1
```

## Related docs

- `go/internal/collector/terraformstate/README.md`
- `go/internal/collector/README.md`
- `go/internal/workflow/README.md`
