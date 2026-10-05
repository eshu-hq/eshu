# #7033 singleton reader-fleet admission

The direct-member inventory now accepts one physical PostgreSQL standby as a
transitional topology. It uses the same member qualification, borrowed-session
identity check, connection reservation, and exported-snapshot path as a larger
fleet. A singleton is not highly available: losing it fails guarded reads
instead of switching to the writer. Adding another member still requires a
configuration change and API/MCP restart, not a new query route.

The separate-context architecture ruling was **GO** for one member in the
general fleet and **NO-GO** for treating that state as redundant, as 100,000-repo
capacity proof, or as #7033's deployed subsecond acceptance. It required a
direct standby endpoint, no DSN fallback, the existing physical identity
fences, whole-set retry only, and an explicit no-redundancy operator signal.

## Behavior proof

The configuration regression was red before the guard change:
`TestLoadConfigReaderMembersAreCredentialFreeAndBounded/single_member` returned
`must be a JSON array of at least two direct members`. It passed afterward;
the same table rejects an explicit empty inventory, duplicate member ID or
endpoint, embedded credentials, and insufficient pool capacity.

`TestSingleReaderMemberKeepsSnapshotAndFailsClosed` ran against an owned
PostgreSQL 18.3 primary and one physical streaming standby on the same host.
It verified one qualified member, four different backend PIDs on one exported
snapshot and server address, zero in-use leases after close, failed readiness
and snapshot setup after the frozen member epoch changed, and rejection of the
primary as a reader. The ordinary non-live runtime/PostgreSQL package test
passed. This live test uses explicit disposable-fixture DSNs and skips when
they are absent; it does not run against ops-qa.

The disposable primary initially had WAL insert `0/3000028` but WAL flush and
standby replay `0/3000000`. The first live test correctly failed its replay
fence after two seconds. A fixture-only `CHECKPOINT` brought insert, flush,
and replay to `0/30000F8`; the unchanged production fence then passed. This
setup correction is not a production performance or correctness finding.

## Narrow performance proof

Performance Evidence: A disposable PostgreSQL 18.3 primary and physical standby
served both production snapshot-set paths after the review-fix rerun. After two
warmups per path, eight
interleaved `legacy, singleton, singleton, legacy` blocks measured the time
from snapshot-set begin through close. Every call succeeded, and the primary
WAL insert LSN was unchanged throughout the run. Each path had 16 samples:

| Path | Ordered samples in milliseconds | Median |
| --- | --- | ---: |
| Legacy single-host | 1.400626, 1.200217, 1.429155, 1.002394, 1.032934, 1.139294, 1.080103, 0.905636, 0.892361, 0.883223, 0.939467, 0.934582, 0.969793, 0.914718, 0.941089, 0.910252 | 0.955441 ms |
| Singleton fleet | 1.101569, 1.060982, 1.459773, 0.988358, 0.949068, 0.938183, 0.921917, 1.016417, 0.940203, 0.924424, 0.925538, 0.902237, 0.900421, 0.903316, 0.904765, 0.916011 | 0.931860 ms |

This is a tiny, empty-database setup microbenchmark. It checks for a gross
singleton routing overhead and is **not** a 16-term endpoint comparison, a
throughput test, or an ops-qa `<1 s` acceptance result. The earlier isolated
ops-qa single-reader canary had a 0.6740565 s descriptive median, but its
streaming corpus and unequal live/canary traffic prevent a causal speedup
claim. The deployed Service's last recorded median remained 1.184 s.

The review-fix fixture used `postgres:18.3` with direct Docker bridge addresses,
a dedicated replication slot, and primary and standby on one host. The earlier
one-off runner had SHA-256 `f1cf425fd77fb182546cfaffd09e074e2cd2e9459eb33cd09cd847debe3cdc31`;
that runner and its containers and volumes were removed. The review-fix fixture
was created with bounded Docker commands and is removed after rerunning gates.
The relevant code SHA-256 values are `21cc87eb03a3ef51815f3ac8a1c2f26f72b2ff7cb133ab781b70299b6d805a54`
for `config.go`, `617832ce95a37b8f7c48b05b28bc0e9d2adf3a1b927af3657aab166bd92c5d1e`
for `reader_members.go`, and
`39b95e670ec4b1259c1c1b1fa8e4131de8d86fc7059cfbbf73e5e35df470b156`
for `singleton_test.go`.

## Deployment gate

No deployment, read-DSN change, or ops-qa database mutation follows from this
local proof. The ops-qa direct member must be checked for address, recovery
role, read-only mode, system/database identity, and postmaster epoch; the
transport must have no pgx fallback. A reviewed image/config rollout, equal
resources and traffic, interleaved canonical 16-term endpoint timings with
matching corpus and backend state, ranked-response equality, and an observed
`<1 s` median are still required to close #7033. Adding a second member and
representative 100,000-repository capacity are separate gates.

No-Observability-Change: fleet mode already emits
`eshu_dp_postgres_reader_member_qualified` by configured ordinal, member
attempts, reservations, waits, stage timings, and query-start identity. One
qualified ordinal signals no reader redundancy at startup; it does not prove
ongoing health. `/readyz` and reader-stage failures cover subsequent loss.
