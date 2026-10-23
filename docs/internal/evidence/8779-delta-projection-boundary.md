# Evidence #8779: delta projection over its union base

Every git delta dead-lettered in the projector as `projection_bug`. The #7797
delta-base union loads base rows that keep the base generation id, and
`validateFactBoundary` required every fact to carry the delta's id. The fix
carries the union base id out of the same load into `Project`, admits exactly
that base, derives reducer intents from the delta's own facts only, and
refuses the additive `union_scope_mismatch` overlay before any write. The
operator record is the "Projection boundary" section of
`docs/public/operate/graph-delta-union.md`.

Root-Cause Evidence: the live proof
`TestDeltaGenerationProjectsOverUnionBaseLive`
(`go/internal/storage/postgres/delta_union_projection_live_test.go`) at the
RED commit ("RED for delta projection through the union base") ended
with the delta work row `dead_letter`,
generation `failed`, `failure_class = projection_bug`, and the message
`fact "…" generation_id "gen-8779-full-a" does not match generation
"gen-8779-delta-b"`, matching the production report.

## No-Regression Evidence:

- Non-union path (every full generation, and every delta before #7797
  resolved a base): `generationOwnedFacts` returns the input slice unchanged,
  and the only other addition is one string compare per fact. Measured with
  the existing 5,000-fact `BenchmarkProjectionCloneRemovalProof/Borrow`
  (`go test ./internal/projector/runtime/ -run '^$' -bench
  'BenchmarkProjectionCloneRemovalProof/Borrow' -benchmem -count=6`), base
  at the RED commit in a throwaway worktree against this branch, compared with
  `benchstat` on one Apple M-series host: 11.25 ms ± 1% against 11.27 ms ± 0%
  (p=0.699), 19.27 MiB/op and 74.05k allocs/op on both sides, no significant
  change.
- Union path: there is no prior baseline, because every union projection
  dead-lettered at the first base fact. The added cost is one O(n) pass that
  copies the delta-owned facts and one `facts.ParseDeltaScope` pass over them.
  Canonical and content builders run over the same union set the overlay
  already produced.
- Postgres: the load issues the same statements as before. A delta union is
  still four queries and a full generation two, with no second base resolve;
  `TestFactStoreLoadProjectionFactsNamesUnionBase` asserts the count.
- Live drain on local PostgreSQL 18.6 (127.0.0.1:5432, isolated bootstrapped
  schema, three files per generation): full A drained in about 31 ms, delta B
  in about 22 ms, and the refused chained delta C in about 12 ms
  (`ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN=… go test ./internal/storage/postgres/
  -run TestDeltaGenerationProjectsOverUnionBaseLive -count=1 -v`). Terminal
  state: A superseded, B active with its work row `succeeded`, C superseded
  with `projector_delta_base_missing`, and no reducer row added by B.
- Concurrency: no lock, claim, lease, batch, or worker change. The base id is
  read from the overlay's own resolve inside one `LoadProjectionFacts` call,
  so a supersede or prune between two reads cannot split the check from the
  facts.

## Observability Evidence:

- The `build_projection` stage log now carries `generation_fact_count`
  (delta-owned rows) and `union_base_fact_count` (base rows admitted) beside
  `fact_count` and `reducer_intent_count`. An operator can tell a union
  projection from a single-generation one, and see how many intents the delta
  emitted, from one log line.
- A refused scope-mismatch union keeps the existing signals: the
  `union_scope_mismatch` outcome on `eshu_dp_projector_delta_union_total`,
  the WARN load log, and the generic dead-letter with a message naming the
  union base and the outcome. No metric or label was added, so the telemetry
  coverage contract is unchanged.
