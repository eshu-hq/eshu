# evidence

## Purpose

Publishes the `EvidenceCitationHandle` alias for the canonical
evidence-citation handle wire shape, so sibling packages can compose derived
answer and report views without redefining or weakening the handle contract
the evidence-citation endpoint resolves. See `doc.go` for the contract.

## Ownership boundary

Owns only the handle alias (`evidence_citation_public.go`). The
evidence-citation endpoint (`POST /api/v0/evidence/citations`), the citation
synthesis helpers (`evidence_citation.go`, `evidence_citation_unified.go`),
the relationship read model (`evidence_read_model.go`), the live bundle
composer (`evidence_bundle_live.go`), and the `EvidenceHandler` stay in the
root query package until the querycontract evidence-authz pre-slice (#6642,
the blocking dependency) and then the full evidence leaf move. The canonical
handle type, coverage, and response shapes live in
`internal/query/querycontract/evidence`; auth gates live in
`internal/query/auth`.

## Layout

- `citation_public.go` — `EvidenceCitationHandle`, an alias for
  `querycontract/evidence.EvidenceCitationHandle`.

## Dependencies

`internal/query/querycontract/evidence` only. The root query package and its
other leaves import this package for the alias; this package must never
import the root query package.

## Telemetry

No-Observability-Change (#6642): this move adds no spans, metrics, or logs.
Citation telemetry stays where it was — the
`POST /api/v0/evidence/citations` handler span
(`telemetry.SpanQueryEvidenceCitationPacket`) and the fixed failure bodies
in the root query package — and no caller-visible shape changes.

## Performance

No-Regression Evidence (#6642): this slice moves only the
`EvidenceCitationHandle` alias — a compile-time `=` alias for the
querycontract/evidence handle — and repoints its eight qualifier users with
no other code change. No Cypher, SQL, queue, lease, batching, list-bound,
or retry change; no new runtime branch. Baseline is origin/main
`8941b78d0`: the alias resolves to the identical type before and after, so
every handle value, response shape, and authz decision is unchanged (35
qualifier refs before and after, per-file equal). The alias adds zero
runtime work — it disappears at compile time. Local proof: `go build` and
`go vet` green on `query/evidence` and `query/querycontract/evidence`,
`gofumpt -l` clean, `go doc` renders the alias. The cgo-cone test binaries
(serviceintel, answerquality, reportbundle, cli/servicereport, mcp) cannot
link on this host — pre-existing go-sitter-forest/perl C-toolchain failure,
identical on the untouched base — and run in CI.
