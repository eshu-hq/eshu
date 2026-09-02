# sbomattest

Decides which SBOM and attestation documents attach to a container image, and
publishes those decisions as durable reducer facts.

This package moved out of the flat `internal/reducer` root under issue #6061. It
is a domain family: it owns one handler and the pipeline behind it, and nothing
else in the reducer depends on its internals.

## What it owns

| piece | file | what it does |
|---|---|---|
| `SBOMAttestationAttachmentHandler` | `sbom_attestation_attachment.go` | the reducer handler the runtime registers |
| `BuildSBOMAttestationAttachmentDecisions` | `sbom_attestation_attachment.go` | turns a batch of fact envelopes into per-document decisions |
| `NormalizedVerificationStatus` | `sbom_attestation_attachment_classify.go` | collapses the raw verification strings producers emit into one vocabulary |
| attachment index | `sbom_attestation_attachment_index.go` | resolves which components a document covers |
| SLSA index | `sbom_attestation_attachment_slsa_index.go` | the same for SLSA provenance statements |
| evidence bounds | `sbom_attestation_attachment_evidence_bounds.go` | caps and orders the evidence a decision carries |
| `PostgresSBOMAttestationAttachmentWriter` | `sbom_attestation_attachment_writer.go` | persists admitted decisions |

`SBOMAttachmentStatus` is a status rather than a boolean on purpose. "Attached",
"unverified" and "rejected" are three answers a caller acts on differently, and
collapsing the middle one into either neighbour loses the distinction between a
document that does not apply and one that applies but could not be verified.

## Package boundary

Imports point strictly downward. This package reaches `reducer/contract`,
`reducer/factdecode`, `reducer/factload`, `reducer/payloadcore` and
`internal/telemetry`, and it never imports the parent `internal/reducer`
package. The dependency runs the other way: the root keeps compatibility aliases
in `sbom_attestation_attachment_compat.go` so its own callers compile unchanged.

`SBOMAttestationAttachmentFactKind` is the one name shared in both directions.
It is declared in `reducer/contract` and aliased here, because the reducer
root's `supply_chain_impact` family joins against the same fact kind in its
evidence-path construction and its active-fact-kind switches. Putting it in
`contract` is what lets both name it without either importing the other; that is
the same treatment the container-image identity vocabulary got in #6431.

## Telemetry

The family registers no instrument of its own.

Documents rejected for a malformed payload increment the shared
`eshu_dp_reducer_input_invalid_facts_total` counter, which is where an operator
should look first when attachments silently stop appearing. The reducer
executions that run this handler remain covered by
`eshu_dp_reducer_executions_total` and `eshu_dp_reducer_run_duration_seconds`.

No-Regression Evidence: #6061 relocates this family without changing a line of
its logic. Every hunk inside the moved files is package-clause and import
requalification: symbols the reducer root used to supply as one-line forwarders
are now imported from the leaf that already owned them (`payloadcore` for the
payload and slice helpers, `contract` for the fact-kind name). A Go import
change adds no indirection at runtime. Measured against baseline `origin/main`
at `348bae817`: `go build ./...` and `go vet ./...` both exit 0 on the branch,
and `go test ./internal/reducer/... -count=1` passes. Binary output was not
compared and no such claim is made here.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span, or
log field. The counter this family increments and the executions that wrap it
are the same before and after the move.

## Gotchas

- **Do not import the reducer root from here.** If this package needs a symbol
  the root defines, the symbol is in the wrong place: hoist it to a shared-core
  tier (`payloadcore` for generic helpers, `contract` for vocabulary) and leave
  a root alias, rather than reaching upward.
- **`PayloadStrings` accepts both a scalar and a slice key** for the same
  logical field, because producers disagree about which they emit. Callers that
  read only one will silently miss documents from the other producer.
- **The evidence bounds are a cap, not a filter.** They bound how much evidence
  a decision carries, so a decision with truncated evidence is still a decision;
  do not read a short evidence list as weak evidence.

## Related docs

- [Reducer package](../README.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
