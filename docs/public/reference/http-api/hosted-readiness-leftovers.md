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
| `state` | `fail` while any exist or when the detection read fails; `unknown` when it gave no verdict (see below); else `pass` |
| `failure_class` | `legacy_workload_nodes`, or `legacy_workload_detection_unavailable` when the read failed |
| `detail` | `<N> name-only Workload nodes with a scoped twin (pre-#5385 leftovers)`, or `no name-only Workload nodes with a scoped twin` |
| `next_diagnostic` | `POST /admin/retire-legacy-workloads {"dry_run":true}` on the reducer admin port, and the retirement runbook |

A leftover whose name or repository id has leading or trailing whitespace is
judged against its trimmed twin and IS counted while that twin exists, so the
check fails until it is retired (#8903). Trimming covers ASCII whitespace
(space proven live; tab, LF, CR equivalent by probe); no-break space is out of
scope. A padded repository id leaves the name clean, so that node still matches
a by-name selector and makes the name ambiguous. A name padded only at the
start matches only its exact handle (`workload: svc`); a name padded at the
end, or at both ends, matches no selector, because selectors are trimmed on
entry.

The count is a lower bound (`N+`) if the read hit its examined cap of 5,000
candidates. The read is the same detection the retirement dry run uses, so the
two share one classification of each node. The numbers can still differ: this
count includes nodes a relationship blocks from retirement, and a dry run
examines at most 1,000 nodes and reports only the eligible ones as
`eligible`.

- **Read failure.** The check runs only after `query_readback` passed, so a
  failed detection read is its own condition. The check fails closed with
  `legacy_workload_detection_unavailable`, readiness is `not_ready`, the API
  logs the WARN line `hosted readiness legacy workload detection failed`, and
  the request span records the error. The body never carries the error text.
- **`unknown`, no failure class.** The detection is validated on Neo4j only.
  On any other graph backend (including an unset `ESHU_GRAPH_BACKEND`) it does
  not run and the check is `unknown`, never `pass`. The check is also `unknown`
  when the read examined its cap and found no leftover, because the nodes past
  the cap were not examined.

To clear it, follow the
[leftover retirement runbook](../../operate/legacy-workload-retirement-runbook.md).
