# Generation Liveness Backfill Readiness Evidence

This note covers the #4207 generation-liveness predicate that decides when an
aged active generation is a real source-local wedge versus unready downstream
backlog.

No-Regression Evidence: the baseline clean-volume full-corpus run
`post4368-currentmain-full-20260630t2010z` on Eshu commit `ed334d8fa` and
NornicDB `v1.1.9@sha256:9a5126d306a48c01869809da47a869a4521b9328a7ab1c855327f5fd7541e4cd`
reached collection/projection `896/896` with no failed, dead-letter, or
retrying rows and no ungranted Postgres locks, but it was not terminal:
`relationship_evidence_facts=5,050`, `backward_evidence_committed=0`, queue
`10,807 total / 8,033 open / 2,774 succeeded`, and generation liveness
recovered `12` source-local rows to `pending` while their only uncompleted
shared-projection backlog was cross-repo `repo_dependency`. The after-input
shape is the same aged active generation with drained same-generation reducer
work and an uncompleted shared-projection intent, but the predicate now treats a
cross-repo `repo_dependency` source-run (`repo_dependency` or
`repo_dependency:<scope>`) as actionable only after the matching
`graph_projection_phase_state` row exists with keyspace `cross_repo_evidence`
and phase `backward_evidence_committed`. The focused red/green proof was
`go test ./internal/storage/postgres -run
'TestRecoverWedgedActiveGenerationsQueryContract|TestGenerationLivenessStoreCountActiveByAge|TestRecoverWedgedActiveGenerationsExcludesHealthyQuietScopes|TestRecoverWedgedActiveGenerationsRecoversBlockedScopes'
-count=1`: it failed before the readiness predicate because the recovery and
stuck-age SQL did not reference `graph_projection_phase_state` /
`backward_evidence_committed`, then passed after the predicate was added.
Final local proof also passed `go test ./internal/storage/postgres -count=1`,
`go test ./internal/reducer -run 'CrossRepo|RepoDependency|Readiness|BackwardEvidence'
-count=1`, `git diff --check`, strict MkDocs build, and `make pre-pr`
including changed-package tests, file cap, telemetry coverage, coverage report,
performance-evidence, and the scoped race lane. This PR does not claim terminal
full-corpus closure for #4207; a fresh clean-volume full-corpus run must still
prove queue/source-local drain after merge.

No-Observability-Change: this predicate adds no table, index, DDL migration,
worker, lease, queue domain, runtime knob, route, metric series, metric label,
span name, or log field. Operators continue to diagnose this lifecycle through
existing `generation liveness recovery cycle completed` logs,
`CountActiveGenerationsByAge` stuck/aging buckets, durable `fact_work_items`
queue/source-local counts, `shared_projection_intents` backlog,
`graph_projection_phase_state` readiness rows, failure-class counts, and
Postgres lock-wait samples. The observable behavior changes only by classifying
unready cross-repo repo-dependency backlog as aging instead of stuck until
backward evidence is committed.
