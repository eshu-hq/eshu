# #5092 relationship-family fact-load guard evidence

Performance Evidence: retained clean DB proof from
`3624-reference-keys-clean-20260711T235901Z` compared the merged #5094
production load path against a relationship-family prefilter candidate before
implementation. The proof loaded OLD/current production facts and NEW candidate
facts for the slow task-777 partition, high-evidence source scopes, and an
ApplicationSet cross-scope case, then ran `relationships.DiscoverEvidence` with
the same repository catalog and compared canonical evidence keys in both
directions.

Measured theory proof:

- `git-repository-scope:repository:r_de3355a0` OLD/current production:
  `old_facts=16361`, `old_evidence=0`, `old_load=5m30.619s`.
- Same task-777 scope with the relationship-family prefilter:
  `candidate_facts=0`, `candidate_evidence=0`, `candidate_load=8.358s`,
  evidence diff `missing=0`, `unexpected=0`.
- Sampled evidence-producing scopes preserved exact evidence with 0/0
  bidirectional diff: `r_d393ab02`, `r_6d9013cd`, `r_7616858d`,
  `r_15dfa4e6`, `r_a1c966b1`, `r_a0dbf5f4`, `r_ba232843`, `r_415c2ca3`,
  `r_68db34ea`, and `r_4d50ff71`.
- ApplicationSet cross-scope proof `r_d737ae8e` also preserved exact evidence
  after retaining nested `argocd/**`, `applicationset(s)/**`, and
  `kind: Application` / `kind: ApplicationSet` marker files.

Implementation proof shape: the production query now materializes bounded
`source_facts`, then applies `deferredRelationshipFamilyCandidatePredicateSQL`
before computing `payload_lower` in `relationship_family_payload_facts`. The
broad non-repo-alias arm, the reference-key side-table arm, and the repo-id
fallback arm all start from that relationship-family payload set, so generic
content/file partitions do not pay either the alias substring scan or the
indexed-reference catalog join. This is a selection guard before the previously
measured false-positive relationship fact-load arms, not a worker-count,
serialization, lease, queue, or write-path change.

Rejected implementation proof: the first production-query implementation put the
relationship-family predicate inline before `payload_lower LIKE ANY($1)`. It was
accuracy-clean, but not performance-clean on the retained DB:

- task-777 OLD/pre-guard: `old_facts=16361`, `old_evidence=0`,
  `old_load=5m31.421s`.
- task-777 NEW/inline guard: `new_facts=0`, `new_evidence=0`,
  `new_load=6m27.243s`.

That shape was rejected because the unconditional ArgoCD content-marker carveout
still lower-scanned large generic content payloads before returning zero rows.
A follow-up task-777 SQL shim compared the current unconditional marker filter
with a CASE-gated marker that only inspects content for YAML paths:

- current unconditional marker guard count: `0` rows in `7749.358ms`.
- CASE-gated marker guard count: `0` rows in `2484.125ms`.

The refined production query therefore splits `source_facts` and
`relationship_family_payload_facts` so the expensive `lower(payload::text)`
expression is only computed for relationship-family facts.

Current production-query proof on this branch:

- Task-777 worst partition:
  `TestDeferredRelationshipFamilyGuardRetainedDBWorstPartitionLoad` loaded
  `new_facts=0` in `6.925268958s` after the Salt `gitfs_remotes` marker
  fallback and Dockerfile/Jenkinsfile prefix basename guard were added.
- Review-fix refresh after adding the Salt YAML path gate and matching
  scope/generation fences to both reference-key arms:
  `TestDeferredRelationshipFamilyGuardRetainedDBWorstPartitionLoad` loaded
  `new_facts=0` in `6.25738575s`.
- Targeted retained evidence-equivalence refresh after the same review fixes:
  `r_68db34ea` preserved exact evidence with `old_facts=37`,
  `new_facts=34`, `old_evidence=238`, `new_evidence=238`,
  `old_load=832.608959ms`, and `new_load=800.830375ms`.
- Review regression tests:
  `TestDeferredRelationshipFamilyGuardKeepsSaltGitfsFallback` verifies the SQL
  family guard keeps top-level Salt `gitfs_remotes:` content, and
  `TestDeferredRelationshipFamilyGuardKeepsDockerfileAndJenkinsfilePrefixes`
  verifies the guard keeps `Dockerfile.*` / `Jenkinsfile.*` paths that
  `relationships.DiscoverEvidence` recognizes.
  `TestDeferredRelationshipFamilyGuardPathGatesSaltGitfsFallback` verifies the
  Salt content marker is gated by YAML paths before scanning content, and
  `TestDeferredRelationshipFamilyGuardScopeFencesReferenceKeyArms` verifies both
  reference-key side-table arms apply the same scope/generation fence. The
  relationships fixture test `TestSaltFormulasB7FixtureResolves` verifies the
  fixture still produces `SALT_FORMULA_REFERENCE` evidence.
