# Go repository scope planning: runtime cost and signals (#10027)

## What changed at runtime

The coordinator gains one new read, and it runs only for an activation of
`eshu.scanner.go-reachability` with `derive_from_go_modules` enabled. No activation
can enable it yet: admission refuses the component until its image digest is
pinned. Every other activation reads nothing new, which
`TestGoModulesDerivationLeavesOtherScannerActivationsAlone` and
`TestGoReachabilityTargetResolverIgnoresOtherScopes` prove.

Each reconcile pass does one of two things:

1. **The bucket's run exists:** one primary-key probe on `workflow_runs`, then stop.
2. **It does not:** read one page of at most 256 candidates, plan, and admit through
   the existing guarded path.

A page that plans no run never creates one, so the second case repeats every tick for
that bucket. That happens when the page has no plannable repository, or every target
is still open. At the 30 s default tick that is up to 2,880 reads a day per activation,
each about 98 ms and 129,543 buffers in the measurement below, plus one INFO line. An
all-open page also adds to the counter each tick. This cost is deliberate and bounded:
it admits a target as soon as it closes.

The extension host adds two primary-key reads of `workflow_runs` to a Go
reachability claim: one to deliver the target, and one to select the installation
when a provider is set and no exact scope matches. Both are small beside a claim
that runs a checkout and `govulncheck`.

## Performance Evidence: choosing the go.mod probe

Backend: Postgres 16 (`postgres:16`, disposable container). Worst case for the
probe: no repository's row ends the scan early.

Input shape: 3,000 repository scopes with active generations. The first 300 hold
1,840,060 `content_entity` facts (20 repositories at 50,000, 280 at 3,000, each
fifth with a `module_declaration` row written last) and 820,060 `content_files`
rows. One page is 256 candidates. `EXPLAIN (ANALYZE, BUFFERS)`, cold cache.

| Candidate | Execution | Buffers read | Migration |
| --- | --- | --- | --- |
| `module_declaration` fact `EXISTS`, existing indexes | 1,511 ms | 196,524 | none |
| `content_files` path `EXISTS` on `content_files_repo_path_idx` | 68 ms | 7,522 | none |
| Fact `EXISTS` on a theoretical partial index | 5 ms | 2 | required |

The fact probe filters about 5,340 rows per repository, and a repository with no
such row scans to its end. The cost grows with facts per repository, so a
real monorepo costs more than this fixture. A page with no Go repository plans no
run, so it would repeat that read on every reconcile. The `content_files` probe was
chosen: it meets the bound with no migration, and it is the source
`GoModuleManifestsQuery` already reads.

What the choice gives up: it can disagree with the fact for a `go.mod` with no
`module` line, or for content ahead of or behind the active generation. The
component reads the checked-out commit itself, so the cost is one empty scan, or a
one-rotation delay for a repository that just gained a `go.mod`. A repository with
no stored `go.mod` is never planned. This was not measured on a production corpus.

## Performance Evidence: the final query

Same data, plus 100,000 `vulnerability_intelligence` scopes in `ingestion_scopes`,
so the repository filter has to be selective. It uses a bitmap of
`ingestion_scopes_source_idx` and `ingestion_scopes_active_generation_idx`.

| Read | Rows | Execution | Buffers |
| --- | --- | --- | --- |
| Page query, cold | 256 of 3,000 | 65.6 ms | hit 8,816, read 47 |
| Page query, warm | 256 of 3,000 | 55.2 ms | hit 8,863 |
| Repeat-pass probe | 0 | 0.013 ms | hit 2 |

There is no before value for the page query, because it is new. The baseline is an
activation without the section, which still reads nothing. Rotation visits all candidates in `ceil(N / 256)` buckets and
the live test proves each is visited once. Fleet cost is bounded by that rotation,
not by fleet size per pass.

## Retracting evidence after a lost go.mod

A repository is planned on `has_go_module OR has_go_evidence`, so one that lost its
`go.mod` is scanned once more and lands a trusted `no_go_modules` generation. Cost:
one extra shallow clone per lost `go.mod`, then nothing.

Performance Evidence: each candidate adds a primary-key probe on `ingestion_scopes`,
and those with an active generation add a prefix probe on
`fact_records_scope_generation_idx`. Measured with `EXPLAIN (ANALYZE, BUFFERS)` on
disposable Postgres 16, `shared_buffers=256MB`, load average 7.18, after a restart.

Input shape: 3,000 repository scopes, 100,000 other scopes, 820,060 content files,
and about 2M facts (the 1,840,060 above plus 81 go-reachability scopes holding about
204,000). One page of 256 candidates: 51 live Go repositories, 10 that lost their
`go.mod`, 10 retracted with evidence left on a superseded generation, 1 with 200,000
warnings in its active generation, and 184 with no go-reachability scope. The page
returned the expected counts: `rows=256 has_go_module=51 has_go_evidence=61
evidence_only=10 planned=61`.

