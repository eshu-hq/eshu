# AGENTS.md — canonical materialization guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../decode/AGENTS.md` — this package decodes through it and must not
   reimplement a decoder locally.
4. `../runtime/README.md` for who calls the extractors and in what order.

## Invariants

- Import `../decode`; never import `../runtime` or the root `projector`
  package. Both import this package, so the reverse is a cycle. A test that
  needs to drive a full projection belongs in `../runtime`, not here — that is
  why the `*_input_invalid`, `*_cassette` and `*_projection` suites for the
  package-registry and Terraform-state families live there.
- Extraction reads only the envelopes it is handed. No store reads, no clock,
  no map-iteration-order dependence: the Ifá replay gate compares two runs of
  `BuildMaterialization` and any of those breaks it.
- A missing required payload field is quarantined through
  `decode.PartitionFailures`, never skipped. A present-but-empty identity
  field is a valid decode that the row builders' identity gate drops. Do not
  collapse the two.
- Row identity for the `canonicalNamePathLineEntityLabels` labels is derived,
  not taken from the incoming `content_entity.entity_id`. Two Ifá fixtures
  (`go/internal/ifa/testdata/codecalls/`, `.../inheritance/`) precompute that
  hash, so changing the derivation changes them too.
- Adding a row type means updating the graph writers in
  `go/internal/storage/cypher` that persist it and the bucket declarations in
  `go/internal/content/shape` — `bucket_sync_gate_test.go` reads
  `entityTypeLabelMap` out of `materialization.go` **by file path** and fails
  at run time, not compile time, if the declaration moves.

## Verification

```bash
cd go && go test ./internal/projector/canonical/... -count=1
cd go && go test ./internal/projector/runtime/... -count=1   # the projection-level suites
cd go && go test ./internal/content/shape/... -count=1        # the bucket sync gate
```

A change to a typed family also needs `../decode`'s suite and, for a fact-kind
or payload-shape change, `eshu-contract-rigor`.
