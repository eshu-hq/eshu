# ifa

## Purpose

`ifa` is the first contract-layer skeleton for the Ifá conformance platform
([#4393](https://github.com/eshu-hq/eshu/issues/4393)). It defines an Odù as a
scenario-level set of `facts.Envelope` inputs and renders those facts through
the existing replay canonicalizer so later phases can compare derived graph and
query truth without re-running collectors.

## Ownership Boundary

This package owns contract-seam canonicalization only. It consumes
`facts.Envelope` values directly or through the same `LoadFacts` shape used by
the projector. It does not own collector execution, parser execution, graph
writes, reducer scheduling, fixture-pack schemas, or B-12 expectation
derivation.

## Exported Surface

- `Odu` - one scenario-level conformance case.
- `FactLoader` - minimal `LoadFacts` contract matching the projector fact-store
  seam.
- `CanonicalizeOdu` - renders one Odù into replay's deterministic canonical JSON
  form.

## Dependencies

`ifa` depends on stable internal contract packages: `facts`, `projector`,
`replay`, and `scope`. It intentionally does not import collector or parser
internals.

## Telemetry

No runtime telemetry is emitted in P0. The package is a pure local conformance
helper.

No-Observability-Change: P0 adds no runtime path, worker, queue, graph write, or
deployed service. Existing diagnostics remain the replay canonicalizer tests and
CI-gate selection output.

## Gotchas / Invariants

- The canonical form is produced by `replay.CanonicalizeValue`, not by a new Ifá
  serializer.
- Facts are cloned before rendering so caller-owned payload maps stay immutable
  after handoff.
- `Work` and `Facts` are mutually exclusive sources for one Odù run; use `Work`
  when validating the durable `FactStore.LoadFacts` seam.

## Related Docs

- `docs/internal/design/4389-ifa-conformance-platform.md`
- `go/internal/replay/README.md`
- `go/internal/facts/README.md`
