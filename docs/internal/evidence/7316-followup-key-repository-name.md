# #7316: follow-up entity keys use the repository fact name

## Question

The git collector built every reducer follow-up key as
`<prefix>:` + `filepath.Base(repoPath)`. The repository fact publishes `name`,
which is `SelectedRepository.DisplayName` when set and the same basename
otherwise. In dependency mode `ESHU_BOOTSTRAP_PACKAGE_NAME` sets `DisplayName`
for every repository in the run, so the two differ. The reducer selects
candidates by comparing the key against the repository fact name, so the key
matched nothing.

## Verdict

Defect, fixed in the collector only. Every follow-up key is now
`<prefix>:<repo.Name>` through one helper, `followupEntityKey` in
`internal/collector/repo/git/followup_facts.go`, fed from `streamFacts`, which
also publishes the repository fact from the same `repo`. No reducer, projector
or query code changed.

Scope is all 13 follow-up envelopes, not only workload_materialization and
deployable_unit_correlation. Five keys are consumed by value:

- `workload_materialization` and `deployable_unit_correlation` select the
  repository's candidate with the key (`filterDeployableUnitCandidates`, the
  retract matcher `deployableUnitIntentMatchesRepository`, and the workload
  input loader).
- `workload_identity` is stored verbatim in `reducer_workload_identity`, and the
  repository summary returns each stored key with the `workload:` prefix stripped
  as the repository's workload names (`repositoryWorkloadNames` in
  `internal/query/repository_read_model_summary.go`).
- `deployment_mapping` and `platform_infra` keys feed the workload replay key
  (`workloadMaterializationReplayEntityKey` in
  `internal/reducer/platformfam/platform_materialization.go`), and the replayed
  `workload_materialization` intent goes through the same candidate filter.

The other eight are acceptance-unit labels today. They share the helper so no
domain keeps a second derivation.

## What did not change

- A repository without a display name has name == checkout basename, so every
  key is byte-identical: `TestFollowupEntityKeysUnchangedWithoutDisplayName`
  asserts the literal pre-change strings. No cassette, Ifá catalog, golden
  snapshot, work item id or phase row changes for it (`git diff --stat -- testdata/`
  is empty).
- No payload field, fact kind or exported symbol was added or removed.
- Graph writes and retract match by repository id
  (`internal/storage/cypher/canonical_deployable_unit_edges.go`) and the edge
  carries no entity key, so nothing in the graph is keyed by the old key.
- Workload node ids already derive from the repository name, so they do not move.
- Keys are scope-qualified everywhere they are consumed (work item id,
  conflict keys, phase state primary key, readiness join), so repositories that
  share one `ESHU_BOOTSTRAP_PACKAGE_NAME` share a key string harmlessly.

## Existing dependency-mode repositories heal on a FULL generation

Old queued or reopen-replayed rows keep the old basename key and stay the no-ops
they are today; they are not matched by compatibility code and there is no
migration. `internal/storage/postgres/ingestion_reopen_correlation.go` replays
succeeded rows only at or above the scope's replay floor (the active generation),
so once a new generation activates the old row is never listed again.

The five value-consumed follow-ups are emitted only after the delta early return
in `streamFacts`, so a delta generation does not heal a repository. Dependency-mode
operators heal on the next full index or reconcile, not on the next commit delta.
Graph edges are then rewritten or retracted by repository id.

Not checked: whether reconcile produces a full generation for an unchanged
dependency-mode repository on its own, and whether the first re-emitted
generation after upgrade leaves both an old-key and a new-key work item for the
same domain. The second is benign if it happens (handlers are idempotent and the
old-key item selects nothing), but it is not claimed impossible.

## Open gap: a name ending in a colon

`payloadcore.NormalizedEntityKey` returns the whole key when the colon is its
last character, so `repo:pkg:` never equals the bare candidate name `pkg:`. An
interior colon (`group:artifact`) is fine: both sides collapse to the segment
after the last colon. The gap predates this change and exists for a checkout
directory ending in a colon as well, so validating the environment variable
would close only one of the two sources. The fix belongs in the matcher (a
prefix-aware alias) or in id-based selection, both reducer contract changes.
`TestDeployableUnitCollectorKeySelectsDisplayNames` pins the current non-match
so the day it is fixed the test forces this note to change. Tracked in
#7384, together with:

- id or scope based selection (most projector intents already key on
  `<domain>:<scopeID>`), which needs the workload-name read model to stop
  reading names out of entity keys, a review of the 32-bit repository id width,
  and regenerated cassettes plus Ifá catalogs in one pass;
- Ifá catalogs that hand-build id-based keys
  (`internal/ifa/codeowners_family_catalog.go`, `internal/ifa/sql_relationship_odu.go`)
  and diverge from the collector's spelling;
- a "key selected zero of N admitted candidates" signal, which needs a reason
  label because foreign-key intents legitimately select zero.

## Proof

Each test that names the collector key is driven by the real collector stream:
`streamFacts` -> `projector/runtime.BuildReducerIntent` -> the real reducer
handler. No key is hand-written. The reducer-facing tests live beside the
collector because `projector/runtime` imports the reducer package, so a
reducer-package test cannot import the projector without a cycle.

