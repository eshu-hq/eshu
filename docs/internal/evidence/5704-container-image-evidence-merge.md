# #5704 container-image evidence merge

## Result

Digest-v3 container-image publication now keeps every independent current
support for a digest. An AWS runtime observation and a CI artifact for the same
digest therefore remain separate durable supports, while the query surface
folds them into one canonical identity with the sorted union of their evidence.

The legacy fact writer is unchanged. Its historical single-winner behavior is
pinned by a renamed regression test so this fix cannot silently change the
pre-cutover contract.

The canonical query keeps metadata selection and authorization unchanged. It
now selects `identity_strength` independently with this explicit order:

1. `explicit_digest`
2. `oci_config_source_label_with_digest`
3. `artifact_digest_with_registry_observation`
4. `immutable_digest`
5. `tag_observation_with_digest`
6. unknown values, ordered lexically and then by support ID

This prevents the support that wins repository or image-reference metadata
from accidentally weakening the digest-level evidence conclusion.

## Root cause and TDD proof

The production digest-v3 support-set builder reused the legacy publication
planner. That planner intentionally selects one winner for each logical legacy
fact key, so two valid same-digest decisions were collapsed before the durable
support set was written.

The regression was reproduced first at the production support-set seam:

- runtime `explicit_digest` plus CI
  `artifact_digest_with_registry_observation` produced one support instead of
  two;
- exact-digest plus tag-resolved support for the same digest also produced one
  support instead of two; and
- a true semantic duplicate still produced one support, as required.

After the fix, all three cases pass. Support identity is derived from the full
normalized semantic payload, so replay order cannot change the set ID or row
order. Exact semantic duplicates remain idempotent; evidence-distinct supports
remain distinct.

## Query, authorization, and deployed-schema proof

The real-Postgres test seeds two active scopes and multiple supports for one
digest. It proves all of the following through the production store and the
deployed SQL function:

- one canonical identity is returned;
- source repositories and evidence fact IDs are unioned and sorted;
- the strongest identity strength is `explicit_digest`;
- the prior canonical metadata winner remains unchanged;
- authorization filters supports before any union or strength fold, so one
  authorized scope cannot reveal another scope's evidence; and
- aggregate and inventory queries use the same canonical strength and logical
  identity count.

Migration 097 replaces only
`container_image_identity_current_facts_for(...)`. Schema-order, migration
content, and live migration tests pin the deployed definition to the Go query
contract.

## Performance evidence

Performance Evidence: the reducer benchmark, PostgreSQL 16 plan comparison,
and full B-7 measurements below use fixed input shapes and terminal row or
queue counts to compare the evidence-preserving path with its exact baseline.

### Support-set builder

The same 1,000 logical digests, each with one runtime and one CI decision, were
run five times against exact pre-change baseline commit `06add5ff9` and this
change. The old path emitted 1,000 supports because it dropped one decision per
digest; the fixed path emits the correct 2,000 supports.

| variant | output supports | median | bytes/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| old collapsing path | 1,000 | 11.960 ms | 13,979,706 | 81,523 |
| evidence-preserving path | 2,000 | 8.424 ms | 21,701,596 | 57,384 |

The corrected path is 29.6% faster and performs 29.6% fewer allocations while
returning twice as many durable rows. Absolute bytes rise 55.2% because the
missing half of the truth now exists; bytes per returned support fall from
about 13.98 KB to 10.85 KB.

Command:

```bash
cd go
go test ./internal/reducer -run '^$' \
  -bench '^BenchmarkBuildContainerImageIdentitySupportSet/(current_1000|converged_1000_2000)$' \
  -benchmem -count=5
```

The command above produces the changed-branch rows. The baseline used exact
`main` commit `06add5ff9d7b013ce86929d721eca234b0925243` in a clean auxiliary
worktree. To give the old implementation the identical input without changing
production source, the benchmark test alone was temporarily patched to build
the same 1,000 runtime plus 1,000 CI decisions and add the
`converged_1000_2000` case with an expected legacy output of 1,000 supports.
The same command was run five times there. The temporary test patch was then
removed and `git status --short` returned empty. This distinguishes the old
production builder from the final builder while keeping corpus, decision
payloads, benchmark loop, and machine constant.

