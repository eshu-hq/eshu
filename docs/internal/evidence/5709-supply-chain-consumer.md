# #5709 — wiring the second cross-scope consumer

## What was broken

Some reducer domains read canonical output that a *different* ingestion scope
produces. A "scope" here is one collector's slice of a deployment — one
registry, one CI provider, one cloud account — and each scope publishes its work
in versioned batches called generations. A generation becomes readable only when
the scope *activates* it, which happens after the producer's own reducer row has
already reached `succeeded`.

A consumer running inside that gap reads nothing, writes a durable answer
computed without its producer's evidence, and no later event disturbs it. Not a
retry, not a visible failure. An answer, and a wrong one.

PR #6074 built the floor that closes that window: sample producer readiness
before the cross-scope load, and defer instead of committing when the load found
nothing and the producers have not activated. The deferral uses a non-counting
failure class (`cross_scope_producer_not_ready`) bounded at 30 minutes of
elapsed time, so it never dead-letters and never waits forever.

**It was wired to one of the two registered consumers.**
`crossScopeDependencyCatalog` (`go/internal/reducer/cross_scope_dependencies.go`)
declares two: `ci_cd_run_correlation`, which got the floor, and
`supply_chain_impact`, which did not. The merged evidence doc says so under
"what a reviewer should push on".

Being in the catalog does not gate a handler. The catalog drives a different
mechanism — the completion fanout that re-schedules a consumer after a producer
acknowledges. The floor is a separate opt-in: the handler has to call the helpers
and its registration has to carry the seam. So `supply_chain_impact` kept
committing early answers with every test green.

This change wires it.

## Confirmed against `origin/main` first

Everything above was re-read on `origin/main` at `435c76f63` before any code was
written, because the premise of a #5709 follow-up is exactly the kind of thing
that moves:

| claim | status on `435c76f63` |
| --- | --- |
| catalog registers two consumers | true — `crossScopeDependencyCatalog` returns `ci_cd_run_correlation` and `supply_chain_impact` |
| `supply_chain_impact` declares both producers | true — `container_image_identity`, `ci_cd_run_correlation` |
| `SupplyChainImpactHandler` is ungated | true — no `ProducerReadiness` field, no call to the floor helpers |
| the readiness store already resolves this consumer | true — `crossScopeProducerCollectorKindByDomain` maps both producer domains, so no store change was needed |
| PR #6085's single-assertion `crossScopeIdentityLookup` refactor | **not on main.** The CI/CD reference still has the two-assertion form: `crossScopeIdentityLookupPlanned` asserts the seam, then `loadActiveCICDRunCorrelationFacts` asserts it again. This change follows what is on main and uses the same two-assertion shape, so a later #6085 rebase touches both consumers the same way |

## What changed

Three edits and one new file.

**`SupplyChainImpactHandler` carries the seam.** Two new optional fields,
`ProducerReadiness` and `Logger`, matching `CICDRunCorrelationHandler`. Nil
`ProducerReadiness` means "no floor", never "not ready" — a deployment that has
not adopted the seam behaves exactly as before.

**The load pipeline samples readiness before its first cross-scope load and
decides after its last.** In `loadSupplyChainImpactEvidence`,
`armCrossScopeProducerFloor` runs immediately before the active-evidence stage
and `crossScopeProducerDeferralAfterLoad` runs immediately after the
peer-identity stage, which is the last stage that can return producer output.
One `time.Now()` reading is carried between them on a small
`supplyChainImpactCrossScopeFloor` value, so a slow load cannot push the intent
past its own 30-minute bound. Both helpers live in
`supply_chain_impact_cross_scope_readiness.go` rather than inline, because
inlining them pushed `loadSupplyChainImpactEvidence` to 154 lines and `funlen`
caps it at 150.

**The registration passes the seam through**
(`defaults_additive_domains_supply_chain.go`), and `cmd/reducer` already builds
both values for the CI/CD consumer, so nothing new was needed at the top.

