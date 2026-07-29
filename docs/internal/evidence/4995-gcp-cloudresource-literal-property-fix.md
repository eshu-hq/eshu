# GCP CloudResource Literal row.<key> Property Fix Evidence (#4995)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## GCP CloudResource literal `row.<key>` property fix (#4995)

`gcpCloudResourceNodeRow` (`gcp_resource_materialization.go`) previously
omitted all 7 anchor/identity keys — `workload_id`, `service_name`,
`service_anchor_status`, `service_anchor_source`, `service_anchor_reason`,
`service_anchor_names`, and `service_anchor_name_tokens` — from every GCP
`CloudResource` row map, mirroring how the AWS row builder
(`aws_resource_service_anchor.go`) omits the same keys when
`cloudResourceServiceAnchorFields` finds no service-anchor decision.
`canonicalCloudResourceUpsertCypher`'s shared `SET` clause
(`go/internal/storage/cypher/cloud_resource_node_writer.go`) unconditionally
reads all 7 `row.<key>` references for every batch row. An audit of every
`row.<key>` reference in that 23-clause `SET` statement plus the `MERGE`
anchor confirms these 7 were the complete set of keys the statement reads but
the GCP row previously omitted; `evidence_source` is injected onto every row by
`WriteCloudResourceNodes`, and all remaining keys were already populated.

Proved-The-Theory-First: the pinned NornicDB backend
(`timothyswt/nornicdb-cpu-bge:v1.1.9`, the default in `docker-compose.yaml`)
does not evaluate a missing `UNWIND` row map key as `null` in a `SET` clause.
Against an isolated, uniquely-named Compose project
(`-p eshu-4995-probe`, host ports 17474/17687), `UNWIND $rows AS row MERGE
(n:Probe {uid: row.uid}) SET n.missing = row.workload_id` with
`rows=[{"uid":"x2"}]` persisted `n.missing` as the literal string
`"{uid: 'x2'}.workload_id"` — a stringified dump of the bound `row` map plus
the missing property name — never `null`. Reproduced with the exact
production statement text (all 23 `SET` clauses) and a multi-key row: every
missing key stringified the same way. A positive control (`row.workload_id`
present as `"svc-alpha"`) confirmed the correct value is set normally when the
key exists, isolating the defect to the missing-key path specifically. GCP has
no service-anchor source today, so it always hit the missing-key path on every
node.

This is not only a raw-property defect. Two downstream consumers acted on the
corrupted literals:

- `service_anchor_reason`'s corrupted literal was non-empty, so
  `serviceTraceCloudDependencyCandidateMissingEvidence`
  (`go/internal/query/service_story_trace_path.go`) would surface it verbatim
  as the "missing evidence" reason text in service-story trace output for any
  GCP `CloudResource` candidate, instead of falling through to the correct
  `candidate_status`-derived reason.
- `service_name`'s corrupted literal was already masked in the API read by a
  targeted `if serviceName == "row.service_name" { serviceName = "" }` drop in
  `cloudResourceRowFromGraph` (`go/internal/query/cloud_resources.go`) — but
  that only cleaned the read; the corrupted `"row.service_name"`-shaped literal
  was still persisted to the graph node itself.

`cloudResourceCandidateStatus`'s `service_anchor_status` switch
(`go/internal/query/cloud_resource_candidates.go`) already fell through to its
`default` branch for both the corrupted literal and a real empty string, so
that specific classification was unaffected.

Fix: `gcpCloudResourceNodeRow` now sets all 7 keys explicitly (`""` for the 6
scalar keys — `workload_id`, `service_name`, and the 4 scalar
`service_anchor_*` keys — and `[]string{}` for `service_anchor_names`) —
present keys with the correct GCP no-anchor parity value, never omitted ones.
The shared Cypher, the AWS row builder, and the API-side `service_name`
placeholder drop are all unchanged.

Known latent parity gap (tracked separately, not fixed here): the AWS row
builder (`cloudResourceServiceAnchorFields` in
`aws_resource_service_anchor.go`) omits these same 7 keys whenever
`decision.Status == ""` (a resource with no service-anchor decision), so an AWS
`CloudResource` with no explicit anchor tag hits the identical
missing-key-stringifies defect. In practice AWS resources commonly carry an
explicit anchor tag, which is why this surfaced as a GCP-only symptom; the
AWS/writer-level fix is a follow-up outside this change's scope.

No-Regression Evidence:
`go test ./internal/reducer -run TestExtractGCPCloudResourceNodeRowsSetsExplicitServiceAnchorParityKeys -count=1`
proves all 7 keys are present (not merely equal to `""`, which a missing key
would also satisfy via `anyToString(nil) == ""`) with the correct empty
values; the test was confirmed red for each missing key and green with all 7
set. `go test ./internal/reducer ./internal/storage/cypher -count=1` covers the
full reducer and shared-writer suites with no regressions. The before/after
NornicDB probe above was re-run against the fixed row shape end-to-end through
the real `canonicalCloudResourceUpsertCypher` statement, confirming
`r.workload_id`, `r.service_anchor_status`, `r.service_anchor_names`, and
`r.service_anchor_name_tokens` persist as `""`, `""`, `[]`, and `""`
respectively instead of the stringified-row literal.

No-Observability-Change: no metric, span, log field, or status field changes.
This is a row-shape correction in an existing materialization path; the
existing `gcp resource materialization completed` log and
`eshu_dp_reducer_*`/materialization-duration instruments are unaffected.
