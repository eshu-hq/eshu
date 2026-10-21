# #7531 Acceptance 2/3 Reference-Corpus Run Manifest (scaffold)

One manifest, both arms. Status: **BLOCKED — not launched** (gate probe
2026-10-21 below). No evidence in this file is measured; every measured value
is `TBD-at-launch`. Connection details stay operator-local (shell variables
only) and are never written here.

Acceptance mapping (issue #7531):

- Acceptance 2: `auto_explain` for the `code_drifted` stage with
  `log_min_duration >= 60s` and `log_analyze` off; entity-index `idx_scan`
  and band-table `relallvisible` before the stage; both arms from this one
  manifest.
- Acceptance 3: `upsert_fingerprints` stage sums with and without the index
  from the same run.

Last-run basis (read, not inferred):
`7254-code-fingerprint-band-entity-idx.md` "Reference corpus" section — run
dir `7254-corpus-20261002T183433Z`, 984 repos, Neo4j backend, accepted
full-corpus profile knobs unchanged, one image run in both arms back to back
(WITH = stock schema; WITHOUT = entity index dropped after `db-migrate`,
before `bootstrap-index`).

## Locks (fill at launch; a changed load-bearing field stops the run)

| Field | Locked value |
| --- | --- |
| Corpus identity + repo count | 984 repos (count verified in preflight) |
| Graph backend + image id | Neo4j; immutable image id TBD-at-launch |
| Reviewed commit | Exact head checked out on the remote, TBD-at-launch |
| Profile / topology knobs | Accepted full-corpus profile, unchanged |
| Volume state | Clean volumes at arm start |
| Retention mode | `stop-and-preserve` |
| Lane | Dedicated quiet lane, no peer stacks (2026-10-02 arms ran under peer load, load avg 55-74) |

This is a Neo4j / 984-repo experiment manifest, not a `full-896-bootstrap`
baseline promotion (backend + corpus differ; totals not comparable per the
runbook rule).

## Procedure (both arms, one image, back to back)

1. Preflight: access check; no `eshu|nornicdb|neo4j|postgres` procs; corpus
   count reads 984; quiet lane confirmed.
2. Fetch + detach the reviewed commit on the remote checkout; verify
   `git rev-parse HEAD` and a clean tree; build the image; record its id.
3. Per arm (order WITH-first or WITHOUT-first recorded in `run-summary.txt`):
   clean volumes, then `db-migrate`, then WITHOUT arm only:
   `DROP INDEX code_fingerprint_band_entity_idx` before `bootstrap-index`.
4. Postgres: `auto_explain.log_min_duration='60s'`, `log_analyze=off`.
5. Timestamped pre-stage reads: `pg_stat_user_indexes.idx_scan` for the
   entity index + `pg_class.relallvisible` for the band table.
6. `bootstrap-index`; poll `fact_work_items` to terminal
   (`succeeded == total`, `pending/in_flight/retrying/failed/dead_letter = 0`).
7. `upsert_fingerprints` stage sums + mean/p99/max from the
   `content writer stage completed` lines.
8. API/MCP/index readbacks; then `stop-and-preserve`.

## Report (per arm)

Wall seconds + human duration for every phase; final queue table;
`auto_explain` plans for `code_drifted` items >= 60s; pre/post
`idx_scan`/`relallvisible`; `upsert_fingerprints` sums, counts, and
distribution in both arms.

## Must not do

No cache drops on the shared validation host. No dropping
`code_fingerprint_band_lookup_idx` (the generic plan uses it; its effect is
unmeasured). No index, `plan_cache_mode`, SQL, or worker-serialization change
rides with this run.

## Gate probe 2026-10-21 (fresh, this lane)

- Operator-local SSH identity file: absent on this machine
  (`test -f` on the skill-configured path reads `no`).
- SSH to the operator-local validation-host target with batch mode and an
  8s connect timeout: `Connection timed out`, exit 255. Route closed from
  here, same as the 2026-10-19 scoper verdict and the 2026-10-20 lane verdict.
- Corpus presence (984 repos) is therefore NOT_CHECKED; it cannot be verified
  without host access.

Needed to launch: validation-host network route + SSH identity, held by the
repo owner/operator. Whoever holds them launches with the scope above verbatim.
Estimated duration from 2026-10-02 measurement: both arms + build + readbacks
run about 3.5-5h wall; time box 8h (>= 3h floor per the deferral record).
