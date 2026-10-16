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
`reducer/factdecode`, `reducer/factload`, `reducer/factwrite`,
`reducer/payloadcore`, `reducer/schemadecode`, `internal/boundedset`,
`internal/facts`, `internal/telemetry` and `internal/truth`, and it never
imports the parent `internal/reducer` package. The dependency runs the other
way: the root keeps compatibility aliases in the sbom stanza of `compat_correlation.go`
(plus `intent.go` for the fact-kind constant) so its own callers compile
unchanged.

`SBOMAttestationAttachmentFactKind` is the one name shared in both directions.
It is declared in `reducer/contract` and aliased here, because the reducer
root's `supply_chain_impact` family joins against the same fact kind in its
evidence-path construction and its active-fact-kind switches. Putting it in
`contract` is what lets both name it without either importing the other; that is
the same treatment the container-image identity vocabulary got in #6431.

## Telemetry

Handle emits `eshu_dp_sbom_attestation_attachments_total` (labeled by domain
and outcome) once per non-empty attachment status after building a batch of
decisions.

Documents rejected for a malformed payload increment the shared
`eshu_dp_reducer_input_invalid_facts_total` counter instead, which is where an
operator should look first when attachments silently stop appearing. The
reducer executions that run this handler remain covered by
`eshu_dp_reducer_executions_total` and `eshu_dp_reducer_run_duration_seconds`.

No-Regression Evidence: #6061 relocates this family's production logic without
changing it. Almost every hunk inside the moved production files is
package-clause and import requalification: symbols the reducer root used to
supply as one-line forwarders are now imported from the leaf that already
owned them (`payloadcore` for the payload/slice/int helpers, `contract` for
the fact-kind name and container-image-identity vocabulary, `factload`/
`factdecode`/`factwrite`/`schemadecode` for the rest). The one exception is
`NormalizedVerificationStatus`, relocated from `sbom_attestation_attachment.go`
to `sbom_attestation_attachment_classify.go` to keep the former under the
package's 500-line cap -- a declaration move, not a logic change. Moved test
files additionally gained local copies of a handful of fixture-builder helpers
and a batch-insert test double/decoder that other reducer-root test files also
use (Go test files cannot share unexported symbols across package boundaries),
and one fixture path (`sbom_attestation_runtime_attachment_test.go`) was
corrected from `../collector/...` to `../../collector/...` after the file
moved one directory deeper -- a real fix to a path that broke on the move, not
a production-code change. A Go import change adds no indirection at runtime.
Measured against baseline `origin/main` at `348bae817`: `go build ./...` and
`go vet ./...` both exit 0 on the branch, and `go test ./internal/reducer/...
-count=1` passes, including this package. Binary output was not compared and
no such claim is made here.

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

## Evidence

No-Regression Evidence (#6626 document importer batch): baseline
`eac086093` exposed five reducer-root aliases used by the document parser
integration test and scannerworker generator attachment test. The after commit
removes those aliases and points both tests at the same `sbomattest` types,
constants, and builder. With the aliases removed before caller edits, recursive
`go test ./internal/collector/sbom/document/... ./internal/reducer/... -run
'^(TestParserFeedsReducerAttachmentTruth|TestScannerWorkerGeneratedSBOMFactsAdmittedByReducerAttachment)$'
-count=1` failed to compile at those caller references (exit 1). After the
import edits, unfiltered recursive tests over those two trees passed (exit 0),
as did recursive `go build` and `go vet`. The backend is in-memory Go
1.26.9 linux/arm64; inputs are the existing CycloneDX/SPDX subject, missing
subject, and malformed fixtures plus scannerworker-generated SBOM facts.
There are no queue or graph writes in this batch, so terminal queue and row
counts are not applicable. The removed wrapper had no runtime caller; tests
now invoke the identical leaf implementation directly. No runtime speedup is
claimed.
No-Observability-Change: this batch changes no metric, span, log, status,
worker, queue, or runtime setting. The existing attachment and reducer
execution counters described above still cover the same production path.

No-Regression Evidence (#6626 batch 2): this change moves the last callers
of `ComponentEvidence` (type alias), `ComponentEvidenceLess` and
`ComponentEvidenceTupleEqual` (thin forwarders with identical signatures),
and `MaxSBOMAttachmentComponentEvidenceRows` (const) off the reducer root
onto this leaf, and deletes the 4 emptied entries from
`compat_correlation.go`. The 2 caller files are both in `internal/query`
(`sbom_attestation_attachment_rows.go`, 9 sites, plus its legacy-bound
test); no other external or root-internal caller exists. No type shape,
wire field, SQL text, or executable statement changes: every hunk
requalifies an identifier or edits an import path only.
Measurement: packages ok on both sides with identical sets (ledger:6626-sbomattest-batch2-after).
Sibling before-row `6626-sbomattest-batch2-before`; baseline `41e247aeaf`
vs measurement commit `12d5520783` (the two ledger rows and this Evidence
section are the only later changes), with the ok-package set byte-identical
after timing strip (156/156).
`go test -list` inventory is identical on both sides (10156 tests).
Backend/version: go1.26.2 linux/amd64, in-memory; no backend touched. Input
shape: n/a (no runtime input). Terminal queue/row counts: none — no queue,
lease, Cypher, or SQL path is touched. Contract gates green on the branch:
`verify-openapi.sh` (261/261 routes), `verify-contracttest.sh`,
`verify-package-docs.sh`, and `verify-performance-evidence.sh`. The change
is safe because it cannot alter runtime behavior: the compiler resolves the
same types and functions through their new paths, and the entry deletions
are compile-enforced total — any missed caller would fail the build.
No-Observability-Change: no metric, span, log line, or status output changes;
telemetry evidence is the unchanged test outcomes above.