### Where this could not copy the CI/CD consumer

Two seams differ, and both would have been wrong if copied.

**What counts as "the producer answered".** `CICDRunCorrelationHandler` asks a
dedicated reader that returns container-image-identity support facts and nothing
else, so a plain envelope count is the right signal there. This consumer's
cross-scope read is the *shared* active-evidence reader
(`listActiveSupplyChainImpactFactsQuery`), which returns twenty-odd fact kinds:
`sbom.component`, `vulnerability.*`, `package_registry.*`, `file`. Counting every
returned envelope would let a pass that resolved a pile of SBOM components and
zero producer facts disarm the floor — the exact case the floor exists for. So
this consumer counts only envelopes whose fact kind belongs to a declared
producer (`reducer_container_image_identity`,
`reducer_ci_cd_run_correlation`), and counts the *delta* across the cross-scope
stages rather than an absolute, because `supplyChainImpactFactKinds` also asks
the intent's own scope for those same kinds.

**What counts as "this pass had something to look up".** The CI/CD gate asks
whether the pass has any artifact digest or workflow image ref. Here the
equivalent question is which filter dimensions the SQL can match a producer row
on, and that is answerable from the query text rather than by analogy.
`listActiveSupplyChainImpactFactsQuery` reaches either producer kind through
exactly three predicates: the digest branch (`subject_digest`, `digest`,
`artifact_digest`, `referrer_digest`, `resolved_digest` against
`SubjectDigests`), `image_ref` against `ImageRefs`, and the repository branch —
whose fact-kind list names both producer kinds — against `RepositoryIDs`. Package
IDs, purls, CVE IDs, advisory IDs, product criteria, and document IDs match
neither producer's payload, so a pass carrying only those can never resolve one
however long it waits. It must not defer.

