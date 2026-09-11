# #5992: codeowners_ownership_edges Ifá exhaustiveness fixtures

## Scope

Issue #5992 (child of the #5543 umbrella) builds the family-specific Ifá
conformance-platform artifacts for `materialized_edges:
codeowners_ownership_edges` and wires them into the two live proof gates.
The changed files fall into six groups.

A note on counting, because this document got it wrong three times: figures
here are quoted against the merge-base (`git merge-base origin/main HEAD`),
never against `origin/main..HEAD`. `origin/main` moves whenever a sibling lane
fetches, and a diff taken against the moved ref reads as a mass deletion. A
raw file count is deliberately absent — it was wrong at 21, at 22, and again
at 23, because each value was true when written and false one commit later.
The six groups below are the map; they do not go stale when a file is added
to one of them.

**Family fixtures and guard.** A compiled Odù
(`go/internal/ifa/codeowners_family_catalog.go`), its cassette projector
(`go/internal/ifa/codeowners_family_odu.go`), the family's vacuity guard
(`go/internal/ifa/materialized_edges_codeowners.go`), a checked-in cassette
(`testdata/cassettes/codeowners/ifa-codeowners-family.json`), a hand-derived
expected-edge-set fixture
(`go/internal/ifa/testdata/codeowners/ifa-codeowners-family-expected-edges.json`),
and their focused tests. The cassette projector decodes with
`DisallowUnknownFields` plus a trailing-`io.EOF` check, matching
`codeCallFamilyCassetteFile` and `documentationFamilyCassetteFile` field for
field: #6137 hardened those two and left this one on a permissive
`json.Unmarshal` against a narrow struct, so its own claim to project through
"the same strict envelope boundary" was false until this change.

**Two splices into shared Ifá files.** `go/internal/ifa/catalog_seed.go` gains
`codeownersFamilyOdu()` and `go/internal/ifa/materialized_edges.go` gains the
family's `Resolve` arm. These follow `rationale_edges` exactly: the family
becomes cataloged and resolvable while staying waived, because
`specs/ifa-materialized-edge-coverage.v1.yaml` is untouched and the new
`Resolve` arm is dormant without a coverage row. No coverage is claimed here.

**One operator-facing message change.** `go/cmd/ifa/assert_edges.go`'s
endpoint-defect message now names the `ref` fallback alongside `uid` and `id`.
See No-Observability-Change below.

**Trigger wiring.** `specs/ci-gates.v1.yaml`,
`.github/workflows/ifa-determinism-gate.yml`,
`scripts/lib/ifa_live_gate_selector_cases.sh`, and the regenerated
`docs/public/reference/ci-gates.md`.

**A new cross-family invariant.**
`TestEveryCoveredFamilyTriggersBothLiveGates`
(`go/internal/ifa/materialized_edges_lockstep_test.go`) requires
every family holding a coverage row to declare a trigger in both gate blocks.
It applies to all 14 families, not just this one, and it adds an obligation
`go/internal/ifa/AGENTS.md` did not previously record — that file gains the
matching bullet in this change. The check is keyed to coverage rows rather than
to families on purpose: requiring triggers of all 14 would land 9 red rows for
families that are honestly waived and not yet wired, and a check that ships red
gets switched off. It lands clean and stays prospective.

**Live-gate shell helpers.** `scripts/lib/ifa_codeowners_live.sh` and
`scripts/lib/ifa_fault_injection_codeowners_cells.sh` remain drafts: neither
live driver (`scripts/verify-ifa-determinism.sh`,
`scripts/verify-ifa-fault-injection.sh`) references the `codeowners_*`
cassette, expected-edge path, or cell functions, so no cell executes against a
live stack. The cells library is no longer unread, though — the new hermetic
mirror `scripts/lib/test-ifa-fault-injection-codeowners-cases.sh` sources it
and drives two of its functions under stubs, and that module is sourced and
run by `scripts/test-verify-ifa-fault-injection.sh`, which `make pre-pr`
executes. `ifa_codeowners_live.sh` is still sourced by nothing.

## No-Regression Evidence:

Three parts of this change touch code that runs outside the test tree. Each is
scoped and proven.

1. **`go/cmd/ifa/assert_edges.go`** — the endpoint-defect message string only.
   No control flow, no predicate, no exit status changes; the branch that
   emits it is entered on exactly the same condition as before.
   `TestEndpointDefectMessageNamesTheRefFallback` pins the new text and failed
   on the old text before the change.
2. **`go/internal/ifa/catalog_seed.go` and `materialized_edges.go`** — one
   entry and one switch arm. `TestCodeownersFamilyIsCatalogedAndResolvable`
   proves the compiled Odù matches the strict cassette projection and that the
   resolver returns the exact five-edge detail. The gate's own behavior is
   unchanged because the family has no coverage row: `RunMaterializedEdgeCoverage`
   still reports it waived, and `TestMaterializedEdgeCoverageLockstepAgainstRealSpecs`
   passes with the waiver in place.
3. **`scripts/test-verify-ifa-fault-injection.sh`** — sources one new case
   module and calls its runner. Both of that module's cases were proven red
   against a deliberately broken
   `scripts/lib/ifa_fault_injection_codeowners_cells.sh` (timeout flipped to
   fail-open; `ifa_det_untrack_bg_pid` call deleted) and green after restore.

