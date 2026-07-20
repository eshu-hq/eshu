# Environment Alias Contract

## Purpose

Centralizes environment naming across the Eshu platform. Before this package,
environment tokens, aliases, and normalization rules were duplicated across
three reducer/query locations with inconsistent case handling.

## Ownership boundary

Owns the canonical environment-alias table, the single normalization rule,
the known-token set for artifact-path detection, and the closed evidence-class
and state vocabularies. Does not own fact emission, environment admission, or
the USES edge exact-join (documented follow-on).

## Exported surface

- `Normalize(raw string) string` — trim + lowercase, the platform normalization rule.
- `Canonical(raw string) string` — normalize + alias-map; unknown values pass through normalized.
- `IsKnownToken(token string) bool` — 12-token union for artifact-path detection.
- `EvidenceClass` — closed typed string for evidence provenance classes.
- `State` — closed typed string for environment binding states.

See `doc.go` for the full godoc contract.

## Dependencies

No internal Eshu dependencies. Standard library only (`fmt`, `strings`).

## Telemetry

No-Observability-Change: pure string functions with no new I/O. All consumers
surface their environment results through existing pipeline signals
(reducer execution counters, query handler spans, HTTP route attribution).

## Performance

Benchmark Evidence: `BenchmarkWorkloadMaterializationSubDurations` (M5 Max,
darwin/arm64, `-count=3`, NornicDB/Postgres not exercised by the benchmark)
measured 121.8-162.4 ns/op, 256 B/op, 2 allocs/op with this package on the
projection path — `Normalize`/`Canonical` are trim+lowercase+map-lookup and sit
inside the existing per-row projection work.

No-Regression Evidence: the full B-7 golden-corpus gate
(`scripts/verify-golden-corpus-gate.sh`, 2026-07-20, branch
5473-environment-alias-contract) passed 432/0 with the B-12 snapshot
byte-identical to `origin/main`, proving corpus row-set equivalence; pipeline
wall time 33s against the 900s budget (baseline 15s, ceiling 2x). Input shape:
the 21-repo minimal corpus with B-10 cassettes; terminal state: snapshot diff
empty. Safe because every migrated consumer delegates to the same alias data
it used before (table-equality unit tests) and the two expected-delta sites
(alias merge, case-fold) are pinned by dedicated regression tests.

## Gotchas / invariants

- Unknown values pass through normalized — never rejected, never invented.
- The alias table maps only production→prod, staging→stage, development→dev.
- `IsKnownToken` is case-sensitive (callers must normalize first).
- The package is a pure data contract; it does not read config, files, or
  the network.

## Related docs

`docs/public/reference/environment-alias-contract.md` — the full contract doc.