There is also a structural difference worth naming: the CI/CD consumer issues
*one* cross-scope load, and this one issues up to ten. The until-stable loop runs
up to 8 rounds, each seeded by the last, then the resolved-digest re-run (#5464)
and the peer-identity pass (#5468) follow. The resolved-digest stage is where a
pure OS-package finding's container image identity arrives, long after the loop
settles, so the producer count has to be taken at the end. A count taken when the
loop settles compiles, passes most tests, and defers exactly the OS-package
findings the identity read exists to serve. That is
`TestSupplyChainImpactCountsProducerFactsFromTheResolvedDigestStage`; my first
version of the fixture did not reach that stage, the mutation ran green, and the
test had to be rebuilt around a scope-partitioned loader before it guarded
anything.

Also: `loadActiveSupplyChainImpactFactsUntilStable` now takes its first-round
filter from the caller. The floor has to inspect that filter before the load
runs, and passing one value means the two cannot disagree about what this pass
asked for — and that the envelope set is still scanned once, not twice.

## The two properties the CI/CD wiring learned the hard way

**Pre-load ordering: the same window exists here, and it is closed the same
way.** The write this floor prevents is durable and unrepaired. The completion
fanout's reopen selects `succeeded` rows, and a maintenance pass racing an intent
that is still `claimed` skips it, so a wrong answer written under a stale
snapshot stays. Sampling readiness after the load lets a producer activate in
between, which makes the store answer "ready" about a snapshot the load already
took without it. `TestSupplyChainImpactDefersDespiteProducerActivatingDuringTheLoad`
flips readiness during the load and fails if the sample moves after it. Moving
the sample broke that test and no other, which is what a targeted guard should
do.

**Batch-wide disarm: yes, it applies here, identically.** One
`SupplyChainImpactHandler.Handle` pass classifies every vulnerability finding in
a scope generation and issues its cross-scope loads filtered by the union of every
finding's package IDs, digests, and repository IDs. The producer count the floor
reads is that one batch-wide number. So a generation carrying finding A, whose
repository an already-activated OCI scope published identity for, and finding B,
whose identity is still inside its producer's activation window, resolves one
envelope and does not defer. B then commits an answer computed without its
producer's evidence — blank `SubjectDigest`, and every downstream
repository-keyed join in `supply_chain_impact_runtime.go` returning nil for it.
That is the #5709 defect, unchanged.

It stays batch-wide for the same reason it does on the CI/CD side. Deferring the
whole batch holds back findings that already have their evidence, on a class that
freezes `attempt_count`, and the real fix is per-digest readiness —
`CrossScopeProducerReadiness` answers per consumer domain and scope, not per
artifact, so that is a different contract than #5709 specifies and it is not
built. `TestSupplyChainImpactDoesNotDeferABatchWhereAnotherFindingResolved` pins
the behaviour so it stays a known property rather than a rediscovery.

## Residual windows

Four of these are inherited from the shared floor and were disclosed in
`5709-cross-scope-readiness-floor.md`. I re-read each against `origin/main` and
all four still hold. The fifth is new and belongs to this consumer.

**The collector-kind map is under-inclusive, on purpose.**
`container_image_identity` intents are also enqueued in `aws`, `azure`, `gcp`,
`git`, and `sbom_attestation` scopes (`containerImageIdentityCandidateFactKinds`,
`internal/projector/container_image_identity_intents.go`), so identity output can
be published by a scope `crossScopeProducerCollectorKindByDomain` does not name.
The floor does not wait for those. A finding whose identity comes from an ECR
scope is still answered early.

**Quiescence does not prove the producer's reducer has run.** The probe checks
that a producer scope's generation is active and its *projector* work has
drained. That is a proxy for the read the consumer performs, not the same
predicate. A producer that has activated and drained but whose identity reducer
has not yet written its support set reads as ready and joins to nothing.

**The gap before projector items exist.** `fact_work_items` carries only
`projector` and `reducer` stages. Between a new generation being scheduled and
its projector items being enqueued, the scope has no live projector row, so the
probe reads it as quiescent off its *previous* activation.

**"At least one quiescent scope of the kind", not all of them.** With several
OCI registry scopes, one idle registry satisfies the whole kind and disarms the
floor for every consumer on every pass. This one bites `supply_chain_impact`
harder than the CI/CD consumer in one direction and softer in another: it has two
producer kinds, so there are two chances to be held back, but also two kinds whose
"at least one" can be satisfied by an idle sibling.

**New here: a producer-reachable dimension that only appears in a later stage is
not gated.** `crossScopeProducerLookupPlanned` inspects the *initial* filter,
computed before the first active-evidence load, because readiness has to be
sampled before that load. A pass whose only digest arrives from the
scanner-analysis stage — the OS-package path — therefore reads as "no floor" and
commits whatever it resolved. That is the pre-#5709 behaviour for that path, not
a regression, and it is the safe direction: the alternative is arming the floor
for passes that can never resolve a producer, which costs a 30-minute deferral on
a retry that does not back off. In practice the initial filter carries a
repository ID for almost any real vulnerability generation, since package
consumption correlations, security alerts, suppressions, workload identity, and
platform materialization all contribute one, so the gate arms on the ordinary
path. How often it does not has not been measured.

## Cost

`supply_chain_impact` declares two producer domains, so a pass that arms the
floor runs two `ProducerScopeQuiescence` queries rather than the CI/CD
consumer's one.

Two, not "up to two". The store used to stop at the first registered kind with
no quiescent-active scope, and per-producer readiness cannot: the producers
after the miss still need answers of their own. So a deferring pass runs one
more sub-millisecond indexed probe than it did, on a pass that is about to wait
anyway, and a committing pass runs the two it always ran. Kinds are still
deduplicated, so two producers mapping to one collector kind would cost one
query.

Two kinds also means two conditions to satisfy, so this consumer is strictly more
likely to defer than the CI/CD one. I have not measured how much more likely.
That sentence is a reading of `CrossScopeProducersReady`, not a number.

No-Regression Evidence: the floor adds two indexed `ProducerScopeQuiescence`
queries per `supply_chain_impact` pass that has a producer-reachable filter
dimension, and zero for a pass that does not — the nothing-to-look-up gate skips
both the probe and the deferral. No existing query
changed shape or predicate. The plan proof for the probe is the Index Scan in
`docs/internal/evidence/5709-quiescence-probe.md`, unchanged by this change
because the query is unchanged: same Nested Loop Anti Join, same
`fact_work_items_scope_generation_idx`, 0.34 ms median on a seeded 500-scope ×
50,000-work-item shape. `loadActiveSupplyChainImpactFactsUntilStable` now
receives its first-round filter from the caller instead of deriving it itself.
That is a wash against today's code, not a saving: one `supplyChainImpactFilter`
pass over the pre-load envelope set before, one after. It is a saving only
against the naive version of this change, where the floor's predicate would have
scanned that set a second time. The filter values and the resulting SQL arguments
are byte-identical, which
`TestSupplyChainImpactFilterExpandsSBOMComponentByCanonicalPackageID` and the
rest of `internal/reducer` cover. Baseline versus after: `internal/reducer`,
`cmd/reducer`, and `internal/storage/postgres` are green before and after.
Terminal queue state is unchanged — the floor enqueues nothing, adds no status
transition, and only converts one durable write into a bounded retry in a
failure class that was already enrolled as non-counting. Limits: plan shape only,
single connection, no contention arm, and no measurement of how often this
consumer defers in a real deployment. None was taken.

Observability Evidence: a deferral this consumer takes is observable exactly like
the CI/CD consumer's. It carries the existing `cross_scope_producer_not_ready`
failure class on `fact_work_items`, counted by
`eshu_dp_reducer_retry_surge_total{failure_class}` and read by the golden-corpus
drain breakdown as readiness-deferred, and it emits one structured log line per
deferral through the shared `logCrossScopeProducerNotReadyDefer`, carrying
`domain`, `scope_id`, `generation_id`, `producer_domains`,
`elapsed_since_cycle_start`, and `max_wait`. Elapsed against the bound, never
`attempt_count`: this class freezes `attempt_count`, so it reads as a constant on
every occurrence and cannot tell an operator how close the intent is to
converging. `producer_domains` now reports two values for this consumer rather
than one, and it still comes from the bounded catalog, so it stays a small closed
set rather than a high-cardinality label. No new metric. The error message names
the consumer domain, scope, generation, and the bounded producer set — never a
uid, which could be a redacted identifier.

## Concurrency

Nothing about lease, status, ordering, or idempotency changes. The probe is a
plain `SELECT` with no `FOR UPDATE`, no advisory lock, and no write, so it takes
no row locks and cannot deadlock against a claim, an acknowledgement, or another
reducer's projection. It runs outside the claim transaction — the queue claims
the work item and commits, then the handler runs — so a slow probe delays one
handler rather than the claim path other workers depend on.

A deferral returns an error the queue already classifies. The row goes back to
retrying under its existing lease rules, in a class already enrolled as
non-counting. No new work item is enqueued and the conflict domain (one scope
generation, one domain) is untouched.

Two consumers probing the same producer concurrently see the same committed state
and reach the same answer independently; there is no shared mutable state between
them. Two passes of the same intent cannot overlap, because the queue claim is
exclusive.

The one race worth naming is the readiness sample against a producer activating
concurrently, and it is asymmetric on purpose. Sampling before the load means the
signal can only be staler than the load, never fresher. A producer that activates
between the sample and the load makes the load read more evidence than the signal
assumed, and the post-load producer count is what decides.

## Readiness and evidence are tracked per producer

The first version of this floor kept one readiness bool and one resolved count
for both declared producers, and review caught what that arithmetic cannot say.
`container_image_identity` publishing twice and `ci_cd_run_correlation`
publishing nothing produced the same pair of numbers as both producers answering
once — `ready = false, resolved = 2` — so the floor disarmed and the handler
durably wrote findings with no deployment context at all. That is the defect
this floor exists to prevent, arriving through the counting rule instead of the
timing window.

The rule now compares one producer at a time. `CrossScopeProducersReady` returns
`CrossScopeProducerReadinessByDomain`, an answer per declared producer, and
`countSupplyChainImpactCrossScopeProducerFacts` returns a count per producer
domain, keyed off the fact kind each producer writes. A pass defers while any
declared producer is both unready and unrepresented in the evidence.

Three consequences worth stating plainly:

- The readiness store no longer stops probing at the first non-quiescent kind.
  It cannot: the producers after it still need answers of their own. A
  deferring `supply_chain_impact` pass therefore runs two quiescence probes
  where it used to run one. `ci_cd_run_correlation`, with a single producer, is
  unchanged at one. The probe is the shape with a committed `EXPLAIN`
  (`5709-quiescence-probe.md`, 0.30 ms on the seeded plan), so the cost is one
  additional sub-millisecond indexed read on a pass that is about to wait
  anyway.
- A producer the store says nothing about reads as unready. A store that drops
  a declared producer costs a bounded deferral instead of a wrong answer, and
  resolved evidence still disarms it.
- `ci_cd_run_correlation` reads a dedicated producer reader, so every envelope
  it gets is that one producer's output and a whole-batch count is
  unambiguous. `singleProducerResolvedCounts` credits it accordingly, and
  credits nothing at all if that consumer ever declares a second producer —
  `TestCICDRunCorrelationDeclaresExactlyOneProducer` fails first if it does.

This narrows one axis of the batch-wide residual window recorded above (which
producer the evidence came from) and leaves the other exactly where it was
(which finding in the batch it belonged to).

## Which guards were proven to guard

Every guard below was run against a deliberately broken production line and
observed to fail, then the break was reverted. A test that passes either way
guards nothing. Real output for each is in the PR body; the failing assertion is
quoted here.

| break introduced | test that caught it | what it printed |
| --- | --- | --- |
| `ProducerReadiness` dropped from the `supply_chain_impact` registration | `TestSupplyChainImpactRegistrationCarriesTheReadinessSeam` **and** `TestBuildReducerServiceWiresCrossScopeProducerReadinessForSupplyChainImpact` | `ProducerReadiness is nil on the registered handler: the readiness floor would ship inert` / `execution error = nil, want a readiness deferral` |
| `Logger` dropped from the same registration | the same two | `Logger is not the one passed to DefaultHandlers` |
| readiness sampled *after* the load (re-arm the floor just before the decision) | `TestSupplyChainImpactDefersDespiteProducerActivatingDuringTheLoad` plus `...IgnoresProducerFactsAlreadyInItsOwnScope` | `Handle() error = <nil>, want a deferral: readiness must be sampled before the load, not after` |
| nothing-to-look-up gate removed | `...DoesNotDeferWhenThereIsNothingToLookUp`, `...DoesNotDeferWithoutTheCrossScopeLoaderSeam`, `...ProducerLookupPlannedTracksTheFilterDimensions` | `want nil: a pass that cannot reach a producer fact must not defer` |
| `RepositoryIDs` dropped from the producer-reachable dimensions | `...DefersWhenProducerScopesHaveNotActivated` and 3 more | `crossScopeProducerLookupPlanned() = false, want true` |
| every cross-scope envelope counted, not only producer-owned ones | `...CountsOnlyProducerOwnedCrossScopeFacts` | `want a deferral: only producer-owned facts may disarm the floor` |
| `ci_cd_run_correlation` dropped from the producer fact-kind map | `...ProducerFactKindsCoverEveryDeclaredProducer`, `...CICDCorrelationAlsoDisarmsTheFloor` | `producer domain ci_cd_run_correlation has no fact kind ...: its output would never disarm the floor` |
| absolute producer count instead of the cross-scope delta | `...IgnoresProducerFactsAlreadyInItsOwnScope` | `want a deferral: only producer facts the CROSS-SCOPE read resolved may disarm the floor` |
| producer count taken when the until-stable loop settles, before the resolved-digest stage | `...CountsProducerFactsFromTheResolvedDigestStage` | `the producer count must be taken after the resolved-digest stage` |
| per-producer resolved counts summed back into one aggregate | `TestSupplyChainImpactDefersWhenOnlyOneProducerResolved`, `TestCrossScopeUnreadyProducersEvaluatesEachProducerSeparately` | `Handle() error = <nil>, want a deferral: two envelopes from one producer do not make two producers ready` / `crossScopeUnreadyProducers() = [], want [ci_cd_run_correlation]` |
| `cross_scope_producer_not_ready` removed from `nonCountingReducerRetryFailureClasses` | `TestReducerContentionGateCrossScopeReadinessDeferralKeepsItsAttemptBudget` (live) | `cycle 2: claimed Intent.AttemptCount = 2, want 1 frozen` |
| readiness store reads a registered-but-unactivated producer scope as ready | the same live proof | `cycle 1: Handle() error = nil, want a deferral while the producer scopes have not activated` |
| the contention gate's `-run` filter narrowed so the live proofs stop being selected | `TestCrossScopeReadinessProofsRunInTheReducerContentionGate` | `the reducer contention gate's -run filter "..." does not select TestReducerContentionGateCrossScopeReadinessDeferralKeepsItsAttemptBudget` |

The ordering row catches two tests rather than one. That is an artefact of how
the break is written against the extracted helper — re-arming the floor also
re-reads the producer baseline, which trips the own-scope guard as well. The
ordering test is still the targeted one: before the helpers were extracted, the
same break failed that test alone.

The last row is the one that went wrong first. The mutation ran **green** against
my original fixture, because that fixture only ever reached the until-stable
loop's later rounds and never the resolved-digest stage. The test was rebuilt
around a scope-partitioned loader that makes the stage observably run
(`digestRoundHit`), and only then did the mutation fail.

## Proof

```
$ cd go && go test ./internal/reducer/ ./cmd/reducer/ ./internal/storage/postgres/ -count=1
ok  	github.com/eshu-hq/eshu/go/internal/reducer	3.235s
ok  	github.com/eshu-hq/eshu/go/cmd/reducer	1.761s
ok  	github.com/eshu-hq/eshu/go/internal/storage/postgres	5.651s
tests_exit=0

$ cd go && go test -race ./internal/reducer/ ./cmd/reducer/ -count=1
ok  	github.com/eshu-hq/eshu/go/internal/reducer	14.736s
ok  	github.com/eshu-hq/eshu/go/cmd/reducer	5.208s
race_exit=0

$ bash scripts/verify-package-docs.sh
verify-package-docs: changed Go package docs present
pkgdocs_exit=0

$ bash scripts/verify-telemetry-coverage.sh
verify-telemetry-coverage: docs/public/observability/telemetry-coverage.md and
go/internal/telemetry/instruments.go agree, no new untracked stages
telemetry_exit=0

$ ESHU_PERFORMANCE_EVIDENCE_BASE=origin/main bash scripts/verify-performance-evidence.sh
verify-performance-evidence: benchmark and observability markers found for hot-path changes
perfevidence_exit=0

$ git diff --check
diffcheck_exit=0
```

### The real queue, not a fake

Review made the point the handler tests above cannot settle: this handler returns
a **non-counting** retry error, and no fake queue can establish that the real
`fact_work_items` path freezes `attempt_count`, keeps the writer quiet while the
row waits, hands the row back on the next claim, and still reaches a terminal
answer. A fake that cannot produce the failing row passes forever. That was a
fair reading of what this doc claimed, and the claim was larger than the
evidence.

`TestReducerContentionGateCrossScopeReadiness*` closes it against real
PostgreSQL. The real `ReducerQueue` Claim/Fail/Ack SQL runs against the real
`fact_work_items` DDL, the real `CrossScopeProducerReadinessStore` reads real
`ingestion_scopes` rows through the committed quiescence probe, and the real
`SupplyChainImpactHandler` produces the real deferral error the queue then
classifies:

```
$ ESHU_POSTGRES_DSN=postgres://…@localhost:15693/eshu go test ./internal/storage/postgres/ \
    -run '^TestReducerContentionGateCrossScopeReadiness' -count=1 -v
=== RUN   TestReducerContentionGateCrossScopeReadinessDeferralKeepsItsAttemptBudget
--- PASS: TestReducerContentionGateCrossScopeReadinessDeferralKeepsItsAttemptBudget (0.25s)
=== RUN   TestReducerContentionGateCrossScopeReadinessConvergesAtTheElapsedBound
--- PASS: TestReducerContentionGateCrossScopeReadinessConvergesAtTheElapsedBound (0.10s)
PASS
ok  	github.com/eshu-hq/eshu/go/internal/storage/postgres	3.237s
```

The first drives five claim → handle → `Fail` cycles with both producer scopes
registered and unactivated. Five is deliberately more than `MaxAttempts = 3`: a
counting class would have dead-lettered the row by cycle four. It reads the
durable row back each cycle — `status = retrying`, `failure_class =
cross_scope_producer_not_ready`, `attempt_count = 1` — and asserts the writer was
never called. Then it activates the producer scopes, and the same handler against
the same store commits: writer called once, `Ack` drives the row to `succeeded`
with `attempt_count` still at 1. The second covers the other terminal path, a
producer that never activates: a row whose cycle began 45 minutes ago commits on
its first claim rather than waiting forever in a class that never dead-letters.

Both are DSN-gated and skip on a machine with no database, so a hermetic sibling
carries the part that must hold everywhere.
`TestCrossScopeReadinessProofsRunInTheReducerContentionGate` reads
`.github/workflows/reducer-contention-gate.yml`, confirms it still passes
`ESHU_POSTGRES_DSN`, and compiles its `-run` filter to check it still selects
both live proofs. That gate runs a PostgreSQL service on every PR touching
`go/internal/reducer/**` or `go/internal/storage/postgres/**`, which this change
does, so the live proofs run in CI rather than skipping there. Narrowing the
filter fails the hermetic guard, which is the row proving it.

What stays a fake is the handler's **fact source**. Supply-chain evidence arrives
through a different store than the queue under test, and seeding it would prove
nothing about `fact_work_items`. Proving the deferral against real fact rows end
to end needs a full ingest, which belongs to the remote corpus run rather than
here.

Still not included: no golden-corpus (B-7) run, and no measurement of deferral
frequency under a real workload.

**The code-coverage report could not be regenerated on this machine.**
`scripts/generate-code-coverage-report.sh` runs `go test ./...`, and six
`cmd/eshu` vulnerability-scan tests fail here because they need a local Postgres
that is not reachable (`dial tcp 127.0.0.1:15439: connect: connection refused`).
The generator aborts on that non-zero exit before writing anything. Those
failures are **not** from this change: they reproduce identically on a pristine
`origin/main` worktree at `435c76f63`, with the failure text differing only in
the worktree path —

```
--- FAIL: TestRunVulnScanRepoSARIFExportPreservesScannerReportContract (0.86s)
    resolve repo selector ".../5709-baseline-check": no matching repository
```

and this change cannot reach them regardless: only `cmd/reducer` wires
`CrossScopeProducerReadinessStore`, so the `cmd/eshu` pipeline runs with a nil
seam and the floor is disabled there by construction. The coverage gate is
`blocking: false`, so this is drift to refresh when the local stack is free, not
a failure to fix.

Refs #5709
