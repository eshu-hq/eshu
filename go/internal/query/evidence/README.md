# evidence

## Purpose

Owns the evidence handler family: the relationship-evidence drilldown, the
evidence-citation packet, the admission decision reads, the live
evidence-bundle composer, and the deployable-unit packet route, all served
through `Handler.Mount`. It also publishes the `EvidenceCitationHandle`
alias for the canonical evidence-citation handle wire shape, so sibling
packages can compose derived answer and report views without redefining or
weakening the handle contract the evidence-citation endpoint resolves. See
`doc.go` for the contract.

## Ownership boundary

Owns the handler (`handler.go`), the citation synthesis
(`citation.go`, `citation_unified.go`, `content_entity_access_batch.go`),
the admission decision family (`admission_decisions.go`,
`admission_decision_store.go`, `admission_decision_types.go`), the live
bundle composer (`bundle_live.go`), the deployable-unit packet route behind
its responder seam (`deployable_unit_packet.go`), and the handle alias
(`citation_public.go`). The relationship read model
(`ContentReader.RelationshipEvidenceByResolvedID` in root's
`evidence_read_model.go`) and the citation file batch
(`ContentReader.EvidenceCitationFiles` in root's
`content_reader_evidence_citation.go`) stay with the content/read family;
this package reaches them through the `RelationshipEvidenceReadModelStore`
and `EvidenceCitationFileStore` ports. The packet envelope
(`BuildDeployableUnitPacket` and the refusal composer) stays in the root
query package until the investigation/packet leaf lands. The canonical
handle type, coverage, and response shapes live in
`internal/query/querycontract/evidence`; auth gates live in
`internal/query/auth`.

## Layout

- `handler.go` — `Handler`, its routes, and the relationship drilldown.
- `handler_tracing.go` — the family tracer and handler-span helper.
- `http_error.go` — the fixed-body read-failure helper.
- `citation.go` — the citation packet synthesis.
- `citation_unified.go` — the canonical-projection helpers.
- `citation_public.go` — `EvidenceCitationHandle`, an alias for
  `querycontract/evidence.EvidenceCitationHandle`.
- `content_entity_access_batch.go` — the batch entity-content access path.
- `admission_decisions.go` — the admission decision list route.
- `admission_decision_store.go` — the Postgres admission decision read model.
- `admission_decision_types.go` — the admission decision read shapes.
- `bundle_live.go` — the live evidence-bundle composer.
- `deployable_unit_packet.go` — the deployable-unit packet route behind the
  `DeployableUnitPacketResponder` seam.
- `capabilities.go` — the family capability support rows, shared with
  root's contract matrix.

## Dependencies

`internal/query/querycontract` (and its `evidence`/`repository` leaves),
`internal/query/auth` (tests only), `internal/query/tracing`,
`internal/query/supply/chain/impact` (the repository selector helper),
`internal/status` and its `collector`/`queue` leaves, `internal/evidencebundle`,
`internal/storage/postgres` (`db`, `admission`), `internal/telemetry`, and
`internal/truth`. The root query package imports this package for the
handler alias; this package must never import the root query package.
