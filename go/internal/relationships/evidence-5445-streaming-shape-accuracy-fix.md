# Evidence: streaming-shape typed-accessor migration is an accuracy fix, not a refactor (#5445 slice 1)

## Reclassification

The three commits on this branch (`33bc75ca1`, `bf968ff3e`, `1d8f3e125`,
`fa5d005fe`) were written and reviewed as an output-preserving refactor:
route eight `parsed_file_data` inner-key read sites in
`go/internal/relationships` (`terraform_evidence.go`,
`terragrunt_helper_evidence.go`, `structured_family_evidence.go`,
`argocd_generator_config.go`, `flux_evidence.go`) through new
`factschema.DecodeParsedFileData*` typed accessors instead of a raw
`parsedFileData[key].([]any)` map lookup, with equivalence proven by every
pre-existing hand-built-map test passing unchanged.

That equivalence proof was real but incomplete: it only exercised the shape
a Postgres JSONB round trip produces (`[]any`), which is the shape the
**deferred** `BackfillAllRelationshipEvidence` pass reads. It never exercised
the shape the **streaming per-commit** ingestion path actually carries.

## What was actually broken, and for how long

`go/internal/parser/shared.AppendBucket` (`go/internal/parser/shared/shared.go:340`)
builds every parser payload bucket, including all eight buckets this slice
touches, as `[]map[string]any`:

```go
func AppendBucket(payload map[string]any, key string, item map[string]any) {
	items, _ := payload[key].([]map[string]any)
	payload[key] = append(items, item)
}
```

`go/internal/collector/git_fact_builder.go:480`'s `fileFactEnvelope` embeds
that payload map **verbatim, with no JSON marshal/unmarshal round trip**,
into the `file` fact envelope's `parsed_file_data` field. The streaming
per-commit ingestion path
(`go/internal/storage/postgres/ingestion.go`, `upsertStreamingFacts`'s batch
callback, line ~281) passes that in-memory `facts.Envelope` batch straight to
`relationships.DiscoverEvidenceWithStats` — no Postgres round trip in
between either.