### PostgreSQL canonical fold

The old migration-092 function and migration 097 were applied in turn to the
same PostgreSQL 16 database containing 2,000 supports for one selected digest.
Five warmed `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` samples used the same
arguments and storage state.

| function | median execution | shared hits | result rows |
| --- | ---: | ---: | ---: |
| old incidental-strength winner | 26.748 ms | 355 | 1 |
| independent strength fold | 26.960 ms | 355 | 1 |

The measured difference is +0.212 ms (+0.8%), with identical buffer work and
row count. The final plan materializes 2,000 selected supports, ranks strength
once, and returns one folded identity. This is below the repository's
investigation threshold and introduces no extra query round trip.

## Golden acceptance proof

The CI/CD cassette now contains a synthetic run and artifact for the digest
already observed by the AWS and OCI fixtures. B-12 requires exactly one HTTP
identity with:

- the exact sorted union of AWS image-reference, OCI manifest, CI run, and CI
  artifact fact IDs;
- `identity_strength = explicit_digest`;
- the CI source repository; and
- the CI revision and its provenance.

The static BITES test rejects a dropped AWS fact, weakened strength, or
duplicate identity. Before recapturing the stale phase baseline, the complete
B-7 pipeline reported:

```text
summary: 524 pass, 0 required-fail, 1 advisory-warn
PASS: B-7 golden corpus gate green (elapsed 143s, budget ceiling 1800s)
```

The advisory was the existing maintenance-drain timing signal: 26 seconds
observed against its 19-second advisory ceiling. To separate branch cost from a
stale baseline, the full gate then ran sequentially in two clean worktrees on
the same host. Exact then-current `main` at `06add5ff9` measured 25 seconds and
reproduced the warning before the #5704 fixture existed. Current-main evidence
in #5770 also records 25 seconds, so the committed 14-second seed no longer
represented the enforcement host's steady state.

Only `maintenance_drains.baseline_seconds` was recaptured to current main's
25-second observation. The branch's 32-second sample was not used. The updated
policy permits 30 seconds through the repository's existing +5-second absolute
slack.

After #5936 moved `main`, the same full comparison was repeated on the rebased
diff. Exact current `main` at
`9f1493348a8b1164c33b56394a9a8a740edd9352` measured 28 seconds and again
warned against the old 14-second seed. The immediately following branch run
measured 23 seconds under the recaptured policy and reported:

```text
phase_maintenance_drains: observed=23.0s, baseline=25.0s, ceiling=30.0s
summary: 525 pass, 0 required-fail, 0 advisory-warn
PASS: B-7 golden corpus gate green (elapsed 141s, budget ceiling 1800s)
```

All required queues were terminal, there were no dead letters, and the complete
graph, HTTP, and MCP truth gates passed.

Primary commands:

```bash
cd go
go test ./internal/reducer -run 'ContainerImageIdentity' -count=1
go test ./internal/query -run '^TestContainerImageIdentity' -count=1
go test ./internal/storage/postgres \
  -run 'ContainerImageIdentity.*(Schema|Migration|Order)' -count=1
ESHU_POSTGRES_TEST_DSN='postgresql://eshu:change-me@localhost:25432/eshu?sslmode=disable' \
  go test ./internal/query \
  -run '^TestContainerImageIdentityV3CanonicalReadPostgresLive$' -count=1
cd ..
bash scripts/test-verify-golden-corpus-gate.sh
bash scripts/verify-golden-corpus-gate.sh
```

## Concurrency and observability

No worker count, claim, lease, retry, queue ordering, lock, or transaction
boundary changes. Publication still installs one immutable support set and
atomically switches the scope's active-set pointer. Stable semantic support IDs
make duplicate delivery and reordered replay converge without serialization.

No-Observability-Change: existing container-image decision and retirement
counters continue to describe reducer outcomes, while reducer duration,
Postgres query duration, terminal queue state, and B-7 phase timing continue to
surface failures and regressions. The change adds no runtime knob, metric label,
or silent fallback.
