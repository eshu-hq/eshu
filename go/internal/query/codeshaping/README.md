# Code shaping helpers (`go/internal/query/codeshaping`)

## Purpose

Owns the method-free shaping helpers of the code query family: the
dead-code candidate scan schedule and the relationship-story response
shapers. All three are pure in-process computations over already-fetched
rows -- no graph access, no store reads, no network.

- `code_dead_code_candidate_schedule.go` -- `DeadCodeCandidateSchedule`
  round-robins candidate labels under one shared row ceiling
  (`DeadCodeCandidateQueryLimit` fans the display limit out to the
  per-label page budget, `DeadCodeCandidateScanLimit` scales it to the
  multi-label ceiling). Sparse labels retire after their first short
  page; saturated labels keep taking turns, so later labels stay fair
  without multiplying downstream hydration and reachability work.
- `code_relationship_story_centrality.go` -- `RelationshipStoryRankByCentrality`
  stamps each row with its bounded centrality (the neighbor's degree
  within the resolved result set) and stably reorders rows by it, so the
  most-connected neighbors survive a small limit or token budget.
- `code_relationship_story_budget.go` -- `RelationshipStoryApplyTokenBudget`
  trims rows in place to the request's `token_budget`, returning an
  accounting map (`limit`, `estimated_tokens`, `truncated`, `dropped`,
  plus narrowing `guidance` when rows were cut).

## Ownership boundary

This package owns the schedule type and its methods, the scan-bound
constants (`DeadCodeDefaultLimit`, `DeadCodeCandidateQuery{Min,Max}`, plus
the leaf-private multiplier and scan-multiple only the limit shapers
read), the candidate page query value and its content-store port
(`DeadCodeCandidateQuery`, `DeadCodeCandidateContentStore`), the page
value dealt by the schedule (`DeadCodeCandidatePage`), and the story
shapers listed above.

Root package `query` keeps the dead-code handlers and readers
(`code_dead_code.go`, `code_dead_code_scan.go`,
`code_dead_code_investigation.go`, `code_dead_code_cross_repo.go`), the
relationship-story handler (`code_relationship_story.go`), the lane-B
content reader (`content_reader_dead_code_candidates.go`), and the
compatibility aliases (`family_code_shim_shaping.go`). Root owns the
router and always links into the production binary, so the staying
callers and their tests live there. The `relationshipStoryRequest` type
itself stays `codemodel`-owned (moved in L1); this package only takes it
as a parameter.

## Tenant isolation

These helpers never see grants: the caller's repository grant is bound
upstream, once, in the candidate read (`deadCodeCandidateRows`) and the
story resolver, before rows reach this package. The schedule pages and
the shapers reorder or trim only the rows they are given, so they cannot
widen a scan across tenants.

## Exported surface

Every export names a staying root caller; see `AGENTS.md` for the
per-symbol list. In brief: the schedule (`DeadCodeCandidateSchedule`
plus `NewDeadCodeCandidateSchedule`, `NextPage`, `Record`,
`CandidateScanTruncated`, and the `DeadCodeCandidatePage` value the
staying readers deal and settle opaquely); the candidate page contract
(`DeadCodeCandidateQuery`, `DeadCodeCandidateContentStore`,
`DeadCodeCandidateQueryLimit`, `DeadCodeCandidateScanLimit`, and the
three scan-bound constants staying code still names); and the story
shapers (`RelationshipStoryApplyTokenBudget`,
`RelationshipStoryRankByCentrality`, `RelationshipStoryRankBasis`). The
multiplier, the scan-multiple, and the guidance/neighbor/centrality
readers stay leaf-private -- only the exported shapers call them. See
`doc.go` for the godoc-rendered contract.

## Dependencies

Internal packages, both leaves that never import root package `query`:

- `internal/query/querycontract` -- row-value decoders (`StringVal`).
- `internal/query/codemodel` -- the `RelationshipStoryRequest` the
  budget applier reads (moved there in L1 with its methods).

No spans, no routes, no capabilities are registered here, so there is no
family-local tracing copy: this package adds no `handler_tracing.go`.

## Telemetry

None. These are pure functions over rows the caller already holds; they
emit no spans, metrics, or logs. Operators keep using the existing
dead-code and relationship-story truth envelopes, HTTP request metrics,
and graph query spans -- all unchanged by the move.

## Move evidence (#6060 lane A L3)

This package was created by moving three files out of root package
`query` (`git mv`, no logic changes) plus the five dead-code scan
constants out of lane-A-owned `code_dead_code.go`. The two assertions
below are structural rather than promissory -- each names what a reader
can check.

No-Regression Evidence: the move is a package relocation, not a
rewrite. `git diff -M --find-renames` pairs each file with its root
predecessor; the only statement-level changes are the `package` clause,
the export renames listed in `AGENTS.md`, the `querycontract`/`codemodel`
qualification of helpers root calls through identical functions, the
root `family_code_shim_shaping.go` aliases/forwarders, and the nine
schedule method-call qualifications in the three staying dead-code
readers. Baseline is the lane base with green suites; after the move,
`go test ./internal/query/... -count=1` passes with 0 failures and the
B-7 golden-corpus gate passes with the B-12 e2e snapshot byte-identical.
Backend/version is unchanged (same NornicDB-first contract over the same
driver path B-7 exercises live), input shape is the staying query suite
plus the golden corpus, and the terminal counts are the per-package ok
plus the corpus checks. The change is safe because behavior is preserved
by construction (a path-only move) and proven by the unchanged suites
and corpus above. A follow-up inline spot-check (the PR1/L2 pattern)
confirms the forwarder/method-export calls land on the moved code.

No-Observability-Change: no spans, metrics, structured logs, status
fields, or pprof surface were added, removed, or renamed; the move adds
no new query path, so dashboards and 3 AM triage read exactly as before.

## Gotchas / invariants

- Do not import root package `query`. Root's
  `family_code_shim_shaping.go` already imports this package, so the
  reverse import cycles.
- The schedule's `index` field and the cursor type stay unexported on
  purpose: callers deal pages and record counts through the methods, and
  the staying readers never construct pages by hand.
- The budget applier keeps incoming row order and only trims. Callers
  that want the most-connected rows to survive a small budget must rank
  (centrality) before trimming -- the staying story handler already
  does, in that order.
- The limit-clamp order matters: `displayLimit <= 0` falls back to
  `DeadCodeDefaultLimit` before the multiplier applies, and the
  `displayLimit+1` overflow guard runs before the min/max clamps. Do not
  reorder the branches in `DeadCodeCandidateQueryLimit`.
- Files must stay under 500 lines. Split by concern rather than growing
  them.

## Related docs

- [HTTP API Reference](../../../../docs/public/reference/http-api.md)
- [Cypher Performance](../../../../docs/public/reference/cypher-performance.md)
- [Architecture](../../../../docs/public/architecture.md)
