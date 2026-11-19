# Live PostgreSQL readiness calibration

Issue #9100 uses a separate, manual GitHub Actions workflow to measure one
reviewed eight-shard schedule. This is source-bound calibration collection only; the existing weight refitter
does not consume these eight-shard dispatch results. A separate reviewed fit is
needed before any acceptance change. The existing
readiness acceptance workflow, its 15% spread guard, 1440-second modeled job
limit, 608-second row limit (#9469; 675 seconds before), and 30-minute hosted
job timeout stay in force.

The fixed input is
`specs/live-postgres-readiness-calibration.v1.json`. Its 38 runner rows cover
1093 unique tests in canonical inventory order. The committed row identity SHA-256
is `96bd1d703608238b206964d69dc44179031c7f44869bcd31bb8a47e8b6ab6038`.
There is no separate proposal file this round: the PR-reviewed manifest is the
reviewed source, so the reviewed-source pin equals the rows digest. The runner
uses these exact rows; it does not
recompute cuts or split a row from the current cost model. Inventory drift fails
before any proof starts.

The offline proposal has **not run on hosted runners**. Its planner cut,
based on acceptance run `44446666633` at source SHA
`52b0d1cf12cb5fc2a2b222106d5999b823121337`, prices the eight jobs at 826 to
873 s against the 1440-second modeled job limit at 3.98% spread. A hosted
run can exceed either that estimate or the 30-minute job timeout. Until the
dispatched pair lands, these runs cannot be called acceptance
proof or used to weaken the existing gate.

## Frozen inventory and drift

The manifest is a snapshot of the readiness inventory at the source SHA above;
its `source_inventory_sha256` pins that inventory independently of the rows.
The live inventory keeps growing as live tests are enrolled, so the manifest
will not match it for long. That is deliberate: `load()` refuses a dispatch
whose live inventory differs from the snapshot ("ordered coverage differs from
inventory"), because evidence from a stale topology would not describe the plan
under review. A new dispatch therefore needs a reviewed re-freeze of the
manifest, its pins, and the bounds above.
The manifest stays frozen on purpose until then. Frozen is the state until
one of two triggers fires (tracked in #9585): re-freeze when a #9532 reopen
trigger fires or a shard-count change is proposed (re-freeze at the current
inventory by the #9460 procedure in one reviewed PR), or retire when the #9532
monitor closes clean (30 post-refit jobs, no trigger) and #9460 agrees (one PR
deletes the workflow, tooling, manifest, test, gate rows, and docs).

`scripts/test-live-postgres-readiness-calibration.py` checks the snapshot
against its own pin, checks that a drifted inventory is refused, and checks the
report and provenance readers. It does not compare the snapshot with the live
inventory, so an enrollment does not fail it. It runs in the hermetic readiness
job and in the `live-postgres-readiness-shard-mirror` gate, selected by changes
to the calibration library, manifest, workflow, and test. The provenance reader
imports the v2 refresher, so a change to that module now runs this test too.

## Capture and read evidence

After the workflow is on `main`, dispatch
`.github/workflows/live-postgres-readiness-calibration.yml` twice on the same
source commit. Do not rerun a single run ID as a replacement for a second run.
Save each run under a directory named by its numeric run ID, with these files:

- `run.json`: full GitHub Actions run API response.
- `jobs.json`: complete, unpaginated jobs API response including all nine jobs
  and steps.
- `shard1.log` through `shard8.log`: full job logs for the corresponding
  calibration shard job IDs.
- `shard1.json` through `shard8.json`: downloaded
  `calibration-shard-RUN_ID-ATTEMPT-N` artifact reports.

For each ID, use `gh api repos/eshu-hq/eshu/actions/runs/ID` for `run.json` and
`gh api 'repos/eshu-hq/eshu/actions/runs/ID/jobs?per_page=100'` for `jobs.json`.
Use each shard job's ID from that response to retrieve its complete job log with
`gh api repos/eshu-hq/eshu/actions/jobs/JOB_ID/logs`. Download each named report
with `gh run download ID --name calibration-shard-ID-ATTEMPT-N --dir TARGET`
and place its `calibration-shard-report.json` at `shardN.json`.

Read the pair with:

```sh
python3 scripts/lib/live_postgres_readiness_calibration_provenance.py \
  --output calibration-evidence.json RUN_ID_1 RUN_ID_2
```

The reader requires two distinct successful `workflow_dispatch` runs from the
calibration workflow on the same source SHA. It binds source workflow, harness,
and manifest hashes, job and attempt IDs, runner and backend profiles, each
shard report and log, exact ordered tests and row hashes, and first-row,
first-later, and repeat measurements. A partial, failed, mismatched, or duplicate
run is rejected. The output remains labeled `CALIBRATION_ONLY_NOT_ACCEPTANCE`;
changing production acceptance requires a separate reviewed decision and PR.
