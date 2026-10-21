# Sweep orphan leaf guidance

## Ownership

- This package owns the orphan-sweep Cypher builders (S1 candidate read, S2
  connected-keys read, S3 clear, S4 mark, S5 sweep), the sweep policy and
  label vocabulary, the Go-side anti-join, and the per-label paging cursors.
- The parent `cypher` package owns the `Executor` and `Statement` types, the
  `OperationCanonicalRetract` operation, and every other writer. This leaf
  imports the parent for those (Pattern B, `edge/writer` precedent).
- The parent and white-box parent tests must NEVER import this package
  (import cycle). Only external wiring (`cmd/reducer`, `cmd/*`) and the
  `edge/writer` leaf (its `Reader` field) import it.

## Invariants

- Keep every Cypher template byte-identical when moving or refactoring code;
  statement or predicate changes need their own issue with `EXPLAIN` and
  contention proof per `cypher-query-rigor`.
- Never add a relationship-existence predicate (`NOT (n)--()`, `(n)--()`,
  `COUNT { (n)--() }`) to any read or write here: all are mis-evaluated on
  the pinned NornicDB backends. New sweep statements need a live-backend
  proof, not just unit coverage.
- Keep the S1/S2 reads and the S3/S4/S5 writes on the same node guard
  (`orphanSweepNodeGuard`) and the same identity binding
  (`orphanSweepIdentityProperties`): a node excluded at read time but not at
  write time (or vice versa) is a wrong-delete or a leak.
- Keep the UNWIND binding variable named `candidate_key`, never `key`:
  reusing the RETURN alias silently returns zero rows on NornicDB.
- Keep Module keyed on (name, lang) everywhere (reads, writes, cursor,
  anti-join): keying on name alone lets a connected same-named sibling mask
  a true orphan.
- Keep `int64Count` behavior-identical to its twin in the edge-writer
  leaf (`../edge/writer/provenance_counts.go`): the two sides cannot
  share one definition.
- Keep the package clause as `package orphan`; external callers use the
  default name, or `sweeporphan` where `orphan` is taken
  (the maintenance `orphan` package in `cmd/reducer`).
- Never import the parent-test fakes from here; the duplicated live-bolt
  harness (`bolt_harness_test.go`) names its root originals.
