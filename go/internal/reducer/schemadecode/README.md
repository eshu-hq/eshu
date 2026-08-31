# Reducer schema decode

## Purpose

Turns a stored fact payload into the typed value a reducer handler works with.

A "fact" is one observation the ingester recorded — an AWS resource, a CI run, an
SBOM component. It is stored as an opaque payload plus a fact kind. A decode seam
is the function that reads one kind and hands back a typed struct, or classifies
the payload as undecodable so the pass can skip it and keep going.

## Ownership boundary

This package owns decode seams and nothing else. It does not read from storage,
write to the graph, enqueue work, or decide what a handler does with the value.

It sits below the reducer root and below the domain families, and above
`factdecode`. Families and root reach it; it reaches nothing above itself.

## Exported surface

One hundred exported functions. Ninety-seven are decode seams named
`Decode<Kind>`: each takes a fact envelope and returns the typed value for that
kind, or an error classified as a terminal dead letter.

The other three are not decoders. `CodegraphDecodeQuarantine` and
`ServiceCatalogDecodeQuarantine` collect the quarantined facts a batch produced so
the caller can record them in one write; `FactschemaEnvelope` converts the reducer's internal
`facts.Envelope` into the `factschema.Envelope` the seams decode from.

The reducer root reaches ninety-eight of them through unexported forwarders of
their original lowercase names, in `decode_seam_compat*.go`. Two more are reached
only from reducer tests. Read the compat files for the authoritative list rather
than relying on a count here — this package is being drained family by family and
any number written down goes stale.

## Dependencies

The per-domain `sdk/go/factschema/*` packages — twenty-one of them — plus
`internal/reducer/factdecode` for `QuarantinedFact` and `NewFactDecodeError`.

That domain-schema import set is the reason these seams are not in `factdecode`.
That package's AGENTS.md carries an import budget which exists to keep domain
schema packages out of the mechanism tier, and twenty-one of them would break it.

## Telemetry

This package emits no signal of its own. A malformed required field routes
through `eshu_dp_reducer_input_invalid_facts_total`, and the reducer pass that
calls a decoder stays covered by `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`.

Every row for these files in `docs/public/observability/telemetry-coverage.md`
carries the `No-Observability-Change:` marker naming those signals.

## Gotchas / invariants

**Filenames are a contract.** The payload-usage manifest gate resolves decode
seams by the `factschema_decode*.go` basename and searches one directory below
the reducer root (#6055 rebuilt the resolver for exactly this move, because
`filepath.Glob` never crosses a `/`). A file keeps its basename when it moves
here. Rename one and its fact kinds silently drop out of the manifest — the gate
reports "no decode seams found" only when it finds a matching file with no seams
in it, so a rename is quieter than that.

The same glob is why the root compatibility files are named `decode_seam_compat*`
rather than `factschema_decode_compat*`: the latter matched the seam glob while
containing only forwarders, which failed the gate.

**A decoder is named for its fact kind, not an owner.** Several families consume
the same kind, so do not assume `factschema_decode_sbom.go` belongs to the sbom
family — `DecodeSBOMComponent` is called from both `supply_chain_impact` and
`sbom_attestation`.

**Decode failure is not fatal.** A bad payload is quarantined and the pass
continues. Do not add a decoder that returns a fatal error.

## Related docs

- [Package restructure design](../../../../docs/internal/design/package-restructure.md) — the seam this move follows
- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
- `../factdecode/README.md` — the quarantine and decode-error mechanism below this package
