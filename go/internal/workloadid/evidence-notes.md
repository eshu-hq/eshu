# Evidence notes — `internal/workloadid`

## Step zero: extracting the identifier constructors (#5385)

This package is a pure extraction. Four call sites in `internal/reducer` that
built `workload:<name>` and `workload-instance:<name>:<env>` with `fmt.Sprintf`
now call `NewWorkloadID` and `NewWorkloadInstanceID`. The constructors reproduce
the current format deliberately; `repoID` is accepted and ignored, reserved for
the repository-scoped key the design proposes.

### Why this file exists at all

`scripts/verify-performance-evidence.sh` flags `workloadid.go` as hot-path. The
directory is **not** in `is_hot_path_by_location` — the gate fires on *content*,
and the only match in the whole file is the word `MERGE` on line 43, **inside a
doc comment** explaining why a blank segment must not yield a bare prefix. There
is no Cypher, worker, lease, batch, or concurrency construct in this package; it
imports `fmt` and `strings` and nothing else. The three touched
`internal/reducer` files are genuinely on the projection path, which is the real
reason this note is warranted.

No-Regression Evidence: the emitted identifiers are byte-identical, and that
was measured rather than reasoned. The constructors and the old inline code do
differ in general — the constructors `strings.TrimSpace` each segment and return
the empty id for a blank one, where `fmt.Sprintf` produced `workload:` or
`workload-instance:checkout:`. That divergence is unreachable at every call site:

| Site | Old | Equal? |
| --- | --- | --- |
| `projection.go:280` | `fmt.Sprintf("workload:%s", workloadName)` | Yes, all inputs. `candidateWorkloadName` returns a trimmed value and the caller skips `""` immediately above; `TrimSpace` is idempotent. |
| `projection.go:328` | `fmt.Sprintf("workload-instance:%s:%s", …)` | Yes on every production input. Every `environment` arrives through `environment.Canonical()` plus a non-empty gate. |
| `projection_helpers.go:119` | same | Yes, same funnel. |
| `dependency.go:76` | `fmt.Sprintf("workload:%s", depName)` | Diverges on a blank `depName`, but `BuildWorkloadDependencyRows` has no production caller — `rg` finds it only from tests, as with its consumer `MaterializeDependencies`. |

The environment funnel was proven empirically, not just read: hostile file facts
(`"  prod  "` namespaces, an `overlays/  prod  /` path, `values-STAGING.yaml`,
`dest_namespace: "   "` and `""`, `namespace: "  Production  "`) fed through both
production producers yielded only `prod` and `stage` — zero empty, zero
untrimmed. A differential harness then recomputed the pre-refactor `fmt.Sprintf`
from each emitted row's own fields and compared: 3 workload + 5 instance rows on
production-shaped environments, 3 + 5 on namespace-fallback environments, and 2
rows at `projection_helpers.go`, all byte-identical. Each harness fails on zero
rows so a vacuous pass cannot read as green — that guard fired and caught a bad
test input during the run.

Existing coverage stands behind the same claim: 116 literal `"workload:` and 85
literal `"workload-instance:` assertions across 34 reducer test files are green,
and the B-12 golden snapshot carries 41 references including asserted ids such
as `workload-instance:deployable-source:prod`.

`go test ./internal/reducer ./internal/workloadid -count=1` exits 0 (3786 pass,
0 fail, 20 skip in the reducer suite; 14 pass in this package's).
`go test ./internal/query ./internal/mcp -count=1` exits 0. `go vet`, `filecap`,
`dirgate`, package-docs, and telemetry checks all exit 0.

No-Observability-Change: nothing is added, removed, or renamed. Grepping
every added line for telemetry, instrument, metric, span, log, counter,
histogram, gauge, worker, lease, queue, and environment-variable patterns returns
zero hits. No telemetry-coverage rows are required: a row is demanded only for a
newly added `.go` file under a stage-owner directory, and `internal/workloadid`
is not one of those directories while the three reducer files are modifications
rather than additions.

### One correctness fix rode along

`projection_helpers.go:111` passed `repoID` — which ranges over the candidate's
*provisioning* repositories — where the sibling site at `projection.go:328`
passes `candidate.RepoID`. The row it builds records `RepoID: candidate.RepoID`,
and `projection.go:399` dedups both sites into one `seenInstances` map, so one
logical WorkloadInstance was being keyed from two different repositories. Today
both collapse to the same string because the constructor discards the argument.
Under the re-key they would produce two ids and duplicate the node — which is
precisely the failure the "accept `repoID` at every call site now" step exists to
prevent. Corrected to `candidate.RepoID` while the argument is still inert.

### What the type does not cover

The compiler enumerates every construction of the typed value, which covers the
reducer write path where projected graph truth is decided. It does not cover
read-side callers that still concatenate the prefix by hand; the package README
names the three that remain. The most consequential is
`internal/query/impact_change_surface_resolvers.go:107`, whose result is matched
against graph nodes — a re-key confined to this package would silently stop that
resolver matching anything. Converting them is the re-key's work, not step
zero's, but the claim is scoped here so it is not read as broader than it is.

### Re-verified on current main (`043143bde`, branch `codex/5385-cleanup`)

The four commits above were cherry-picked onto current main with no conflicts.
Every claim re-checked against the current tree:

- All four call sites route through the constructors
  (`projection.go:280,:328`, `projection_helpers.go:119`, `dependency.go:76`).
- `candidateWorkloadName` still trims both branches, so the constructor trim is
  idempotent at sites 1, 3, and 4.
- The environment funnel still normalizes at every producer, and every output
  is either blank or trimmed: `ExtractOverlayEnvironments` trims, drops
  blanks, and Canonicalizes (`projection.go:220-224`);
  `helmValuesFilenameEnvironment` admits only exact known tokens before
  Canonicalizing (`environment_signals.go:35-53`);
  `collectNamespaceEnvironmentsFromFileData` goes through `namespaceEnvironment`
  (Normalize plus a non-empty allowlist gate, `environment_signals.go:65-81`);
  the namespace fallback allowlists before Canonicalizing
  (`projection_helpers.go:197-207`). No untrimmed environment reaches either
  instance site on the production path; a blank one yields the empty id rather
  than a colliding bare-prefix node.
- `BuildWorkloadDependencyRows` still has no non-test caller, and the live
  DEPENDS_ON path (`BuildWorkloadDependencyIntentRowsFromEdges`) only carries
  already-built ids from projection rows or stored graph reads — not a
  construction site.
- New regression coverage: `internal/reducer/projection_workloadid_test.go`
  recomputes the constructors from each emitted row's own fields and pins the
  blank-environment guard. The blank test was proven non-vacuous by
  temporarily restoring the old inline `fmt.Sprintf` at `projection.go:328`:
  it fails with `InstanceID = "workload-instance:checkout:"`, then passes
  again after the restore.
- Current counts: 111 `"workload:` and 76 `"workload-instance:` literals
  across 31 reducer test files, all green unchanged — the byte-identity proof
  on this base.
- `go test ./internal/reducer/ ./internal/workloadid/ -count=1`: 2007 pass,
  0 fail, 2 skip (both pre-existing: a live-backend-gated Bolt retract test
  and a provenance-replay tombstone test).
  `go test ./internal/query/ ./internal/mcp/ -count=1`: green.
  `go vet`, `gofmt`, `verify-package-docs.sh`,
  `verify-performance-evidence.sh`, and `test-verify-golden-corpus-gate.sh`:
  all exit 0. (`gofumpt -l` flags 11 reducer files, all pre-existing drift on
  main in files this change does not touch.)