In Go, `[]map[string]any` does not satisfy a `.([]any)` type assertion. Every
one of the eight OLD read sites did exactly that assertion (for example,
pre-#5445 `terraform_evidence.go`: `if modules, ok :=
parsedFileData["terraform_modules"].([]any); ok { ... }`). Against the real
streaming shape, `ok` is always `false`, silently, with no error, log, or
panic. So on the streaming path, for every commit, for as long as these
eight extractors have existed, this branch's own regression proof (see
"Delta proof" below) confirms: terraform_modules, terragrunt_dependencies,
terragrunt_configs, helm_charts, helm_values, argocd_applications,
argocd_applicationsets, and flux_git_repositories produced **zero** evidence
facts on the streaming path.

The only place this evidence ever appeared was the deferred
`BackfillAllRelationshipEvidence` pass
(`go/internal/storage/postgres/ingestion_backfill.go`), which reloads facts
from Postgres via `loadDeferredAnchorScopedRelationshipFacts` — a real SQL
query whose JSONB decode always yields `[]any`, so the pre-#5445 `.([]any)`
read worked correctly there. Every pre-existing hand-built test for these
eight extractors uses `[]any` fixtures — the shape the streaming pipeline
never emits — which is why the refactor's original equivalence proof missed
this: it was equivalent on the shape it tested, and that shape was the wrong
one for the path this change actually touches.

`decodeParsedFileDataTolerantSlice`
(`sdk/go/factschema/decode_parsed_file_data_tolerant.go`) accepts BOTH
`[]map[string]any` and `[]any`. That is what makes this migration an
accuracy fix: it is the first change that makes the streaming path decode
its own native wire shape correctly, not merely a like-for-like read-site
swap.

## Delta proof

`sdk/go/factschema/decode_parsed_file_data_map_shape_test.go` (new) is the
accessor-level regression lock: eight subtests, one per migrated accessor,
each feeding a `[]map[string]any` input (never `[]any`) — the exact shape
`AppendBucket` produces. RED/GREEN proven directly on this branch: with the
`case []map[string]any:` branch temporarily removed from
`decodeParsedFileDataTolerantSlice`, all eight subtests failed with
`want slice of JSON objects, got []map[string]interface {}`; restored, all
eight pass.

`go/internal/relationships/structured_evidence_streaming_volume_bench_test.go`
(new) reproduces the "OLD CODE evidence count (real-parser payload,
streaming shape) = 0" claim directly on this branch (not only cited from the
original finding): it builds a 24-file representative platform/gitops repo
with the real HCL/YAML parser (`go/internal/parser`'s `DefaultEngine`, the
exact engine the ingester uses), asserts the pre-#5445
`.([]any)` assertion fails on every one of the 100 real-parser-populated
buckets across those files, then runs the actual NEW streaming call
(`DiscoverEvidenceWithStats`) against the identical batch and asserts it
resolves real evidence. See "Performance evidence" below for the measured
counts.

## Why the pre-typing tests didn't catch it

`decode_parsed_file_data_terraform_test.go` and
`decode_parsed_file_data_gitops_test.go` (pre-existing, from
`bf968ff3e`/this branch) use `[]any` fixtures exclusively across all eight
accessors — confirmed by `rg '\[\]any\{|\[\]map\[string\]any\{'` against
both files, which returns only `[]any{` matches. Only
`structured_evidence_real_parser_test.go` (also new on this branch) ran the
real parser and therefore exercised the true shape, transitively, which is
why it — not the dedicated accessor tests — is what caught this. The new
`decode_parsed_file_data_map_shape_test.go` closes that gap at the accessor
level directly, so a future refactor of
`decodeParsedFileDataTolerantSlice` cannot silently reintroduce the bug
without a fast, parser-independent test failing immediately.

## Performance evidence

Runtime-affecting-change proof, per CLAUDE.md: every relevant streaming
commit now does real catalog matching and `UpsertEvidenceFacts` work for
these 8 buckets that was previously always skipped.

**Streaming-time evidence volume** (measured,
`TestStreamingEvidenceVolume_RepresentativeRepo`, real HCL/YAML parser, a
24-file representative platform/gitops repo spanning all 8 migrated
buckets):

| | OLD (pre-#5445, streaming shape) | NEW |
| --- | ---: | ---: |
| Evidence facts | 0 | 28 |

**Reducer-side (in-memory) discovery cost** (measured,
`BenchmarkStreamingEvidenceDiscovery_RepresentativeRepo`, same 24-file batch,
Apple M1 Max, `go test -bench . -benchtime=2s`):

```
BenchmarkStreamingEvidenceDiscovery_RepresentativeRepo-10  11590  224443 ns/op  145112 B/op  1474 allocs/op
```

~224µs and ~145KB per streaming commit batch for this representative-repo
scale — negligible against the surrounding commit transaction's Postgres
work (fact upsert, catalog load, lock acquisition), which is already
milliseconds. This is not a meaningful streaming-throughput regression risk.

**Postgres write-side cost** (modeled, not live-Postgres wall-clock — see
"What was not measured" below): `RelationshipStore.UpsertEvidenceFacts`
(`go/internal/storage/postgres/relationship_store.go`) already batches
evidence rows into bounded multi-row `INSERT ... ON CONFLICT (evidence_id)
DO NOTHING` statements of up to `evidenceInsertBatchRows = 500` rows each —
pre-existing infrastructure from issue #3704, unmodified by this change.
`BenchmarkUpsertEvidenceFactsStreamingDelta_RepresentativeCommit`
(`go/internal/storage/postgres/relationship_evidence_upsert_streaming_delta_bench_test.go`,
new) models the marginal write cost using the same fixed-latency-fake
technique `BenchmarkDeferredBackfillSerial` already uses for the #3704
proof, at the measured 28-row NEW volume above, with a conservative 1ms
simulated per-statement round trip:

| | Before (0 rows) | After (28 rows) |
| --- | ---: | ---: |
| ExecContext calls (batches) | 0 | 1 |
| Modeled wall time | ~6ns (no-op) | ~1.2ms |

28 rows is well under the 500-row batch bound, so this is exactly one
additional `INSERT` statement per representative commit, not per row. A
commit touching a much larger IaC surface (thousands of Terraform/Helm/
ArgoCD/Flux files in one push) would still cost only
`ceil(evidence_count / 500)` additional statements.

**What was not measured**: a live-Postgres wall-clock number for
`UpsertEvidenceFacts` itself. `go/internal/relationships` has no dependency
on `go/internal/storage/postgres` (the service-boundary ownership table in
`docs/internal/agent-guide.md`), so the in-memory discovery proof above
cannot reach the write path; and this task's scope explicitly excludes
`make pre-pr`, push, and PR creation, so a live Compose Postgres run was not
launched. The modeled number above is a structural lower bound (batch count
× a conservative same-region latency estimate), not a substitute for a
live-Postgres confirmation; that confirmation is the natural pre-merge
follow-up.

## Backfill redundancy

Checked whether `BackfillAllRelationshipEvidence` now does redundant work
for these 8 families, since streaming is no longer the silent zero-producer
for them. It does, but this is not new or unique to this fix: `DiscoverEvidence`
(`go/internal/relationships/evidence.go`) already runs a corpus-wide,
independent rediscovery for EVERY evidence family on every deferred backfill
pass, regardless of whether streaming already discovered and wrote the same
evidence for a family that already worked correctly (GCP cloud relationship
evidence, content-regex Terraform/Kustomize evidence, etc.). That is the
by-design shape of the two-pass system: streaming is the fast incremental
path, backfill is the deferred corpus-wide sweep that also covers
cross-repo cases a single commit's batch view cannot see. `UpsertEvidenceFacts`
is `ON CONFLICT (evidence_id) DO NOTHING` specifically because this overlap
is expected and already occurs today for every other family.

This fix adds these 8 families to that pre-existing, already-accepted
overlap pattern; it does not introduce a new class of waste. The marginal
backfill cost is bounded to the same shape every other family already
pays: some extra CPU work re-discovering already-known evidence, and some
`INSERT` statements that resolve as no-ops within the same generation
(evidence_id is derived including `generationID`
(`go/internal/storage/postgres/relationship_evidence_batch.go`'s
`insertEvidenceFactBatch`), so a re-discovery under the SAME active
generation dedupes to a true no-op; a re-discovery under a NEWER active
generation correctly writes fresh rows for that generation's own evidence
coverage — that is backfill doing its actual job, not redundant work).
Backfill code is unmodified in this change, per this task's scope.

## Finding 4: whole-row drop is a deliberate, documented, currently-unreachable stricter contract

See `go/internal/relationships/structured_family_evidence.go`'s
`argoApplicationSourceRefs` doc comment for the full reasoning. Decision:
document the stricter contract, do not restore per-field tolerance. Summary:
`codegraphv1.ArgoCDApplication.SourceRepos` (and every other CSV-ish field
these accessors read) is enforced as a Go `string` at decode time
(`sdk/go/factschema/decode_map.go`'s `assignField`, `reflect.String` case,
pre-existing and unmodified). A field that failed that assertion would make
`decodeMapInto` error, and `decodeParsedFileDataTolerantSlice` skips the
WHOLE malformed row, not just the one bad field — stricter than the
pre-#5445 raw-map read, which passed the raw value straight to
`tupleCSVValues` (tolerant of `string`/`[]string`/`[]any` per field). This is
not special-cased back to per-field tolerance because (1) the sole real
producer, `go/internal/parser/yaml/argocd.go`'s `joinArgoSourceTupleValues`,
always returns a Go `string` for these fields — there is no live path that
emits an array today — and (2) the row-atomic-on-type-mismatch behavior is
identical for every named field on every one of the 8 migrated accessors,
not unique to `SourceRepos`, so special-casing one field would leave the
same landmine everywhere else. A regression test
(`TestDecodeParsedFileDataArgoCDApplications_WholeRowDroppedOnFieldTypeMismatch`,
`sdk/go/factschema/decode_parsed_file_data_gitops_test.go`) locks the current
behavior in.

## Operator signal for skipped rows (finding 2)

`decodeParsedFileDataTolerantSlice` now emits one `slog.Debug` record naming
the decoded element type, total element count, and skipped count whenever at
least one element is skipped, closing the "zero rows, evaluated" vs "zero
rows, quietly dropped" ambiguity a producer regression could otherwise hide.
The common nothing-skipped path stays silent. Regression-locked by
`TestDecodeParsedFileDataTolerantSlice_LogsSkippedElements` and
`TestDecodeParsedFileDataTolerantSlice_NoLogWhenNothingSkipped`
(`sdk/go/factschema/decode_parsed_file_data_tolerant_test.go`).

## Verification run

```
cd go && go test ./internal/relationships/... -count=1
cd go && go test ./internal/storage/postgres/... -count=1
cd go && go test ./internal/ifa/... -count=1
cd sdk/go/factschema && go test ./... -count=1
cd go && go test ./internal/reducer -run TestPayloadUsageManifest -count=1
cd go && go vet ./internal/relationships/... ./internal/storage/postgres/...
cd sdk/go/factschema && go vet ./...
cd go && golangci-lint run --new-from-rev=origin/main ./internal/relationships/... ./internal/storage/postgres/...
cd sdk/go/factschema && golangci-lint run ./...   # 1 pre-existing staticcheck finding in
                                                   # schema_bytes.go, untouched by this change
gofumpt -l <every touched/added file>              # clean
```

All green except the noted pre-existing `schema_bytes.go` staticcheck finding,
which this change does not touch.

## Golden-corpus gate proof (re-review finding F1)

This migration moves 8 evidence extractors from emitting nothing to emitting
real evidence on the always-on streaming path — deterministic fact emission
that, per this repo's proof-tier table, requires cassette/golden replay, not
just the in-package unit/benchmark proof above. Ran the B-7 golden-corpus
gate against this exact rebased head (`4e1c637c9`'s descendant, 9 commits
ahead / 0 behind `origin/main`):

```
COMPOSE_PROJECT_NAME=eshu5445rereview \
ESHU_POSTGRES_PORT=25433 \
NEO4J_BOLT_PORT=25688 \
NEO4J_HTTP_PORT=25475 \
GATE_API_PORT=25081 \
GATE_MCP_PORT=25092 \
GATE_COLLECTOR_SETTLE_SECONDS=45 \
bash scripts/verify-golden-corpus-gate.sh
```

Result: **`PASS: B-7 golden corpus gate green (elapsed 63s, budget ceiling
1800s)`**, summary `450 pass, 0 required-fail, 1 advisory-warn` (the one
advisory is `phase_collect: observed=46.0s, baseline=20.0s, ceiling=25.0s`,
a timing band, not a correctness assertion).

The six required correlations the reviewer named as exercising these exact
families all passed, including their evidence-kind predicates:

```
[PASS] rc-1:   (Repository)-[:CORRELATES_DEPLOYABLE_UNIT]->(Repository) count=2, want >= 1
[PASS] rc-19:  (Repository)-[:DEPLOYS_FROM]->(Repository) count=4, want >= 1
[PASS] rc-156: (Repository)-[:DEPLOYS_FROM]->(Repository) evidence_kinds⊇[ARGOCD_APPLICATION_SOURCE] count=2, want >= 1
[PASS] rc-155: (Repository)-[:USES_MODULE]->(Repository) evidence_kinds⊇[TERRAFORM_MODULE_SOURCE] count=1, want >= 1
[PASS] rc-29:  (Repository)-[:DEPLOYS_FROM]->(Repository) evidence_kinds⊇[KUSTOMIZE_RESOURCE_REFERENCE] count=1, want >= 1
[PASS] rc-34:  (Repository)-[:DEPLOYS_FROM]->(Repository) evidence_kinds⊇[HELM_CHART_REFERENCE] count=1, want >= 1
```

The snapshot (`testdata/golden/e2e-20repo-snapshot.json`) did not drift: no
count range, required correlation, or query shape needed adjustment to pass.
This confirms the reviewer's structural hypothesis (minimum_count tolerances,
generation-scoped evidenceID dedup between streaming and backfill, and
backfill already deriving this evidence today) rather than merely assuming
it — the gate replayed the real cassette + fixture corpus through the actual
streaming path this change modifies and the committed golden truth held.

Stack was isolated (unique compose project + host ports) and torn down
automatically by the gate script's own cleanup trap on exit; no containers,
volumes, or networks remained afterward. The `eshu5218postmerge1` project
belonging to a separate concurrent session was not referenced and was
confirmed still absent/untouched on this host both before and after the run.
