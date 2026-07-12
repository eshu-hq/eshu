# AGENTS.md - internal/ifa/graphdump guidance

## Read first

1. `README.md` - package purpose, ownership boundary, and performance/
   observability evidence for the `ifa graph-dump` verb.
2. `doc.go` - full design rationale: content addressing vs. ID addressing,
   the reused canonical JSON core, and the over-normalize/under-normalize
   tradeoff.
3. `reader.go` - `Node`, `Edge`, `Reader`; read this before adding a new
   `Reader` implementation anywhere.
4. `canonical.go` - `Canonicalize`/`Digest`/`Equal` and the node/edge record
   shape.
5. `normalize.go` - `denylistKeys`/`normalizeProps`/`sortedLabels`; read the
   evidence bar in `doc.go` before adding a denylist entry.
6. `go/cmd/ifa/graphdump_reader.go`, `go/cmd/ifa/graph_dump.go` - the only
   production `Reader` implementation and the `ifa graph-dump` verb that
   calls it.

## Invariants

- This package stays driver-free: no NornicDB/Neo4j/Bolt import, no Docker or
  live-graph dependency in any test here. A live `Reader` belongs in
  `go/cmd/ifa`, not here — see `reader.go`'s own doc comment and the
  README's "Ownership Boundary" section for why.
- `Node`/`Edge` must never carry a backend element ID field. Canonical
  identity is content-addressed (sha256 of sorted labels + normalized
  props); adding an ID-like field would reintroduce the exact run-local
  non-determinism this package exists to eliminate.
- Reuse `go/internal/replay.CanonicalizeValue` for JSON serialization. Do not
  write a second canonicalizer; pass the zero-value `replay.CanonicalOptions`
  (not `DefaultCanonicalOptions`), since this package's content is graph
  truth, not cassette-shaped run metadata — see `canonical.go`'s
  `canonicalOptions` doc for exactly why.
- `denylistKeys` (`normalize.go`) grows only on evidence: a demonstrated
  volatile/run-local property key observed on a real populated graph, with
  the concrete write path documented next to the entry. Do not add a
  hypothetical entry, and do not widen the denylist to "fix" a determinism
  test failure without first proving the property is genuinely run-local
  rather than a real concurrency defect the test is correctly catching (see
  `doc.go`'s over-normalize/under-normalize tradeoff and the repo's
  "Serialization Is Not A Fix" rule: a false green here hides exactly the
  kind of defect this package is supposed to surface).
- `Canonicalize`/`Digest`/`Equal` must stay deterministic: no wall-clock time,
  randomness, network, or storage side effects.
- `ifa graph-dump` is read-only. Do not add a write path, schema DDL, or a
  Cypher statement other than the two proven `MATCH` reads in
  `graphdump_reader.go` without re-running this package's
  prove-the-theory-first evidence bar (a shim proving the new query works on
  NornicDB) and updating the README's performance/observability evidence
  section in the same change.

## Memory + byte-identity invariants (issue #5009)

- The canonical OUTPUT is byte-identical and is the determinism matrix's whole
  comparison basis. Any change to record shape, sort, indentation, or framing
  MUST keep `TestCanonicalizeGoldenDigests` green — those pinned digests are the
  regression, captured from the pre-streaming implementation.
- `Reader` is STREAMING (`StreamNodes`/`StreamEdges` with a yield callback). Do
  not reintroduce whole-slice `Nodes()`/`Edges()`: a live reader must never
  materialize the entire node/edge set (each `Edge` duplicates both endpoints'
  property maps, so the struct set dwarfs the byte set at scale).
- `Canonicalize` assembles the final document directly from the sorted record
  bytes (`assembleGraph`), NOT by decoding them back into `map[string]any`. The
  decode round trip re-exploded memory; do not restore it. If you change the
  shared canonicalizer's indentation, `assembleGraph` must follow (the digest
  test catches drift either way).
- `scale_memaudit_test.go`'s `TestMemAuditCanonicalizeScale` (behind the
  `ifamemaudit` build tag, so it runs in NO ordinary CI lane — it allocates
  ~1.4 GiB) is the before/after memory evidence. Run it explicitly with
  `go test -tags ifamemaudit -run TestMemAuditCanonicalizeScale ./internal/ifa/graphdump/`;
  keep it runnable for any further memory work (output streaming into the hash,
  external merge-sort). Do NOT re-gate it on `testing.Short()` — no repo lane
  passes `-short`, so it would run unguarded in the authoritative `-race` shard.

## Verification

```bash
cd go && go test ./internal/ifa/graphdump/... -count=1
cd go && go test -race ./internal/ifa/graphdump/... ./cmd/ifa/... -count=1
ESHU_PERFORMANCE_EVIDENCE_BASE=origin/main bash scripts/verify-performance-evidence.sh
```
