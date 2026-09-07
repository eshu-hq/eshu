# Agent instructions: codeshaping family

Read `doc.go` and `README.md` first.

## Invariants

- MUST NOT import root package `query`. Root's
  `family_code_shim_shaping.go` already imports this package for its
  compatibility aliases, so the reverse import cycles. If a change needs
  something only root exposes, either a leaf equivalent already exists
  (`querycontract`, `codemodel`) or it does not belong in this family;
  ask before adding one.
- Every helper MUST stay pure and in-process: no graph reads, no store
  reads, no network, no goroutines, no package-level mutable state. The
  schedule threads all of its state through the struct; the shapers work
  on caller-owned rows.
- The schedule's `index` field and the cursor type MUST stay unexported.
  Pages are dealt by `NextPage` and settled by `Record`; hand-built pages
  bypass the cursor bounds check.
- The budget applier keeps incoming row order and only trims. Rank
  (centrality) MUST run before trim in every caller -- the staying story
  handler already orders them that way.
- Do NOT reorder the clamp branches in `DeadCodeCandidateQueryLimit`
  (default fallback, overflow guard, then min/max). The order is the
  contract the dead-code contract tests pin.
- Files must stay under 500 lines. Split by concern rather than growing
  them.

## Exported symbols and why each is exported

Every export below names a staying root caller -- no speculative API. Do
not export a new symbol without adding its caller to this list.

- `DeadCodeCandidateSchedule`, `NewDeadCodeCandidateSchedule`,
  `NextPage`, `Record`, `CandidateScanTruncated` -- the staying
  dead-code scan, investigation, and cross-repo readers (via the root
  `deadCodeCandidateSchedule` alias and `newDeadCodeCandidateSchedule`
  forwarder).
- `DeadCodeCandidatePage` -- the page value the staying readers deal
  and settle opaquely through the schedule methods (field reads need no
  alias; nothing outside this package names the type).
- `DeadCodeCandidateQuery`, `DeadCodeCandidateContentStore` -- the
  staying scan's candidate-read assertion and the lane-B content
  reader's `DeadCodeCandidateRows` signature (via the root aliases),
  plus the staying dead-code doubles and tests that construct queries.
- `DeadCodeCandidateQueryLimit`, `DeadCodeCandidateScanLimit` -- the
  staying readers' page budgets and the staying contract/scan tests
  (via the root forwarders).
- `DeadCodeDefaultLimit`, `DeadCodeCandidateQueryMin`,
  `DeadCodeCandidateQueryMax` -- the staying orchestrator clamps, the
  lane-B content reader floor, and the staying contract tests (via the
  root const aliases). The multiplier and the scan-multiple stay
  leaf-private: only the limit shapers read them, so they are
  unexported and have no shim entry.
- `RelationshipStoryApplyTokenBudget` -- the staying story handler's
  budget pass (via the root forwarder).
- `RelationshipStoryRankByCentrality`, `RelationshipStoryRankBasis` --
  the staying story handler's rank pass and coverage stamp (via the
  root forwarder/alias). The guidance, neighbor, and centrality
  readers stay leaf-private: only the exported shapers call them, so
  they are unexported and have no shim entries.

## No-Cypher-text rule

No Cypher lives in this package. If a future change adds a builder
here, run `go test ./internal/queryplan/` and the in-package binding
tests; a `source_sha256` change means the edit altered declaration
bytes -- revert unless the rename was forced, and never let
`cypher_sha256` change. `file:` paths in
`go/internal/queryplan/testdata/{handler-hot-cypher,hot-cypher}.yaml`
and `query-source-coverage.yaml` MUST track the builder's real location.

## Verification (paste all)

```bash
cd go && go build ./internal/query/...
cd go && go vet ./internal/query/ ./internal/query/codeshaping/
cd go && go test ./internal/query/... -count=1
cd go && go test ./internal/queryplan/ -count=1
bash scripts/verify-dirgate.sh --digest internal/query
git diff --check
gofmt -l <touched files, repo-relative>
```
