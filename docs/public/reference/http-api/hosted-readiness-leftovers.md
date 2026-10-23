# Hosted Readiness: Leftover Workload Nodes

`GET /api/v0/status/hosted-readiness` (and the MCP tool `get_hosted_readiness`)
carries the check `workload_identity_leftovers`.

It fails, with failure class `legacy_workload_nodes`, while name-only `Workload`
nodes (`workload:<name>`) that have a scoped twin remain from before the
repository-scoped identity change (#5385). Each such node makes a by-name
service lookup ambiguous. The check counts nodes that:

- have the exact legacy id `workload:<name>` and no `generation_id`,
- are referenced by no `WorkloadInstance` (by edge or by `workload_id`), and
- have a scoped twin: the same name and `repo_id`, with a `generation_id`, that
  the same repository `DEFINES`.

Fields on the check:

| field | value |
| --- | --- |
| `name` | `workload_identity_leftovers` |
| `state` | `fail` while any exist, else `pass` (readiness has no warn state) |
| `failure_class` | `legacy_workload_nodes` |
| `detail` | `<N> name-only Workload nodes with a scoped twin (pre-#5385 leftovers)`, or `no name-only Workload nodes with a scoped twin` |
| `next_diagnostic` | `POST /admin/retire-legacy-workloads {"dry_run":true}` on the reducer admin port, and the retirement runbook |

The count is a lower bound (`N+`) if the read hit its examined cap. When the
bounded graph read fails the check is omitted rather than failed, because
`query_readback` already reports an unreadable graph. The read is the same
detection the retirement dry run uses, so the two cannot disagree.

To clear it, follow the
[leftover retirement runbook](../../operate/legacy-workload-retirement-runbook.md).