- Retained evidence scopes preserved exact evidence:
  - `r_d393ab02`: `old_facts=103`, `new_facts=100`, `old_evidence=788`,
    `new_evidence=788`, `old_load=2.72416375s`, `new_load=2.472079708s`.
  - `r_415c2ca3`: `old_facts=1239`, `new_facts=1123`, `old_evidence=210`,
    `new_evidence=210`, `old_load=9.617611083s`, `new_load=8.433963583s`.
  - `r_d737ae8e`: `old_facts=367`, `new_facts=271`, `old_evidence=123`,
    `new_evidence=123`, `old_load=4.385380334s`, `new_load=4.2141575s`.
  - `r_6d9013cd`: `old_facts=107`, `new_facts=100`, `old_evidence=572`,
    `new_evidence=572`, `old_load=3.290941375s`, `new_load=3.222823166s`.
  - `r_7616858d`: `old_facts=106`, `new_facts=100`, `old_evidence=478`,
    `new_evidence=478`, `old_load=2.984213833s`, `new_load=2.999677792s`.
  - `r_15dfa4e6`: `old_facts=55`, `new_facts=52`, `old_evidence=456`,
    `new_evidence=456`, `old_load=1.189070375s`, `new_load=1.205099833s`.
  - `r_a1c966b1`: `old_facts=119`, `new_facts=116`, `old_evidence=284`,
    `new_evidence=284`, `old_load=2.005701291s`, `new_load=2.000616084s`.
  - `r_a0dbf5f4`: `old_facts=92`, `new_facts=88`, `old_evidence=242`,
    `new_evidence=242`, `old_load=1.54108675s`, `new_load=1.552038959s`.
  - `r_ba232843`: `old_facts=122`, `new_facts=112`, `old_evidence=192`,
    `new_evidence=192`, `old_load=3.107008541s`, `new_load=2.763602042s`.
  - `r_68db34ea`: `old_facts=37`, `new_facts=34`, `old_evidence=238`,
    `new_evidence=238`, `old_load=760.529541ms`, `new_load=716.011459ms`.
  - `r_4d50ff71`: `old_facts=75`, `new_facts=72`, `old_evidence=173`,
    `new_evidence=173`, `old_load=945.034208ms`, `new_load=948.411167ms`.

Local proof on this branch:

- `GOCACHE=<worktree>/.gocache go test ./internal/storage/postgres -run 'TestDeferredRelationshipFamilyGuard' -count=1`
- `GOCACHE=<worktree>/.gocache go test ./internal/relationships -run 'TestSaltFormulasB7FixtureResolves|TestDiscoverSaltEvidence|TestIsSaltGitfsArtifact' -count=1`
- `GOCACHE=<worktree>/.gocache ESHU_RELATIONSHIP_FAMILY_PROOF_DSN=<retained-clean-db> go test ./internal/storage/postgres -run '^TestDeferredRelationshipFamilyGuardRetainedDBWorstPartitionLoad$' -count=1 -timeout=3m -v`
- `GOCACHE=<worktree>/.gocache ESHU_RELATIONSHIP_FAMILY_PROOF_DSN=<retained-clean-db> ESHU_RELATIONSHIP_FAMILY_PROOF_SCOPES=<retained-evidence-scopes> go test ./internal/storage/postgres -run '^TestDeferredRelationshipFamilyGuardRetainedDBEvidenceEquivalence$' -count=1 -timeout=15m -v`
- Review-fix reruns:
  `go test ./internal/storage/postgres -run 'TestDeferredRelationshipFamilyGuard(PathGatesSaltGitfsFallback|ScopeFencesReferenceKeyArms|KeepsSaltGitfsFallback|KeepsDockerfileAndJenkinsfilePrefixes|WrapsPayloadScanningArms)' -count=1`,
  `go test ./internal/storage/postgres -run 'TestDeferredRelationshipFamilyGuard|TestBackfillDeferredPassExcludesSelfRepoIDMatch|TestDeferredScoped' -count=1`,
  `go test ./internal/relationships -run 'TestSaltFormulasB7FixtureResolves|TestDiscoverSaltEvidence|TestIsSaltGitfsArtifact|TestDiscoverDockerfileSourceLabelEvidence|TestDiscoverJenkins' -count=1`,
  `ESHU_RELATIONSHIP_FAMILY_PROOF_DSN=<retained-clean-db> go test ./internal/storage/postgres -run 'TestDeferredRelationshipFamilyGuardRetainedDBWorstPartitionLoad' -count=1 -v`,
  and
  `ESHU_RELATIONSHIP_FAMILY_PROOF_DSN=<retained-clean-db> ESHU_RELATIONSHIP_FAMILY_PROOF_SCOPES='git-repository-scope:repository:r_68db34ea' go test ./internal/storage/postgres -run 'TestDeferredRelationshipFamilyGuardRetainedDBEvidenceEquivalence' -count=1 -v`.

The retained-DB production-query proof is codified as
`TestDeferredRelationshipFamilyGuardRetainedDBEvidenceEquivalence`. It is gated
by `ESHU_RELATIONSHIP_FAMILY_PROOF_DSN` and derives the OLD pre-guard query from
the current production query. It must pass against the retained clean DB with
both exact evidence and a task-777 load-time improvement before any clean
896-repository stack run is promoted.

No-Observability-Change: this change adds no metric, span, log field, route,
worker, lease, batch size, queue state, or runtime knob. Operators continue to
see the same deferred backfill partition counters, worker counts,
`DeferredBackfillPartitionLoadDuration`, and `deferred_backfill_fact_load_completed`
log line. The expected operational signal is a lower partition load duration for
the same partition shape with unchanged terminal queue and relationship evidence
truth.
