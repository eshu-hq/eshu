# Supply-Chain Impact Active-Evidence File-Fact Scan (#5237)

## What the issue reported

A bounded OSV ingestion for one exact Maven package/version made the
`supply_chain_impact` reducer spend about 116 seconds per active generation on
a retained 896-repository corpus. Roughly 99.6% of the handler time sat in
`load_active_evidence`, which returned **zero** rows.

The issue's stated diagnosis was a per-fact load — an N+1.

## What the code actually does

Not an N+1. `loadActiveSupplyChainImpactFactsUntilStable`
(`go/internal/reducer/supply_chain_impact_handler_helpers.go`) is a fixed-point
loop bounded at `maxSupplyChainImpactActiveEvidenceLoads = 8`, and when the
first round returns nothing the follow-up filter is empty, so it exits after
**one** call. The 116 seconds is a single SQL execution.

The second standing theory — that the ~30 OR'd JSONB predicates defeat index
selection and force a sequential scan — is also wrong. The plan is a nested
loop: the `ingestion_scopes`/`scope_generations` join supplies
`(scope_id, generation_id)` as an index condition, `fact_records` is reached
through `fact_records_scope_generation_idx`, and the disjunction is applied as
a per-row **filter** on the already-narrowed rows. There is no sequential scan
of `fact_records`.

What costs the time is *which rows reach that filter*:

1. `'file'` is in the query's scanned `fact_kind IN (...)` list. File facts
   outnumber every other listed kind by orders of magnitude — one per indexed
   source file per repository.