| Test | Package | Pins |
| --- | --- | --- |
| `TestFollowupEntityKeysUseRepositoryFactName` | `collector/repo/git` | all 13 envelopes (and the 4 delta ones) carry `<prefix>:<fact name>`; count asserted |
| `TestFollowupEntityKeysUnchangedWithoutDisplayName` | `collector/repo/git` | keys byte-identical when name == basename |
| `TestDeployableUnitCorrelationSelectsDisplayNamedRepository` | `collector/repo/git` | edge written, stale edge retracted, all edges retracted when evidence is gone, foreign key untouched, pre-fix key a no-op |
| `TestWorkloadProjectionLoaderSelectsDisplayNamedRepository` | `collector/repo/git` | `Candidates` equals the admitted scope set; pre-fix key selects 0 |
| `TestPlatformReplayKeySelectsDisplayNamedRepository` | `collector/repo/git` | `deployment:<name>` -> replay `repo:<name>` -> selects the repository |
| `TestDeployableUnitCollectorKeySelectsDisplayNames` | `reducer` | plain, mixed case, interior colon, scoped npm names select; trailing colon does not (open gap) |
| `TestRepositoryWorkloadNamesReturnDisplayNamedWorkloads` | `query` | `workload:pkg-display` reads back as `pkg-display` (a contract pin; the query is unchanged, so it cannot be RED) |

RED was produced by running the collector tests with the 14 call sites
temporarily reverted to `filepath.Base(repoPath)`: the collector key tests, the
edge write and retract, the workload loader selection and the replay key all
failed for the intended reason, then passed with the fix restored.

## Gates

- No-Observability-Change: the change alters which string the collector emits and
  adds no runtime path. The existing workload_materialization completion log
  already carries `entity_keys` and `candidate_count`
  (`internal/reducer/workload_materialization_handler_logging.go`).
- No-Regression Evidence: a string concatenation replaces `filepath.Base` on a
  per-repository path. No hot path, query, lock, queue or write shape changed, so
  no benchmark is required.
- The golden-corpus gate runs filesystem mode without dependency mode. It shows
  no regression here; it cannot prove the fix.

## #7384: keys are now `<prefix>:<repo.ID>` (supersedes the verdict above)

The open gap above ("id or scope based selection") is implemented: every
follow-up key is `<prefix>:<repo.ID>` through the same `followupEntityKey`
helper, reducer selection compares ids only, and the read model resolves
display names from the repository fact instead of key suffixes.

Arbiter resolutions (all Q for lane-N):

- Q1: `workload:` + repo.ID verbatim. Selection equality is effectively on
  the id suffix; documented at the call sites.
- Q2: the read model returns the repository fact payload `name` by scope
  (`repositoryWorkloadNames` joins `reducer_workload_identity` to the scope's
  `repository` fact), never a `repository:r_...` id. A tombstoned or absent
  repository fact yields empty, not a fallback.
- Q3: `CanonicalRepositoryID` keeps the 8-hex truncation; the birthday bound
  (~1% at ~9,300 distinct repos, ~50% at ~77,000) is documented on the
  function. Corpus scale stays far below the 1% knee.
- Q4: the candidate filter reports a closed selection taxonomy
  (`no_keys` / `no_admitted_candidates` / `key_match` / `no_key_match` /
  `foreign_key_expected`, refined by `refineCandidateSelectionReason`) and the
  completion logs carry selected count plus reason.
- Q5: reducer name matching is removed. The trailing-colon gap above is closed
  by construction (no name comparison can mismatch); in-flight name-keyed
  intents select nothing for at most one generation, then collectors re-emit
  id keys.
- Q6: single PR. Cassettes regenerated in one pass (the two rationale
  cassettes whose `entity_key` was name-keyed; every other family cassette
  already keys by repo id), fixture key tables rekeyed in lockstep, and the
  four contract gates green (`fact-kind-registry`,
  `contract-source-of-truth`, `factschema-diff`, `payload-usage-manifest`).

Exemption in place of a shim: there is no compatibility dual-match. An old
name-keyed work item matches no candidate, issues no graph statement (the
retract and write sets are both the matched repositories), and heals through
the same replay-floor path §"Existing dependency-mode repositories heal"
describes: the next full generation re-emits id keys. The key tables pin the
legacy name key as explicitly non-selecting
(`TestDeployableUnitRetractScope*`, `scopeKeepKeyCases`), so a future
reintroduction of name matching fails closed.

Proof deltas against the table above: `TestFollowupEntityKeysUseRepositoryID`
/ `TestFollowupEntityKeysIgnoreDisplayName` replace the name-key tests;
`TestDeployableUnitCollectorKeySelectsDisplayNames` is now an id table (the
trailing-colon row selects via the id half); `TestFilterDeployableUnitCandidatesReportsSelectionReason`
and `TestRefineCandidateSelectionReasonForeignKey` pin Q4;
`TestRepositoryWorkloadNamesResolveIdKeysToRepositoryName` pins Q2
(id key resolves to the payload name; tombstoned repo yields empty).

#7384 Gates

- Observability Evidence: the deployable-unit and workload-materialization
  completion logs now carry `selected_candidate_count` and `selection_reason`
  (closed taxonomy), so a zero selection is distinguishable from a mismatch
  without reading keys. No new span or metric; the read-model query keeps the
  existing `postgres.query` span with `db.operation=repository_workload_names`.
- No-Regression Evidence: the read-model change keeps one round trip per
  repository summary; the workload-identity side reuses the
  `fact_records_workload_names_scope_idx` predicate and the repository side
  probes same-scope rows through `fact_records_scope_generation_idx`, so no
  new index and no benchmark is required. Reducer selection stays a
  same-scope in-memory filter over admitted candidates.
