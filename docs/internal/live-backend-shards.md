# Live backend shard execution

The live-test ledger remains the source of truth for CI coverage. The local
command `bash scripts/run-live-backend-tests.sh --backend both` runs its full
plan sequentially. Every file receives a fresh database, and successful teardown
requires the owned Compose containers and volumes to be absent.

Hosted CI uses four isolated runner jobs: shard `1/4` owns all NornicDB tuples;
shards `2/4`, `3/4`, and `4/4` split Neo4j's canonical order into contiguous
chunks of `ceil(Neo4j tuples / 3)`. Backend pins and complete test expressions
come from the ledger extractor. New rows enter the partition automatically.
An empty selected shard fails instead of certifying missing coverage.

For an individual shard, run:

```bash
bash scripts/run-live-backend-tests.sh --shard 2/4 --results shard-2.json
```

Results require managed Compose lifecycle: `--keep` and `--no-compose` cannot
produce acceptance reports. Local shards remain serialized by the existing live
gate lock; changing the project name does not make shared host ports concurrent.
Each hosted job sets its own project name on its isolated runner.

The blocking check remains `live-backend (nornicdb and neo4j)`. Its aggregate
requires successful queue selection. Selected executions require every shard
job to succeed and download only artifacts named for their current workflow run
and attempt, then
rederive the full plan from that checkout. The verifier rejects wrong source
SHAs, missing or duplicate shards, altered test expressions, missing terminal
events, and nonzero test statuses. An idle queue selection skips the matrix
before allocating runners; the aggregate accepts skipped shards only with
successful, explicitly idle selection.

Logs identify the source SHA, shard, planned count, and each target's setup,
test, cleanup, and exit status. JSON reports retain the same terminal records.
Phase seconds are diagnostic observations; they do not establish a causal
speedup or an observed parallel critical path. Measure hosted overlap and full
gate latency separately from summed runner work before making those claims.
