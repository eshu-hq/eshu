# Live PostgreSQL readiness calibration

Issue #9100 uses a separate, manual GitHub Actions workflow to measure one
reviewed six-shard schedule. This is source-bound calibration collection only; the existing weight refitter
does not consume these six-shard dispatch results. A separate reviewed fit is
needed before any acceptance change. The existing
readiness acceptance workflow, its 15% spread guard, 1440-second modeled job
limit, 608-second row limit (#9469; 675 seconds before), and 30-minute hosted
job timeout stay in force.

The fixed input is
`specs/live-postgres-readiness-calibration.v1.json`. Its 34 runner rows cover
702 unique tests in canonical inventory order. The committed row identity SHA-256
is `a203de14ef00ae93c0ccb274b65460a9b0e8680e0c0d25f47316ba1d49707a0c`.
The reviewed source manifest had SHA-256
`f00e9933ca1c2a913c42c667e080f8542118fa4acc67651bc56b8092ebec5ba9`;
the committed JSON has identical values with shorter formatting to stay under
the repository's 500-line limit. The runner uses these exact rows; it does not
recompute cuts or split a row from the current cost model. Inventory drift fails
before any proof starts.

The offline proposal has **not run on hosted runners**. Its observed-overlay
model, based on acceptance run `40784513987` at source SHA
`2e3ba9fc40ec89a552e28ac25038646571df8b4e`, left only 20.8 seconds
below the 1440-second modeled job limit. A hosted
run can exceed either that estimate or the 30-minute job timeout. The six-shard
committed-model spread is above 15%, so these runs cannot be called acceptance
proof or used to weaken the existing gate.

## Capture and read evidence

After the workflow is on `main`, dispatch
`.github/workflows/live-postgres-readiness-calibration.yml` twice on the same
source commit. Do not rerun a single run ID as a replacement for a second run.
Save each run under a directory named by its numeric run ID, with these files:

- `run.json`: full GitHub Actions run API response.
- `jobs.json`: complete, unpaginated jobs API response including all seven jobs
  and steps.
- `shard1.log` through `shard6.log`: full job logs for the corresponding
  calibration shard job IDs.
- `shard1.json` through `shard6.json`: downloaded
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
