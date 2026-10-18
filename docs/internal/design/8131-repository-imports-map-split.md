# #8131: Split `imports_map` Off the Repository Fact

Status: design packet (no code). Refs #8131, #8082.
Audience: maintainers and implementing agents.
Decision (arbiter ruling on #8131, 2026-10-18): split `imports_map` into a
separate fact kind in the same scope and generation, with dual-read for old
generations. A content-store reference and a payload-column split were
rejected (section 10). This packet is the prove-theory-first and census gate
that ruling requires before any producer change.

Pre-Edit Gate (for the implementation train, section 9):

1. Entry point: `streamFacts` (`go/internal/collector/repo/git/fact_builder.go`) sends
   `gitcontent.RepositoryFactEnvelope`
   (`RepositoryFactEnvelope` in `go/internal/collector/git/content/envelopes.go`) once per
   generation; the call-graph reducer entry is `ExtractRelationships` and
   `ExtractRows` in `go/internal/reducer/code/call/extract.go`, both calling
   `shared.CollectRepositoryImports`.
2. Phase ordering: collector emit -> fact admission/storage -> reducer
   factload (`go/internal/reducer/factload`) -> code-call extraction. The
   new kind must be loadable by factload before the producer stops writing
   the inline key.
3. Data dependencies: `CollectRepositoryImports` needs, per repository in
   the generation under reduction, the symbol -> file-paths map the parser
   prescan resolved (`snapshot.ImportsMap`,
   `RepositorySnapshot.HeadCommitSHA` in
   `go/internal/collector/repo/git/source_types.go`). Nothing else reads
   it (section 2).
4. Re-trigger: facts are generation-scoped; a new generation re-emits both
   kinds and the reducer re-runs per generation. No cross-generation join
   exists today and none is added.

## 1. Problem

Every `repository` fact carries the repository's whole `imports_map`
(`envelopes.go:172`, `payload["imports_map"] = importsMap`, written when
non-empty). Measured on the test environment (#8131, 2026-10-14): 27,984
repository facts, 4.4 GB of JSON, about 158 KB per fact on average, largest
5.76 MB of which `imports_map` is 5,758,575 bytes and every other key
together is under 4 KB. Only the call-graph reducer reads the map
(section 2), so every other reader of repository facts pays storage,
TOAST detoast, transfer, or Go decode for data it never uses. The #8082
catalog fix already proved the shape of the win for one reader; #8131
removes the cost at the source instead of projecting around it per reader.

## 2. Verified census of repository-fact payload readers

Method: `rg -n 'imports_map'` over the whole tree plus the 13 readers
#8131 listed, each code body read (2026-10-18, worktree at
`design-8131-imports-map-split`). "Pays" classifies how the reader meets
the map today: **reads** (consumes `imports_map`), **Go-decode** (the
envelope payload is materialized as `map[string]any` in Go, so the map is
decoded and held whether or not the handler touches it), **SQL-key**
(Postgres extracts named keys with `payload->>` / `jsonb_to_record`; the
row is detoasted server-side but the map never ships to Go), or
**kind-scan** (only `FactKind` is inspected; the payload cost was paid
upstream by the loader that built the envelope slice).

| # | Reader | Keys read | `imports_map`? | Pays |
|---|---|---|---|---|
| P | Producer `collector/git/content/envelopes.go:137-192` | writes all | writes it | — |
| 1 | `reducer/code/call/shared/imports.go:42` `CollectRepositoryImports` (callers `ExtractRelationships`/`ExtractRows` in `extract.go`, `python/metaclass.go:31`) | `repo_id`/`graph_id`, `imports_map` | **reads — the only consumer** | Go-decode (intended) |
| 2 | `storage/postgres/backfill/catalog.go:29,51-55` via `relationships.RepositoryCatalogPayloadKeys()` | `repo_id, graph_id, name, repo_name, repo_slug, remote_url` (`relationships/catalog.go:68-70`) | no | SQL-key since #8082 (`jsonb_to_record` projects six keys; comment at `catalog.go:20-24`) |
| 3 | `storage/postgres/repository_freshness_sql.go:36,45` | `repo_id, graph_id, name` | no | SQL-key |
| 4 | `storage/postgres/tfstate_backend_queries.go:15-31,180-198,202,275-298,328-335` | `repo_id, graph_id, name, repo_slug, local_path` | no | SQL-key |
| 5 | `storage/postgres/relationship_store.go:182-189` | `repo_id` | no | SQL-key |
| 6 | `query/repository_read_model_summary.go:118-119,144-153` | `repo_id, id, name` | no | SQL-key |
| 7 | `reducer/servicecatalog/service_catalog_correlation_index.go:329-337` | typed decode `DecodeCodegraphRepository`: `graph_id, repo_id, name, remote_url` | no | Go-decode |
| 8 | `reducer/securityalert/reconciliation_handler.go:380-386` | none (kind presence) | no | kind-scan |
| 9 | `reducer/workload/retract/retract.go:104-114` | `graph_id`, `delta_generation` | no | Go-decode |
| 10 | `reducer/containerimage/container_image_identity.go:348-351` | none here (kind in load list; identity comes from scope) | no | kind-scan (+ loader Go-decode) |
| 11 | `reducer/packages/source/matching.go:45-60` | `graph_id, repo_id, repository_id, name, remote_url` | no | Go-decode |
| 12 | `reducer/packages/correlation/source_handler.go:328-336,372` | none (kind presence + load list) | no | kind-scan (+ loader Go-decode) |
| 13 | `reducer/candidate_loader.go:40-44` | `graph_id, name` | no | Go-decode |
| 14 | `ifa/evidence.go:29-32` via `relationships.RepositoryCatalogEntry` | the six catalog keys (row 2) | no | Go-decode (fixture-scale) |
| 15 | `ifa/familyodu/rationale_family_odu.go:201-213` | writes fixture `imports_map` onto its typed repository fact | writes (fixture) | — |

Corroboration: a tree-wide `rg 'imports_map'` outside tests, docs
diagrams (stale Python-era `docs/diagrams/*.mmd`), and the rows above
returns only the snapshot type (`source_types.go:97`, collector-internal,
also feeds the freshness fingerprint at `snapshot_freshness.go:177-184`)
and the schema note in `sdk/go/factschema/codegraph/v1/repository.go:83-92`.
Test-only emitters of the inline shape: `accuracygate`
(`resolver_coverage_*_test.go`, `measure_resolvers_test.go:263`),
`resolutionparity` (`golden_call_graph_test.go:196`,
`resolution_tiers_test.go:199`), `reducer/code/call/*_test.go` (many), and
`collector/repo/git/source_test.go:181-186`. The two rationale cassettes
(`testdata/cassettes/rationale/ifa-rationale-family.json:27` and
`testdata/cassettes/rationale/ifa-rationale-family-delta.json:27`)
embed it.

Non-Go consumers: **none found in this repository** — no hit under
`scripts/`, `deploy/`, charts, or SQL migrations. Dashboards or ad-hoc SQL
outside the repo are **NOT_CHECKED**; the implementation train keeps
dual-read for old generations, so an out-of-repo reader of stored facts
keeps working on historical rows but must be notified before step 5
(section 9) stops the inline write.

Census conclusion: exactly one production reader consumes `imports_map`.
Rows 7, 9, 11, 13 (and the factload materialization behind rows 8, 10, 12)
pay full Go decode of an average ~158 KB payload to read two to five short
strings. Rows 2-6 already avoid shipping the map to Go but still detoast
multi-MB rows server-side per key extraction.

## 3. Prove-theory-first measurement

Theory: removing `imports_map` from the repository payload removes ~99% of
per-fact bytes and the dominant share of decode cost for every Go-decode
reader, with no behavior change for the one real consumer once it reads
the same map from a sibling fact.

Shim: the existing committed benchmark
`BenchmarkLoadRepositoryCatalogDecode`
(`go/internal/storage/postgres/backfill/catalog_bench_test.go`), which
runs the real `LoadRepositoryCatalog` loop over 28,000 synthetic
repository payloads modeled on the measured QA distribution (six catalog
keys plus an `imports_map` of short entries; ~4% over 1 MB; pool built at
`catalog_bench_test.go:24-101`). `whole_payload` is today's shape;
`projected` is the same rows with only the six identity keys — an upper
bound proxy for the post-split repository payload (the real post-split
payload also keeps `git_refs`/delta keys; per #8131 all non-map keys
together are under 4 KB even on the largest fact).

Command (2026-10-18, this worktree, arm64):

```text
cd go && go test ./internal/storage/postgres/backfill/ -run '^$' \
  -bench BenchmarkLoadRepositoryCatalogDecode -benchtime 3x -benchmem -count 1

BenchmarkLoadRepositoryCatalogDecode/whole_payload-16   3  44728435319 ns/op  4925459616 payload_bytes/op  21690845634 B/op  576230921 allocs/op
BenchmarkLoadRepositoryCatalogDecode/projected-16       3    198104206 ns/op     4965912 payload_bytes/op     36134229 B/op    1120052 allocs/op
PASS (exit 0)
```

| Per 28,000-fact pass | Today (whole) | Keys only | Ratio |
|---|---|---|---|
| Wall time | 44.73 s | 0.198 s | 226x |
| Payload bytes shipped/decoded | 4,925,459,616 | 4,965,912 | 992x |
| Allocations | 576,230,921 | 1,120,052 | 514x |
| Derived per fact | ~176 KB, ~1.60 ms decode | ~177 B, ~7 µs | — |

Limits, stated plainly: the corpus is synthetic (shaped from the #8082 QA
measurements, not captured prod payloads) and excludes the database
(detoast + transfer would widen, not narrow, the gap). It proves the
decode-cost theory and the byte reduction; it does not prove end-to-end
pass time on the test environment. Before step 5 of the train, confirm
the live distribution with (read replica, bounded):

```sql
SELECT count(*) AS facts,
       sum(pg_column_size(payload)) AS payload_bytes,
       sum(pg_column_size(payload - 'imports_map')) AS without_map_bytes,
       percentile_disc(0.5) WITHIN GROUP (ORDER BY pg_column_size(payload)) AS p50,
       percentile_disc(0.99) WITHIN GROUP (ORDER BY pg_column_size(payload)) AS p99,
       max(pg_column_size(payload)) AS max_bytes
FROM fact_records WHERE fact_kind = 'repository';
```

Acceptance threshold for proceeding past step 4: `without_map_bytes` at
most 5% of `payload_bytes` on the live corpus (the synthetic shim says
0.1%; #8131's largest-fact key census says under 0.1% for that row).

## 4. Contract classification (Contract System v1)

Typed-struct status today: the `repository` kind **has** a typed struct —
`codegraphv1.Repository`
(`sdk/go/factschema/codegraph/v1/repository.go:28`), decode seam
`factschema.DecodeCodegraphRepository` (`decode_codegraph.go:43`), kind
constant `FactKindCodegraphRepository = "repository"`
(`fact_kinds.go:461`), JSON Schema
`sdk/go/factschema/schema/repository.v1.schema.json`, registry
declaration in the code family (`CodeFamilyDeclaration` in `go/internal/facts/legacy_git_kinds.go`).
But `imports_map` is deliberately **not** a typed field: it passes through
the open top-level object (`additionalProperties: true`) because its
`map[string][]string` wire shape renders as array-valued
`additionalProperties`, which the collector conformance validator's
supported subset rejects (`repository.go:83-92`).

Classification of this change:

- Removing `imports_map` from the `repository` payload is a **major**
  payload change by policy (field removal with a live consumer), even
  though `factschema-diff` cannot see it — the key was never in the JSON
  Schema. The packet therefore treats the census (section 2), not the
  schema diff, as the compatibility evidence, and requires the conversion
  shim (dual-read, section 5) in the same train. The schema file gains
  only a doc note; no major version bump of `repository` v1 is needed
  because no declared field changes. This is the one place the train must
  not lean on the gate: `factschema-diff` passing proves nothing here.
- The new kind is **additive** (minor for the contracts module as a
  whole): new typed struct, new schema file, new registry kind.
- Shim location: per `eshu-contract-rigor`, version handling lives in the
  contracts decode seam (`sdk/go/factschema`), never as
  `schema_version` branching in a reducer handler. Reconciliation with
  the arbiter's "dual-read in the call-graph reducer" wording: the
  *decision* of which source supplied a generation's map is
  generation-shape detection, and it belongs to a seam function in
  `sdk/go/factschema` (working name
  `DecodeCodegraphRepositoryImports`) that accepts the generation's
  envelopes and returns the latest typed view from either the new kind
  or the legacy inline key. `CollectRepositoryImports` keeps its
  signature and calls the seam; no handler branches on kind or version.
  The seam is also where the legacy read is deleted at the end of the
  compat window — one deletion, not a handler edit.
- New-kind wire shape: do **not** repeat the untyped-map mistake. Model
  the payload as a typed entries list so the conformance subset accepts
  it and ordering is deterministic:

  ```text
  RepositoryImports v1 payload:
    repo_id  string             (required — join identity, same rule as Repository.RepoID)
    entries  []ImportEntry      (required, non-empty — an empty map emits no fact, section 5)
  ImportEntry:
    symbol   string             (required)
    paths    []string           (required, sorted — finalizeDerivedPreScanImportsMap order)
  ```

  Entries sorted by `symbol`; `paths` keep today's sorted order
  (`snapshot_prescan_dedup.go:74-75`). The seam converts entries to the
  `map[string][]string` the resolvers consume. Alternative — grow the
  conformance subset to accept array-valued `additionalProperties` and
  type the map directly — is rejected for this train: it expands a
  validator contract for one kind and keeps nondeterministic map
  rendering in golden fixtures.
- Missing required field on the new kind (`repo_id`, or an entry missing
  `symbol`/`paths`) is a classified `input_invalid` dead letter
  (`TriageClassInputInvalid`), never a silent empty map — same rule the
  `Repository` struct documents for `RepoID`. An **absent** imports fact
  is different from a malformed one: absence is the normal old-generation
  and no-imports case (section 5) and must not dead-letter.

## 5. Design

New fact kind: **`repository_imports`** in the existing `code` family
(bare-name convention of its siblings `repository` and `file`; constant
`FactKindCodegraphRepositoryImports`). One fact per repository per
generation, emitted by the same `fact_builder.go` send sequence that
emits the `repository` fact, with:

- same `scope_id`, `generation_id`, `observed_at`, and source system as
  the sibling `repository` fact;
- stable key `repository_imports:<repoID>` (mirrors `repository:<repoID>`
  at `envelopes.go:192`), so re-emission of a generation is idempotent;
- typed payload per section 4 (`repo_id` + sorted `entries`), built via
  `factschema.EncodeCodegraphRepositoryImports` — never a hand-built
  `map[string]any` (the producer currently hand-builds the repository
  payload; the new kind starts typed).

Emission rule: emit only when the map is non-empty, exactly today's
`len(importsMap) > 0` condition. A repository with no resolved imports
has no imports fact, same as it today has no inline key. Delta
generations carry the full merged map today (unchanged files' entries
survive — `snapshot_delta_test.go:95-100`) and keep doing so; a delta
encoding of the map is out of scope for v1 of the kind.

Atomicity: both facts are sent in the same generation build
(`fact_builder.go:201` sequence) and become visible under the same
generation activation. There is no cross-fact transaction beyond what
generation commit already gives sibling `file` facts, and none is
needed: the failure mode "repository fact present, imports fact for the
same generation lost" degrades to today's no-map behavior (weakened
call resolution for that generation, visible in tier metrics), never to
wrong edges — resolvers treat a missing map as no import evidence, the
same as `imports, ok := …; if !ok { continue }` today
(`imports.go:57-60`). The train must still prove same-generation
co-emission with a collector test, because silent partial emission
would be an accuracy regression that looks like a parser change.

Dual-read rules (implemented once, in the contracts seam — section 4),
evaluated per repository within the generation under reduction:

1. If a `repository_imports` fact is present for the repo, it is
   authoritative; any inline `imports_map` on the sibling `repository`
   fact is ignored (both present only during the dual-write step).
2. Else if the `repository` fact carries an inline `imports_map`, use
   it (every pre-split generation; no backfill — stored generations are
   immutable and are read as written for as long as they are retained).
3. Else the repo has no import map (today's empty case).

Both-present disagreement is impossible by construction during
dual-write (same `snapshot.ImportsMap` value rendered twice), and the
seam's precedence makes the new kind win if it ever happened; the seam
records which source fired (section 8) so a disagreement rate is
observable rather than silent.

Call-graph path: `factload` kind lists for the code-call domains gain
`repository_imports` so the envelopes reach `extract.go`;
`CollectRepositoryImports` keeps its signature and return type, sourcing
through the seam. No resolver changes: everything downstream already
consumes `map[string]map[string][]string`. The import-cache and
normalization helpers in `shared/imports.go:130-273` are untouched.

Backfill stance: **none**. Old generations keep the inline key until
generation retention drops them; rewriting stored payloads would
invalidate fact identity and buy nothing, since dual-read covers them.
Storage reclaim therefore arrives at the pace of generation churn and
retention, not at cutover — the PR that stops the inline write must say
so, with the section-3 query re-run as the before/after evidence on new
generations only (`generation_id` filtered). Retention policy itself is
**NOT_CHECKED** in this packet.

## 6. Ownership (joint contract change)

| Owner | Change |
|---|---|
| Contracts (`sdk/go/factschema`) | `RepositoryImports`/`ImportEntry` typed structs, encode/decode seam incl. legacy dual-read, kind constant, generated JSON Schema, `repository.go` note updated |
| Collector (`collector/git/content`, `collector/repo/git`) | emit the new kind from `snapshot.ImportsMap`; stop inline write at cutover; freshness fingerprint unchanged (it reads the snapshot struct, not the fact) |
| Reducer (`reducer/code/call`, `reducer/factload`) | factload kind lists; `CollectRepositoryImports` via seam; no handler version branching |
| Registry (`go/internal/facts`) | code-family declaration gains the kind + producer entry; regenerate registry YAML/shards/`fact_kind_registry.generated.go` via the gate tooling, never by hand |
| Ifá (`go/internal/ifa`) | rationale Odù builds the new kind instead of hand-appending `imports_map` (`rationale_family_odu.go:206`); `materializededges` open-object guard test updated (`rationale_family_live_fixture_test.go:239-244`); cassette regeneration |
| Storage (`storage/postgres`) | no change required: generic fact storage, and rows 2-6 already project keys. The win lands automatically as new generations arrive. Confirm no kind allowlist blocks admission of the new kind (code family is `AdmissionExempt: true`, `legacy_git_kinds.go:18` — verify the new kind inherits this in step 1) |

## 7. Fixture, Odù, and golden-corpus impact

- Rationale Odù + cassettes: `testdata/cassettes/rationale/ifa-rationale-family.json`
  and `testdata/cassettes/rationale/ifa-rationale-family-delta.json`
  embed the inline key; both regenerate when the Odù moves to the new
  kind. Because an Odù overlaps cassettes, `eshu-golden-corpus-rigor`
  applies to that step alongside `eshu-contract-rigor`: regenerate via
  the sanctioned writer, never hand-edit cassette JSON.
- B-12 snapshot (`testdata/golden/e2e-20repo-snapshot.json`): whether it
  embeds repository payloads (vs derived graph truth) is **NOT_CHECKED**
  in this packet; step 3's acceptance includes answering it. If it only
  pins derived truth, the split must leave it byte-identical — that is
  the strongest available proof the split changed no graph output.
- Test emitters (accuracygate, resolutionparity, `reducer/code/call`
  suites, `collector/repo/git/source_test.go`): migrate through one
  shared test helper that builds the new-kind envelope from a
  `map[string][]string`, rather than editing dozens of payload literals
  ad hoc. A deliberate subset keeps emitting the inline shape to pin
  dual-read rule 2 until the compat window closes.
- Fixture packs (`sdk/go/factschema/fixturepack`): if the codegraph
  family has a pinned fixture pack, it is re-cut in lockstep with the
  contracts change (step 1 acceptance checks `fixturepack_drift_test`).

## 8. Telemetry

Operator question at 3 AM: "is call resolution degrading because import
maps stopped arriving, and are we still reading the legacy inline key?"
Required signals (named here, implemented in steps 2-4; exact
`eshu_dp_*` names follow the telemetry standards doc at implementation):

- Per code-call reduction, a counter/attribute on the existing
  extraction span recording import-map source per repo:
  `new_kind | inline_fallback | absent`. A rising `absent` rate after
  cutover, or `inline_fallback` not trending to zero as generations
  churn, is the failure signature.
- Fact payload bytes by kind already derivable from storage; step 5's
  acceptance re-runs the section-3 SQL instead of adding a metric.
- No new signal for rows 2-13: their improvement is absence of cost,
  proven by the benchmark and the SQL before/after, not by a counter.

No-observability-change is **not** claimed: the source-attribution
signal is a required part of steps 2-4, because without it a broken
producer (repository facts flowing, imports facts silently missing)
is indistinguishable from a parser-quality regression.

## 9. PR train

Each step is independently mergeable and leaves `main` correct. Gates
named per step are in addition to the repo floor the coordinator runs.

1. **Contracts only.** Typed structs, seam (new-kind decode; legacy path
   not yet wired to any handler), kind constant, generated schema,
   code-family registry declaration + regeneration, conformance fixture.
   Acceptance: `factschema-diff` PASS (additive schema only),
   `fact-kind-registry` PASS, `contract-source-of-truth` PASS,
   conformance payload validation accepts the entries shape and rejects
   a missing-`repo_id` fixture by name; admission of the new kind
   verified against the code family's exemption.
2. **Dual-read.** Seam gains the legacy inline fallback + source
   attribution; `CollectRepositoryImports` sources through it; factload
   kind lists gain the kind (harmless before any producer emits it).
   Acceptance: `payload-usage-manifest` PASS (the seam's field reads are
   schema-declared); existing call-graph suites green unchanged —
   behavior proof that dual-read is transparent; new seam unit tests pin
   rules 1-3 and the malformed-fact dead-letter classification.
3. **Dual-write.** Collector emits `repository_imports` alongside the
   inline key from the same `snapshot.ImportsMap`; test-helper migration
   begins. Acceptance: collector test proves same-generation co-emission
   and byte-equal map content both ways; B-12 question from section 7
   answered and, if derived-truth-only, snapshot unchanged;
   `source_test.go` updated to assert the new fact.
4. **Ifá + fixtures.** Rationale Odù emits the new kind; cassettes
   regenerated by the sanctioned writer; `materializededges` guard test
   updated; remaining test emitters migrated (inline-shape subset kept
   deliberately). Acceptance: golden/Ifá gates green with regenerated
   cassettes; `eshu-golden-corpus-rigor` checklist recorded in the PR.
5. **Stop the inline write.** Producer drops `payload["imports_map"]`;
   live-corpus threshold from section 3 verified first on a read
   replica; out-of-repo reader notification (section 2) confirmed done
   or explicitly waived in the PR. Acceptance: before/after on the same
   corpus — per-fact repository payload bytes and one catalog pass —
   using the section-3 SQL generation-filtered, plus
   `BenchmarkLoadRepositoryCatalogDecode` re-run; resolution-tier
   goldens (`resolutionparity`) unchanged.
6. **Compat-window close (later, separate issue).** Delete the seam's
   legacy inline read once retained generations predate step 5 no
   longer exist or the owner accepts re-collection. Acceptance:
   `inline_fallback` telemetry at zero over an agreed window before
   deletion. Until then the legacy read stays — removing it early makes
   every retained old generation silently lose import resolution.

Rollback at any step: revert that step's PR. Steps 1-4 are additive;
step 5's revert restores the inline write and dual-read keeps both
shapes correct in either direction. No step requires a data migration.

## 10. Rejected alternatives

- **Content-store reference** (store the map as content, fact carries a
  ref): rejected by the arbiter ruling. It adds a second fetch and a
  lifetime/GC question (content retention vs generation retention) to
  the reducer's hot load path, and splits one generation's truth across
  two stores with no shared activation boundary. The sibling-fact shape
  gets the same byte win inside the existing generation model.
- **Payload-column split** (dedicated `imports_map` column on
  `fact_records`): rejected by the arbiter ruling. It special-cases one
  kind in the generic fact table and every generic loader, and the win
  still depends on every reader remembering not to select the column.
- **Project keys in SQL everywhere** (generalize the #8082 fix): leaves
  4.4 GB stored and detoasted per new generation, and does nothing for
  Go-decode readers that load whole envelopes through factload
  (rows 7-13). Projection remains correct for the catalog reader; it is
  not a substitute for not storing the map on the wrong fact.
- **Emit the map only on the latest generation / deduplicate across
  generations**: breaks generation-scoped truth — reducing an older
  generation (replay, repair) would read a different generation's map.
  Content-addressed dedup inside the new kind's storage is a possible
  later optimization; v1 keeps one fact per generation, matching how
  `file` facts already behave.
- **Grow the conformance subset and type the map in place on
  `repository`**: rejected in section 4 — validator-contract expansion
  for one kind, and it keeps the bytes on the repository fact, which is
  the problem.

## 11. Risks and NOT_CHECKED

- Out-of-repo readers of the stored inline key: NOT_CHECKED (section 2);
  gated at step 5.
- Generation retention pace (when the 4.4 GB actually leaves the table):
  NOT_CHECKED; step 5 reports new-generation bytes only.
- Write-side cost change at commit time (one more fact row per repo per
  generation vs a smaller repository row): not measured in this packet.
  The row count grows by one small-envelope-plus-map fact; total bytes
  written are unchanged until step 5, then strictly smaller. If fact
  admission has a per-fact fixed cost that matters at 28k-generation
  scale, step 3's PR must measure it — do not assume it is free.
- Whether any deployment profile disables the code-call domains (in
  which case the new kind is stored but never loaded): NOT_CHECKED;
  emission stays unconditional in v1 for symmetry with today.
- The stale Python-era diagrams under `docs/diagrams/` mention
  `imports_map`; they do not describe the current Go pipeline and are
  out of scope here.
