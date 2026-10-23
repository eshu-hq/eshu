<!-- docs-catalog
title: Collector cutover truth checks
description: Confirms that no two producers hold authority after a collector rollback, defines the last committed generation per fact family, and lists the read-truth queries to compare.
type: operate
audience: operator
entrypoint: false
landing: false
-->

# Collector cutover truth checks

Use this page with the [collector cutover rollback runbook](collector-cutover-rollback.md)
to prove a rollback left exactly one producer in charge and the same answers
behind the read APIs. The runbook's script runs the SQL checks. You run the
read-truth queries here before the cutover and again after the rollback.

## Confirm that no two producers hold authority

The runbook script checks this at the end of every run and in `verify`. The checks are
read-only SQL in `scripts/lib/collector-cutover-rollback-dual.sql` and
`scripts/lib/collector-cutover-rollback-drain.sql`:

- The external instance holds 0 claimed work items and 0 active claims.
- The external and in-tree instances never held claims on the same `scope_id`
  during overlapping time spans. A finished claim ends at the earlier of its
  `finished_at` and `lease_expires_at`, so a late reap does not invent an
  overlap. An unfinished claim has not ended.
- Grants for the external component show `revoked=true` in
  `eshu component grants --producer <producer-id> --component-home <component-home>`.
- No extension worker pod remains.

Know the limit of the overlap check: it matches work items by `scope_id`. If the
two producers name the same source with different `scope_id` values, the check
cannot see the overlap. The cutover rehearsal must confirm that both producers
use the same scope IDs for the same source.

## Replay from the last committed generation

A generation is committed when it becomes the scope's active generation. You
can read it with:

```sql
SELECT scope_id, active_generation_id
FROM ingestion_scopes
WHERE collector_kind = '<collector-kind>';
```

and its commit time from `scope_generations.activated_at` for that
`generation_id`. Facts from a generation that never became active, such as a
failed run by the external producer, are not part of read truth, so they need
no cleanup.

Replay does not copy facts. It makes the in-tree producer observe the source
again and commit a new generation under its own instance. The coordinator
restart in step 4 triggers the first planning pass. Later passes follow
`ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL` (default 30 seconds). No other
command forces a pass; step 5 waits for the work this creates.

What a replay covers depends on the collector:

| Fact family | Collector kind | What the in-tree producer observes on replay | Window to check |
| --- | --- | --- | --- |
| `work_item.*` | `jira` | Issues updated inside the target's `updated_lookback` window | Set `updated_lookback` in the restore values to at least the time since the last in-tree committed generation. The maximum is 30 days. |
| `incident.record`, `incident.lifecycle_event`, `incident_routing.observed_pagerduty_*` | `pagerduty` | Incidents and live routing for the configured scopes, on the schedule, bootstrap, or webhook trigger | Set `incident_lookback` in the restore values to at least the time since the last in-tree committed generation. |
| `documentation_*` | `documentation` | The configured documentation sources, read again | Compare against the baseline; step 6 shows any source the replay did not reach. |

Read the collector's own configuration for the exact window setting before you
rely on this table. A window shorter than the gap means older source changes
are missing from the replayed generation, and step 6 will show it as a
difference from the baseline. That difference is correct output: widen the
window and replay again instead of accepting it.

## Compare read truth before and after

Run the same queries before the cutover and after the rollback, using the same
scope and keys. All are documented HTTP routes with an MCP equivalent. Use a
credential that can read the scopes you compare, and pass it from the
environment:

```bash
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" \
  "<service-url>/api/v0/work-items/evidence?scope_id=<scope-id>&limit=50"
```

| Fact family | HTTP route | MCP tool | Compare |
| --- | --- | --- | --- |
| `work_item.*` (record, transition, external link, project, status, field, issue type, workflow metadata, metadata warning) | `GET /api/v0/work-items/evidence?scope_id=<scope-id>&limit=50`, or `work_item_key=<key>` for one issue | `list_work_item_evidence` | The set of `work_item_key` values, each key's status and transition count, external links, and any `metadata_warning` rows. Page with `next_cursor.after_fact_id` until it is empty. |
| `incident.record`, `incident.lifecycle_event`, `incident_routing.*` | `GET /api/v0/incidents/<incident-id>/context?scope_id=<scope-id>` | `get_incident_context` | The ordered evidence path: incident, service, and routing slots. `live_routing` comes from the PagerDuty collector. `intended_routing` and `applied_routing` come from Terraform sources and state, so they must not change at all. |
| `documentation_*` | `GET /api/v0/documentation/findings/count`, `GET /api/v0/documentation/findings/inventory?group_by=status` (also `truth_level`, `freshness_state`, `finding_type`, `source_id`), and `GET /api/v0/documentation/facts?fact_kind=<fact-kind>&limit=100` | `list_documentation_findings`, `list_documentation_facts` | Finding counts per status and type, and the document and section IDs per source. |

For each family, run the queries over a fixed set of keys you chose before the
cutover: a handful of known issues, incidents, and documents, plus the counts.
Compare the answers.

These fields are expected to differ and are not a rollback failure: `fact_id`
(the host builds a new envelope, so it changes with the producer),
`generation_id`, `observed_at`, and `ingested_at`. Anything else that differs is
a finding. If the source itself changed between the baseline and the rollback,
re-capture the source's state first so you compare like with like.

The script's own `verify` compares the same families at the fact level. For each
fact kind it computes the count, a digest of the stable keys, and a digest of
the payloads over every scope's active generation, for the collector kind. It
does not use `fact_id`. `verify` is one pass over those facts, so run it against
a reader or standby when one exists.

```bash
scripts/collector-cutover-rollback.sh verify \
  --collector-kind <collector-kind> \
  --external-instance <external-instance-id> \
  --in-tree-instance <in-tree-instance-id> \
  --baseline baseline-<collector-kind>.txt
```

A mismatch exits 6 and prints the differing fact kinds. Then run the HTTP
queries above for those kinds to see which keys differ.

## Related

- [Collector cutover rollback](collector-cutover-rollback.md)
- [Collector Repository Migration Policy](../reference/collector-extraction-policy.md)
- [HTTP API reference](../reference/http-api.md)
- [MCP reference](../reference/mcp-reference.md)