The production reducer handler
(`go/internal/reducer/codeowners_ownership_materialization.go`), its pure
extraction seam (`ExtractCodeownersOwnershipEdgeRowsWithQuarantine`), and the
production Cypher writer
(`go/internal/storage/cypher/canonical_codeowners_edges.go`) are untouched by
this branch — they do not appear in `git diff --name-only <merge-base>..HEAD`
at all. This change calls the existing extractor from
new test code; it does not modify it, and it adds no graph write, query shape,
route, worker, queue, lease, batch, or runtime knob.

The trigger wiring changes which gates re-run, not what any gate asserts. It
adds 9 inputs to `ifa-determinism` (182 → 191 paths) and 11 to
`ifa-fault-injection` (209 → 220), and the workflow's single shared `paths:`
filter goes 219 → 230 gaining the union. One of those entries,
`go/internal/reducer/factschema_decode_*.go`, is a deliberate widening rather
than a codeowners path: it pulls every `factschema_decode_<family>.go` in the
reducer — around twenty files — into both expensive live gates, so any
family's decoder edit now re-runs both. That is the intended
trade — today no gate that materializes a family's edge re-runs when the
decoder feeding it is edited.

Verification, all run after the final edit:

```
go build ./...                                                          exit 0
go test ./internal/ifa/... ./cmd/ifa/... ./cmd/ci-gates/... -count=1     exit 0
go test ./internal/reducer/... ./internal/storage/cypher/... -count=1    exit 0
scripts/dev/precommit-go.sh lint                                        exit 0
scripts/dev/precommit-go.sh filecap                                     exit 0
scripts/dev/precommit-go.sh dirgate                                     exit 0
bash scripts/test-verify-ifa-determinism.sh                             exit 0
bash scripts/test-verify-ifa-fault-injection.sh                         exit 0
bash scripts/verify-ci-gates-registry.sh                                exit 0
bash scripts/test-generate-ci-gates-doc.sh                              exit 0
bash scripts/verify-package-docs.sh                                     exit 0
ESHU_PERFORMANCE_EVIDENCE_BASE=origin/main scripts/verify-performance-evidence.sh
                                                                        exit 0
mkdocs build --strict                                                   exit 0
git diff --check "$(git merge-base origin/main HEAD)"..HEAD             exit 0
```

Exit codes only. Per-run counts a gate happens to print (packages linted,
sub-cases passed) are deliberately not quoted: they change whenever a file is
added and say nothing the exit code does not.

The range on that last line is load-bearing, and an earlier revision of this
commit dropped it. Bare `git diff --check` compares the working tree against
the index, so on a clean tree it examines nothing and exits 0 no matter what
the branch contains — the row would still read as evidence while having become
a check that cannot fail. Applying this document's own counting rule means
repointing the ref at the merge-base, not deleting the range.

One limit of this table, stated because the rest of the document is about not
overclaiming: it is hand-maintained and binds to no commit. A `make pre-pr`
stamp records the SHA it passed for; these eleven lines record only that
someone ran them. They can be re-run at any SHA to check, and that is the only
thing keeping them honest.

An earlier revision of this branch carried three deliberately-red tests: two
proving that the shared `ifa.ExpectedEdge` triple could not tell two codeowners
rules apart, and one proving the family was not yet cataloged. #6137 landed the
relationship-identity properties on `ExpectedEdge`, so the first gap is closed
and this branch reads the fixture through the shared `ifa.LoadExpectedEdges`
loader and `ExpectedEdge.Key()` instead of a family-local struct, loader, and
key. Those two tests were replaced by positive proofs of the closed mechanism
(`TestExpectedEdgeKeyDistinguishesADroppedRuleFromAnUnrelatedDuplicate`,
`TestCodeownersOwnershipIdentityExcludesOrderIndex`), and the third is green
because of splice group 2 above.

The live lane runs on this diff — the trigger wiring selects it — and it
passed. It proves nothing about this family: no codeowners cell reaches either
driver, since `scripts/verify-ifa-determinism.sh` and
`scripts/verify-ifa-fault-injection.sh` reference neither
`ifa_codeowners_live.sh` nor the cells library, so the lane exercised the
pre-existing cells only. A green live lane on this PR is not family proof, and
the coverage rows and the waiver removal stay for the live-proof phase.

## No-Observability-Change:

This change adds no metric, span, log field, status field, queue table, worker,
lease, batch, or runtime knob, and no route or graph query shape.

It does change one operator-facing string. `eshu-ifa assert-edges`'s
endpoint-defect diagnostic previously read "carries neither uid nor id"; this
change made it read "carries neither uid, id, nor (for a CodeownerTeam
endpoint) ref". (#6228 later added a fourth, Environment-scoped `name` fallback
and widened the same string again — see
`docs/internal/evidence/6228-direct-family-coverage.md`. This paragraph records
what this change did, not the wording that is current today.)
`endpointID` has had three fallbacks since #6137 and the message named two, so
the operator of a `DECLARES_CODEOWNER` regression was sent looking for two
properties a `CodeownerTeam` node is never keyed by.
`codeowners_ownership_edges` is the first family that can reach that branch on
a real target. This is a diagnostic correction on an existing surface, not a
new surface: no field, format, or exit status changes, and nothing parses this
string.

Operators diagnosing the Ifá conformance platform continue to use the surfaces
documented in `go/internal/ifa/README.md`'s Telemetry section: `go test`, `ifa
coverage`'s JSON report and stdout summary, and the existing `ifa
assert-edges` / `ifa drive` CLI output.
