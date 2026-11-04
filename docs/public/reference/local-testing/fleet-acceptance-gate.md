# Fleet Acceptance Gate

The fleet acceptance gate (#9875) checks that an exploited CVE on a deployed
service shows up in the service rollup, with the right priority tier, within a
time bound. It runs against a disposable PostgreSQL 18 database and needs no QA
environment.

## What it proves

The gate seeds one run of the pipeline:

- an advisory (`vulnerability.cve` with CVSS 7.5, plus `vulnerability.affected_package`
  for `pkg:npm/fleet-9875@1.0.0` at `1.0.0`);
- a KEV listing (`vulnerability.known_exploited`) in its own scope;
- a repository that consumes the package at the affected version and has a
  service/workload catalog mapping to `service:fleet-9875`;
- a second consuming repository with no service mapping, as the control.

It then drives the production path:

1. The real `SupplyChainImpactHandler` claims and handles every work item from
   the reducer queue, including the fan-out reruns, until the queue is empty.
2. The winners table gets a full resweep before the seed, as the reducer does
   at startup. After the drain, the keyed winners tick runs, which is the
   steady-state maintenance path.
3. The mounted supply-chain routes answer
   `GET /api/v0/supply-chain/impact/inventory?group_by=service_id` and
   `group_by=priority_bucket&service_id=service:fleet-9875`.

A run passes only when all of these hold:

| Check | Violation code |
| --- | --- |
| The service rollup answers 200 | `rollup_unavailable` |
| `service:fleet-9875` has a bucket, and the bucket is `complete` | `service_absent`, `service_completeness` |
| The bucket counts the finding as `affected_exact` | `service_impact_status` |
| The bucket's `max_priority_score` equals the winning finding's score | `service_priority` |
| The winning finding is `known_exploited` and carries the `cisa_kev` reason | `kev_missing` |
| The reducer attributed the winning finding to the same service | `winner_service` |
| The finding and the service's tier breakdown are `critical` | `priority_tier` |
| The control finding is in the unattributed bucket, with reason `service_mapping` | `unattributed_bucket` |
| `distinct_findings` is 2 | `distinct_findings` |
| The stage stamps are present and in order | `stage_order` |
| Source commit to rollup read is within the bound | `elapsed_bound` |

The tier depends on both signals. With the KEV listing and the deployment
evidence, the finding scores 100 (`critical` starts at 95). Without the KEV
listing it scores 80, and without the service mapping it scores 90. Both of
those are `high`.

## Run it

Start a disposable PostgreSQL 18 server. The DSN must name the `postgres`
database. The test creates its own database and drops it afterward.

```bash
docker run -d --name eshu-fleet-gate-pg -e POSTGRES_USER=eshu -e POSTGRES_PASSWORD=pw \
  -p 127.0.0.1:35439:5432 postgres:18
cd go
ESHU_FLEET_GATE_PROOF_DSN='postgres://eshu:pw@127.0.0.1:35439/postgres?sslmode=disable' \
ESHU_FLEET_GATE_PROOF_DISPOSABLE=1 \
go test ./internal/storage/postgres/ -run '^TestFleetAcceptanceGate' -count=1 -v
```

Without the DSN the two live tests skip. The classifier's RED/GREEN pair runs
in every `go test` of the package with no database:

```bash
cd go && go test ./internal/storage/postgres/ -run 'FleetGate' -count=1
```

The live tests are enrolled in the
[live PostgreSQL readiness runner](live-postgres-readiness-shards.md) through
their `//eshu:readiness` markers. The generator adds them to the inventory, so
they need no separate CI workflow or registry entry.

## The bound

`ESHU_FLEET_GATE_MAX_ELAPSED` sets the ceiling on source commit to rollup read,
as a Go duration such as `10s`. The default is `30s`. A value that does not
parse, or is not positive, fails the test; it never falls back to the default.
`ESHU_FLEET_GATE_MAX_ELAPSED=1ms` is a quick way to see `elapsed_bound` fail.

This is a bound on the local pipeline. It is not the fleet freshness SLO: that
is the time from an upstream publish to a finding in QA, per source, which the
arbiter sets on #9875.

## The stage clock

No timestamp for source commit or finding written exists on the production
path today. The test stamps `time.Now()` after each stage returns:

| Stage | Starts at | Ends when |
| --- | --- | --- |
| `finding_written` | the seeded facts and work items commit (this stands in for the collector committing the upstream publish) | the reducer queue is empty |
| `winners_built` | the queue drains | the keyed winners tick commits |
| `rollup_read` | the tick commits | `group_by=service_id` answers |

The test logs each stage and the total. On PostgreSQL 18.6 locally, the clean
run took 1.26 s in total (finding written 1.23 s, winners built 19 ms, rollup
read 11 ms), and 1.62 s under `-race`. The drain dominates, because it handles
seven work items, including fan-out reruns.

## The RED half

`TestFleetAcceptanceGateFailsWhenEvidenceIsWithheld9875Live` withholds one fact
per run and requires the named violations:

| Withheld fact | Required violations |
| --- | --- |
| KEV listing | `kev_missing`, `priority_tier` |
| `affected_package` | `service_absent`, `winner_absent`, `unattributed_bucket` |
| Service catalog mapping | `service_absent`, `winner_service`, `unattributed_bucket` |

`TestFleetGateViolationsSeededPairs9875` plants each violation into the pure
classifier, so the pair also runs without a database.

## What it does not cover

These need QA and are not part of this gate:

- per-source time from upstream publish to finding, for OSV and KEV;
- the bound per source or for the fleet, which the arbiter sets;
- a second run on a different day, so one lucky rotation slot cannot pass it.
