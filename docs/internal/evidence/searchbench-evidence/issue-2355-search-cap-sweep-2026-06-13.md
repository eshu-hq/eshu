# Issue #2355 Search Cap Sweep (2026-06-13)

## Decision

The current public Eshu corpus does not justify a fixed searchable-document
corpus cap below the full curated search-document set. The 500-document
placeholder cap missed every labeled handle in this suite. Full-corpus indexing
restored all expected handles while keeping the measured in-process BM25 p95
query latency at 24 us for the labeled exact-symbol suite.

This supports keeping the public semantic-search response contract at
`corpus_limit=0` for the persisted Postgres BM25 lane until a larger corpus or
new backend proves a different cap. It does not claim vector-search quality or
NornicDB search readiness.

## Corpus

- Repo commit: `96e50de76e8d6d22e47e3f33748f2b3d8d6baabe`.
- Go runtime: `go1.26.4 darwin/arm64`.
- Stores: local Docker Postgres 18 and NornicDB graph backend. NornicDB was used
  for bootstrap projection only; the benchmark did not query NornicDB search.
- Repository scope: `repository:r_413958de`.
- Raw rows: 228,754 `content_entities`, 7,796 `content_files`, 261,853
  `fact_records`.
- Curated search documents: 227,196.
- Projection exclusions: 9,354 sensitive rows, 0 excluded-source rows, 0
  missing-handle rows.
- Bootstrap projection evidence: bootstrap completed with 261,853 fact records,
  228,754 content entities, and 7,796 content files.

## Suite

Suite file:
[`issue-2355-content-handle-suite-v1.json`](../../public/reference/searchbench-evidence/issue-2355-content-handle-suite-v1.json).

The suite contains 20 repository-scoped keyword queries. Expected handles were
selected from public content-entity rows before running the benchmark and use
stable `content_entity:<content entity id>` graph handles. Query text uses exact
symbol names, so the result measures cap effects on handle recall and latency
over the shared `searchdocs` projection. It is not a human relevance judgment
for broad natural-language semantic search.

## Commands

Corpus shape:

```bash
cd go
ESHU_BENCH_DSN="postgres://eshu:<local-password>@localhost:25432/eshu" \
  go run ./cmd/search-bench \
    --repo repository:r_413958de \
    --max-docs 300000 \
    --queries 5 \
    --rounds 1 \
    --limit 20
```

Cap sweep:

```bash
cd go
ESHU_BENCH_DSN="postgres://eshu:<local-password>@localhost:25432/eshu" \
  go run ./cmd/search-bench \
    --repo repository:r_413958de \
    --max-docs 300000 \
    --suite ../docs/public/reference/searchbench-evidence/issue-2355-content-handle-suite-v1.json \
    --caps 500,5000,20000,50000,100000,all \
    --query-timeout 30s
```

## Results

| Cap | Indexed | Overflow | Build | p50 | p95 | Recall | Precision | nDCG | False Canonical |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 500 | 500 | 226,696 | 14 ms | 1 us | 9 us | 0.000 | 0.000 | 0.000 | 0 |
| 5,000 | 5,000 | 222,196 | 36 ms | 1 us | 9 us | 0.000 | 0.000 | 0.000 | 0 |
| 20,000 | 20,000 | 207,196 | 133 ms | 1 us | 17 us | 0.050 | 0.050 | 0.050 | 0 |
| 50,000 | 50,000 | 177,196 | 244 ms | 2 us | 16 us | 0.050 | 0.025 | 0.032 | 0 |
| 100,000 | 100,000 | 127,196 | 507 ms | 3 us | 26 us | 0.300 | 0.175 | 0.268 | 0 |
| 227,196 | 227,196 | 0 | 1.276 s | 8 us | 24 us | 1.000 | 0.227 | 0.757 | 0 |

## Interpretation

The deterministic corpus cap sorts documents by search-document id before
truncation. That makes low caps stable, but it also means they can exclude valid
repository handles that sort later. In this corpus, every fixed cap below the
full 227,196-document set lost labeled handles. The existing 500-document
placeholder had zero recall and is not an acceptable production cap.

Full-corpus build cost was 1.276 seconds in the benchmark harness. The #2343
runtime path uses reducer-maintained persisted postings rather than request-local
index rebuilds, so this build cost is evidence for cap sizing, not a request
latency claim. The measured per-query p95 in the full-corpus in-process BM25
sweep was 24 us, with zero false canonical claims.

## Observability And Follow-Up

No runtime code changed in this evidence slice. The existing semantic-search
response already exposes `indexed_document_count`, `corpus_limit`,
`corpus_may_be_truncated`, `truncated`, and false-canonical counts for operator
diagnosis. A future vector or NornicDB-search lane still needs a separate
measured run with its own backend evidence.
