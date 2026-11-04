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
  and four `skipped_<reason>` counts.
- ERROR `workflow coordinator refused derived scopes for component activation` for a
  mislabeled host, with `reason=host_scope_kind_or_source_system_mismatch`.
- `eshu_dp_postgres_query_duration_seconds{store="go_module_repositories"}` for the read.
- Claim failures use the existing `eshu_dp_component_extension_results_total`:
  `invalid_claim` for an unusable run entry and `auth_denied` for an unmapped owner.

At 3 AM: a flat counter with `candidates` above zero and `go_module_repositories` at
zero means this page holds no Go repository, and a later slot may. A non-zero
`skipped_unsupported_remote` means repositories whose remotes the component cannot
fetch. `slot_count` times `scan_interval` is the rotation period.

## NOT_CHECKED

No live scanner component claimed this work: the image does not exist yet. The
delivered `host_derived_target` shape is proven at the host boundary only.
