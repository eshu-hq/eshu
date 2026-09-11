# #6627 package-source projector package move

## Scope

Original TDD base: `722d636e9f805d76cf0fc6a24141d240867a048c`

Current review base: `2b01f131acfd5bf4f7642f0c9aa25b1182a280a9`

The implementation commit rebased cleanly after PRs #6639 and #6636 merged.
Before this evidence-binding correction, `git range-diff` marked the original
and rebased implementation patches as unchanged. PR #6639 had no changed-path
intersection. PR #6636 overlapped only the telemetry coverage document and a
supply-chain README; the rebased text preserves its reducer changes while
repointing the package-source path.

The branch later rebased onto `2b01f131a` after PRs #6610, #6612, and #6622
merged. `git range-diff 8ff548233..9bc4524b3 2b01f131a..447c05c6f` marked the
implementation patch unchanged; none of those merges touched a file this
branch changes.

Review follow-up: the root dispatcher imports the leaf under the descriptive
alias `packagesource` rather than `packages`, which read as the documentation
parent (whose `doc.go` declares `package packages`). The two README sentences
that quote the call were updated to match. This is an identifier change in
`scope_generation_intents.go` only; the call, its arguments, and its fan-out
position are unchanged.

This slice moves the package-source-correlation reducer-intent builder from
`internal/projector/packagesource` to `internal/projector/package/source`,
renames `correlation_intents.go` to `reducer_intent.go`, and shortens the
exported API from `BuildPackageSourceCorrelationReducerIntent` to
`BuildReducerIntent`. The production function body is otherwise unchanged.

The `internal/projector/package` package is documentation-only. The `source`
leaf still imports Eshu's internal facts, projector-intent, and reducer-domain
contracts, and root projector assembly remains its only production caller.
This is a clearer ownership seam for a future repository split, not a claim
that the package is independently extractable today.

## Rebased inventory and dependency edges

The affected leaf at the rebased base contained five files: its package-doc
trio, one non-test implementation file, and one test file. The destination
leaf contains those same five responsibilities. The new parent adds only its
required package-doc trio, including a declaration-only `doc.go`; root
projector non-test file count is unchanged.

Before and after the move, production imports are exactly `internal/facts`,
`internal/projector/intent`, and `internal/reducer`. The leaf test imports the
same three repository packages plus `reflect`, `testing`, and `time`. The sole
production reverse importer before and after path substitution is
`internal/projector/scope_generation_intents.go`; no test imports the leaf.
The destination adds no root-projector, sibling, storage, transport, or runtime
implementation dependency, and no old-path forwarding package remains.

Adding the `package` namespace makes dirgate's basename heuristic classify the
four root-owned `package_registry_*` implementation files as candidate members
of the new child. They remain at root because three build root
`CanonicalMaterialization` state and the fourth defines locking on root
`Runtime`. Each package clause carries the gate's supported, file-local
justification. No naming-exempt ledger or generated grandfather file changes.

## Preserved contracts

- A source hint wins over a package identity even when the identity appears
  earlier in input order.
- Within the winning kind, the earliest fact in original input order remains
  the anchor.
- A package identity remains the fallback trigger when no source hint exists;
  all other kinds enqueue nothing.
- Domain, `package_source_correlation:<scope>` entity key, both reason strings,
  fact ID, and two-tier source-system selection are unchanged.
- Payloads are not decoded. A malformed source hint still triggers reducer
  work because routing reads only `Envelope.FactKind`.
- Root fan-out remains 44 probes. Package source remains the first probe,
  immediately before AWS runtime drift.
- Package identity still triggers supply-chain impact independently.
- Root owns lookup construction, assembly, queueing, retries, and enqueue
  telemetry. The reducer owns classification, ownership and publication
  decisions, consumption admission, graph writes, and reducer telemetry.
- No public HTTP or MCP API, payload, fact, reducer-domain, queue, graph,
  telemetry, IFA fixture, or golden corpus contract changes; the internal Go
  path and API rename are described above.

## TDD evidence

Before the move, the focused leaf guard passed 1 of 1 tests and the exact root
fan-out parity and documented-probe-count guard passed 2 of 2 tests.

The destination test was then moved first, changed to `package source`, and
updated to call `BuildReducerIntent`. Running the focused guard for
`TestBuildReducerIntent` against `./internal/projector/package/source` failed
with four `undefined: BuildReducerIntent` compile errors and exit 1. That
compile-contract failure is the red phase for the path and API move.

## Final verification

All commands below completed with exit 0 after the final production edit. The
focused leaf and root guards were also rerun against the exact current review
base and the rebased branch:

- The focused `TestBuildReducerIntent` guard passed 1 of 1 tests.
- The exact root fan-out parity and documented-probe-count guard passed 2 of 2
  tests.
- At current review base `8ff548233`,
  `go test ./internal/projector/packagesource ./internal/projector -count=1`
  passed for both the original leaf and root dispatcher.
- `go test ./internal/projector/... -count=1` passed.
- `go test -race ./internal/projector/package/source ./internal/projector -count=1`
  passed.
- The hosted [offline replay job](https://github.com/eshu-hq/eshu/actions/runs/34544679430/job/103094619762)
  ran `bash scripts/verify-replay-tier.sh` against real NornicDB on production
  diff commit `0fb98eedd54f8f985eac9c165f78e62433cd6197`. The live replay tier passed
  in 272 seconds, and the follow-on SQL-table blast-radius proof passed every
  UNION branch in 37 seconds.
- The hosted [B-7 NornicDB corpus job](https://github.com/eshu-hq/eshu/actions/runs/34544679436/job/103094619643)
  ran `bash scripts/verify-golden-corpus-gate.sh` on the same production diff.
  It reported `561 pass, 0 required-fail, 1 advisory-warn` and
  `PASS: B-7 golden corpus gate green` in 213 seconds against a 1,800-second
  ceiling. The advisory was maintenance-drain timing at 32 seconds against a
  30-second advisory ceiling; it did not affect correctness or the blocking
  gate result. This evidence-only follow-up does not change production code,
  fixtures, cassettes, or the B-12 snapshot exercised by those jobs.
- A normalized diff of the executable function body between the base file and
  `package/source/reducer_intent.go`, substituting only the exported symbol,
  was empty.
- `go doc ./internal/projector/package` and
  `go doc ./internal/projector/package/source` passed.
- `go list` reported exactly the three intended direct production imports:
  `internal/facts`, `internal/projector/intent`, and `internal/reducer`.
- Diffs against the base for `testdata/cassettes` and
  `testdata/golden/e2e-20repo-snapshot.json` were empty.
- Both the dirgate mirror tests and the full-tree dirgate check passed with the
  four intentional root-ownership annotations; the naming-exempt ledger and
  generated grandfather map were unchanged.

No-Regression Evidence: the base and final production trees passed the same
focused leaf and root guards under Go 1.26.6 on Linux/amd64. Leaf input shapes
cover source-hint priority over an earlier identity, earliest-fact selection
within each kind, package-identity fallback, source-system fallback, and an
empty lookup. Root parity returns the complete multi-domain intent slice and
pins the package-source identity branch and fan-out position.

No-Observability-Change: the moved leaf emits no signal. Root intent enqueue
remains covered by `eshu_dp_reducer_intents_enqueued_total`; the reducer retains
`eshu_dp_package_source_correlations_total` and
`eshu_dp_package_consumption_repo_edges_total`. No metric, label, span, log
field, status surface, or telemetry ownership changes; only the existing
coverage row's source path was repointed.
