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
served both production snapshot-set paths after the PR review fix. After two
warmups per path, eight
interleaved `legacy, singleton, singleton, legacy` blocks measured the time
from snapshot-set begin through close. Every call succeeded, and the primary
WAL insert LSN was unchanged throughout the run. Each path had 16 samples:

| Path | Ordered samples in milliseconds | Median |
| --- | --- | ---: |
| Legacy single-host | 1.504776, 1.416369, 1.225845, 1.285367, 1.256645, 1.090634, 1.175259, 1.192888, 1.018590, 1.222353, 1.112917, 1.017374, 0.851068, 0.994530, 1.025416, 1.026072 | 1.144088 ms |
| Singleton fleet | 1.515644, 1.437424, 1.518375, 1.517710, 1.569395, 1.548240, 1.378988, 1.499060, 1.319000, 1.209196, 1.231332, 1.257339, 1.167514, 1.227753, 1.255433, 1.354932 | 1.366960 ms |

This is a tiny, empty-database setup microbenchmark. The singleton median was
0.222872 ms higher on this fixture. That narrow setup delta is **not** a
16-term endpoint comparison, throughput test, or ops-qa `<1 s` acceptance
result. The earlier isolated
ops-qa single-reader canary had a 0.6740565 s descriptive median, but its
streaming corpus and unequal live/canary traffic prevent a causal speedup
claim. The deployed Service's last recorded median remained 1.184 s.

The post-review fixture used `postgres:18.3` with direct host-network loopback
ports, a dedicated replication slot, and primary and standby on one host. The earlier
one-off runner had SHA-256 `f1cf425fd77fb182546cfaffd09e074e2cd2e9459eb33cd09cd847debe3cdc31`;
that runner and its containers and volumes were removed. The post-review fixture
was created with bounded Docker commands and is removed after rerunning gates.
The relevant code SHA-256 values are `21cc87eb03a3ef51815f3ac8a1c2f26f72b2ff7cb133ab781b70299b6d805a54`
for `config.go`, `617832ce95a37b8f7c48b05b28bc0e9d2adf3a1b927af3657aab166bd92c5d1e`
for `reader_members.go`, and
`4922be08072bc7422e587b71c517e8ea20ce003fbe39fc2a90e6fa1547df9965`
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