| Query | Run | Execution | Buffers |
| --- | --- | --- | --- |
| Before, no evidence branch | warm | 95.4 ms, 96.2 ms | hit 128,393 |
| After, with `has_go_evidence` | warm | 98.2 ms, 97.3 ms | hit 129,543 |
| After, with `has_go_evidence` | cold | 153.2 ms | hit 115,103, read 14,446 |

The branch adds about 2 ms and 1,150 buffers to a page. The fact probe ran 72 times,
once per candidate with an active go-reachability scope. `fact_kind` is in its index
condition, and all 72 probes together cost 310 buffers, so the 200,000 warnings are
not scanned.

Do not compare these with the 66 ms and 55 ms above. Those came from an earlier run
at `shared_buffers=512MB`, whose `content_files` index scan made no heap fetches.
This run made 603,551, which likely accounts for most of the difference, probably
because the visibility map was not yet set after seeding. That cause is inferred, not
tested. Compare before and after within this run only.

Proof (orchestrator-run, disposable Postgres 16 and again on Postgres 18): the live
suite exits 0 with 6 live tests and 1 unit test passing. Four mutations of `has_go_evidence` were
caught: warnings counted as evidence, either evidence kind dropped, and the
evidence-only count including live repositories. Before the final rebases, with the
database up, these were also caught: either branch of the `OR` removed, evidence read
from any generation, tombstones counted as evidence, and the scope literal drifting.

## Cadence regression caught before review

An activation has no top-level `scan_interval`, so `Service.scanInterval` returns
the global reconcile interval for it, 30 s by default. The first build bucketed on
that. It would have planned a new page every tick, so a fleet under 256 repositories
would re-run `govulncheck` about every 30 s after each run finished. The derive
section now owns the cadence, with a 24 h default.
`TestGoModulesDerivationDefaultsToADailyCadence` failed on the first build with
`pass 1 left the 24h bucket`, and passes now.

## Concurrency

The derivation adds no lock, claim, or lease path. Admission is the existing
`CreateRunWithWorkItemsIfNoOpenTargets`, with its per-instance advisory lock and
open-target guard, which `TestGoModulePageAdmissionIsIdempotentLive` runs against
real Postgres.

One narrow race remains. Two coordinators inside one bucket both read "not planned".
The second upserts the run only if its page differs, which needs a repository to join
or leave within those milliseconds. An open item whose entry was removed then fails
its claim terminal as `invalid_claim`, and the repository returns on its next visit.
It was reasoned from the admission code, not reproduced.

## Signals

Observability Evidence: the read and its planning report on these signals.

- Counter `eshu_dp_workflow_coordinator_activation_derived_scopes_planned_total` with
  `collector_kind="scanner_worker"`, `ecosystem="go"`, and the instrument's existing
  `collector_instance_id`. It reuses the #9311 instrument, so the series shape stays
  consistent. No repository id or URL is a label:
  `TestGoModulesDerivationTelemetryHoldsNoRepositoryValue`.
- INFO `workflow coordinator planned derived scopes for component activation` with
  planned and enqueued counts, `candidates`, `total_candidates`, `slot`, `slot_count`,
  `evidence_only_repositories`, and five `skipped_<reason>` counts.
- ERROR `workflow coordinator refused derived scopes for component activation` for a
  mislabeled host, with `reason=host_scope_kind_or_source_system_mismatch`.
- `eshu_dp_postgres_query_duration_seconds{store="go_module_repositories"}` for the read.
- Claim failures use the existing `eshu_dp_component_extension_results_total`:
  `invalid_claim` for an unusable run entry and `auth_denied` for an unmapped owner.

At 3 AM: a flat counter with `candidates` above zero and `go_module_repositories` at
zero means this page holds no Go repository, and a later slot may. A non-zero
`skipped_unsupported_remote` means repositories with no remote, or a remote that is
not in canonical form. A non-zero `skipped_unsupported_remote_with_evidence` is an
invariant alarm: a stored remote keeps one form for a scope's life, so identity
normalization or the remote stamp changed, and that is a bug to root-cause. A dead or
revoked remote does not show here: it is claimed, its claim fails, and readiness shows
`collector_generation_dead_lettered` and `dead_lettered_work` (#10055). `slot_count` times `scan_interval` is the rotation period.

## NOT_CHECKED

No live scanner component claimed this work: the image does not exist yet. The
delivered `host_derived_target` shape is proven at the host boundary only.