2. Roughly thirteen of the disjunction's branches are `fact.payload->>'key' =
   ANY($n::text[])` with **no fact-kind narrowing in front of them**, so every
   row that reaches the filter is tested against all of them.
3. Each `payload->>'key'` detoasts the whole payload again. A `file` payload
   carrying `parsed_file_data` is TOASTed, so one row can decompress its
   payload thirteen times.

The join has no scope restriction, so this runs across every active scope in
the database. Zero rows out is not zero work — it is the full per-scope index
scan with every file payload detoasted about thirteen times and nothing
surviving.

`'file'` is in that kind list only to feed the JS/TS reachability branch, which
cannot match unless `$10` (`FileRepositoryIDs`) is non-empty. `$10` is
populated only when an affected package is npm-ecosystem
(`npmAffectedPackages`, `go/internal/reducer/supply_chain_impact_active_filter.go`).
The reported intent was **Maven**, so `$10` was empty and the entire file-fact
scan could not produce a single row.

## The fix

One conjunct in `listActiveSupplyChainImpactFactsQuery`
(`go/internal/storage/postgres/facts_active_supply_chain_impact.go`):

```sql
AND (fact.fact_kind <> 'file' OR COALESCE(cardinality($10::text[]), 0) > 0)
```

placed before the disjunction, so a `file` row short-circuits on a string
comparison instead of thirteen detoasts.

### Why it is exact

With `$10` empty a `file` row cannot satisfy any branch:

- the suppression-scope branches are gated to `fact_kind =
  'vulnerability.suppression'`;
- the repository branch is gated to `fact_kind IN ('vulnerability.suppression',
  'reducer_package_consumption_correlation', ...)` — `'file'` is not in it;
- the file branch itself requires `$10` to be non-empty;
- the ungated identity predicates compare payload keys the file fact contract
  never carries. `codegraph/v1.File` is `repo_id`, `relative_path`,
  `parsed_file_data`, `graph_id`, `graph_kind`, `is_dependency`, `language` —
  and none of `package_id`, `purl`, `cve_id`, `advisory_id`, `subject_digest`,
  `digest`, `artifact_digest`, `referrer_digest`, `resolved_digest`, `cpe`,
  `criteria`, `document_id`, `image_ref`.

When `$10` is non-empty the conjunct is `true` and nothing changes at all.

There is no partial case to worry about. `$10` is bound once per statement, so
the gate is all-or-nothing for a call: a mixed npm+Maven intent has a non-empty
`$10` from its npm side and keeps every file row. Pagination is safe for the
same reason — `loadActiveSupplyChainImpactFactPagePair` re-binds the same
filter on every page and advances only the cursors, so the gate cannot flip
mid-stream.

The multi-round loader is safe by induction.
`loadActiveSupplyChainImpactFactsUntilStable` grows its filter from the rows
each round returns, so a later round could acquire an npm package and populate
`$10`. That does not change anything: with `$10` empty the file branch could
not return a file row before the gate either, so each round's rows — and
therefore the follow-up filter derived from them — are identical with and
without the conjunct. The fixed point is unchanged.

`NULL` behaves correctly too. If the driver binds `$10` as `NULL` rather than
`{}`, `cardinality` returns `NULL` and the `COALESCE` makes the gate `false`,
which matches the file branch's own `= ANY(NULL)` behaviour.

### Concurrency

Nothing here touches a claim, lease, lock, or queue row. This is a `SELECT` in
an existing read-only `REPEATABLE READ` transaction, and the conjunct only
removes rows from it. The effect on concurrency is one-way: the snapshot the
statement holds open drops from ~43 s to ~0.05 s on the proof corpus, which
shortens how long this reader pins the xmin horizon against autovacuum on
`fact_records`. There is no new contention to prove.

That last bullet is the one load-bearing assumption, so it is pinned rather
than assumed, by two tests that have to hold together:

- `TestSupplyChainImpactFileFactCarriesNoUngatedIdentityKey` reflects over
  `codegraph/v1.File` and fails if a contract change ever adds one of those
  keys to the file payload.
- `TestSupplyChainImpactUngatedIdentityKeysStayInLockstepWithQuery` keeps the
  key list the first test checks honest. It **derives** the ungated set by
  parsing the shipped query constant — walking outward from every
  `payload->>'key'` occurrence to see whether an enclosing `fact_kind`
  restriction keeps a `file` row away from it — and fails unless the derived
  set equals the declared list exactly.

The derivation matters. The obvious version of that second test — check that
every declared key still appears in the query — only catches a removal. It
stays green when someone ADDS an ungated predicate, which is the change that
actually breaks the gate: an ungated `payload->>'relative_path'` comparison
would match file facts, and the gate would start dropping rows the ungated
query returns. Four drift probes were run against the derived version: adding
an ungated predicate fails it, removing a declared one fails it, adding a key
inside a kind-gated branch correctly does not, and removing the `$10` guard
from the file branch fails it.

Limit worth stating: `file.v1.schema.json` sets `additionalProperties: true`,
so the guarantee is over the emitter and the typed contract, not over a
hypothetical third-party writer. Such a fact would in any case be ignored
downstream — `addSupplyChainImpactIndexEntry` has no `case factKindFile`, and
the three reachability builders key on `repo_id`.

## Measurements

Postgres 16 in a throwaway container, isolated schema, real table shapes plus
`fact_records_scope_generation_idx`. The statement measured is the shipped
constant `listActiveSupplyChainImpactFactPagesQuery` executed with the 21
placeholders bound exactly as `loadActiveSupplyChainImpactFactPagePair` binds
them — extracted from the Go constant, never retyped.

Corpus: 896 repository scopes plus one OSV scope; 448,000 `file` facts
(~8 KB payloads, 2.5 GB total payload, `fact_records` 2769 MB); Maven intent,
so `FileRepositoryIDs` is empty. The query returns the two seeded OSV rows and
nothing else — no `file` row matches either before or after, which is the whole
point.

Performance Evidence:

The headline before/after was taken by ONE test binary against ONE seeded
corpus, so the only difference between the rows is the conjunct itself: the
`before` row runs the shipped query with the gate string removed again by
substitution.

| Maven intent | EXPLAIN exec | Wall median (n=5) | Spread | Rows | Buffers |
| --- | ---: | ---: | --- | ---: | ---: |
| before (gate removed) | 43 375.690 ms | 42.951 s | 42.656–45.520 s | 2 | 28 252 679 |
| after (shipped) | 266.448 ms | 0.052 s | 0.051–0.144 s | 2 | 14 737 |

That is 826x on wall median and 163x on planner-measured execution, with an
identical result set — the same two OSV rows before and after.

The buffer counters are the clearest statement of the mechanism: 28.3 million
buffer accesses become 14.7 thousand, a 1 917x drop. The pre-fix figure is
roughly 220 GB of buffer traffic against a 2 769 MB table, about 80x the
table's own size. That is not scan volume; it is the same TOASTed payloads
decompressed once per identity predicate per row.

Per-loop shape before the fix: 897 loops (one per active scope), 63.7 ms each,
499 rows removed by filter per loop — about 127 µs per file row.

Controls over the same corpus, which is why the fix targets the rows rather
than the predicates:

| Control variant | Wall median (n=5) |
| --- | ---: |
| no gate, `cardinality()` guards on the empty arrays only | 16.962 s |
| gate plus those guards | 0.050 s |
| one identity predicate narrowed by fact kind (EXPLAIN only) | 49 801 ms vs 57 168 ms |

Two things fall out of that. Narrowing a single predicate moved almost nothing,
because the cost is spread evenly across all thirteen extractions — so a fix
has to remove the rows, not one branch. And once the gate is in, adding the
guards changes nothing measurable (0.052 s → 0.050 s, inside the noise): the
gate already removed every row they would have saved work on.

Scaling is linear in file rows, so the reported 116 s corresponds to roughly
1.1 million file facts, or about 1 200 indexed files per repository across 896
repositories. That is an ordinary corpus, which is why the symptom showed up on
a dogfood run rather than only at extreme scale.

Observability Evidence: no new instrumentation. The stage is already visible as
`sub_duration_load_active_evidence_seconds` with
`sub_signal_active_evidence_facts` (#4429,
`docs/internal/evidence/4429-supply-chain-impact-handler-attribution.md`), which
is exactly the pair that made this diagnosable — a large duration next to a zero
fact count.

## Limits of this evidence

- The corpus is synthetic. It reproduces the mechanism and its scaling law; it
  is not the retained 896-repository corpus the issue measured, which was not
  available here. The absolute 116 s is therefore explained and bracketed, not
  re-measured.
- File payloads were generated with `md5`-derived content at ~8 KB. Real
  `parsed_file_data` varies widely, and per-row cost scales with payload size,
  so a corpus of larger files would be slower and one of smaller files faster.
- Query-shape and single-statement wall time only. No end-to-end reducer drain
  or queue-wait measurement was taken, so this does not by itself establish the
  handler-level or dashboard-freshness improvement.
- The machine was loaded throughout (load average 13–16, a sibling worktree
  running a full-module coverage build). Absolute seconds are therefore
  inflated and the spread is wider than an idle machine would give. Both rows
  of the headline table were measured under the same contention, minutes apart,
  so the ratio is sound; the absolute numbers are an upper bound.
- The issue's second symptom — a repeated same-scope generation waiting 95.5 s
  behind the same conflict key — is queue behaviour, untouched by this change.

## Next long pole

An npm intent still scans every file row, because `$10` is non-empty and those
rows are genuinely wanted by the JS/TS reachability branch. It also still pays
the thirteen ungated extractions on each of them. The measured, provably exact
candidate for that case is the `cardinality()` guard row above (2x on this
corpus), which is the same idiom the query already applies to `$4` and to every
suppression-scope branch. It is not included here because it is a separate
change with its own exactness argument and its own before/after, and folding it
in would blur which conjunct produced which number.
