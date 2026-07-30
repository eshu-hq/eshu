# GCP CloudResource Literal row.<key> Property Fix Evidence (#4995)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. The original section's
content is unchanged; the "AWS and Azure follow-up closure (#5714/#5055)"
section below was added on `main` after the split and carried forward here.

## GCP CloudResource literal `row.<key>` property fix (#4995)

`gcpCloudResourceNodeRow` (`gcp_resource_materialization.go`) previously
omitted all 7 anchor/identity keys — `workload_id`, `service_name`,
`service_anchor_status`, `service_anchor_source`, `service_anchor_reason`,
`service_anchor_names`, and `service_anchor_name_tokens` — from every GCP
`CloudResource` row map, mirroring how the AWS row builder
(`aws_resource_service_anchor.go`) omits the same keys when
`applyCloudResourceServiceAnchorFields` finds no service-anchor decision.
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
builder (`applyCloudResourceServiceAnchorFields` in
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

### AWS and Azure follow-up closure (#5714/#5055)

The "known latent parity gap" noted above is now closed, plus a second,
independently-discovered instance:

- **AWS**: `applyCloudResourceServiceAnchorFields` (`aws_resource_service_anchor.go`)
  returned `nil, nil` whenever `cloudResourceServiceAnchorDecisionForPayload`
  found no decision (`decision.Status == ""`), and even a real decision could
  leave `workload_id`/`service_name` unset (an ambiguous decision names no
  single workload/service). It now always returns all 7 keys
  (`cloudResourceServiceAnchorFieldsAbsent` supplies the no-anchor parity
  defaults, mirroring `gcpCloudResourceNodeRow`'s and
  `runningImageFieldsAbsent`'s convention), and only overwrites the specific
  keys a real decision resolves.
- **Azure**: `azureCloudResourceNodeRow` (`azure_resource_materialization.go`)
  never received the #4995 fix at all — it omitted all 7 keys unconditionally,
  so every Azure `CloudResource` was exposed to the missing-map-key-in-UNWIND
  corruption whenever its batch was heterogeneous. It now reuses
  `cloudResourceServiceAnchorFieldsAbsent` the same way.
- **Shared-writer backstop**: `WriteCloudResourceNodes`
  (`go/internal/storage/cypher/cloud_resource_node_writer.go`) now
  default-fills any row key its Cypher `SET` clause reads that a caller's row
  map omits, so a *future* row builder cannot reopen this class of bug even if
  it forgets a key. See that package's README "Shared-writer row-key
  default-fill backstop (#5714/#5055)" section and
  `docs/internal/evidence/5714-cloudresource-row-key-defaults.md` for the
  prove-theory-first shim that chose Go-side default-fill over an in-Cypher
  `coalesce` rewrite (both were measured viable against the pinned NornicDB
  image; Option A needs no Cypher rewrite and is unit-testable without a live
  backend).

No-Regression Evidence:
`TestExtractCloudResourceNodeRowsSetsExplicitServiceAnchorParityKeysWhenNoDecision`
(AWS) and `TestExtractAzureCloudResourceNodeRowsSetsExplicitServiceAnchorParityKeys`
(Azure) assert presence (not just `""` equality) for the no-decision/no-anchor
case, mirroring the GCP test above; both were confirmed red before this fix
(missing keys) and green after.
`TestCloudResourceNodeWriterLiveHeterogeneousBatchNeverPersistsLiteral`
(`go/internal/storage/cypher`) is the live-NornicDB, non-vacuous end-to-end
proof: a batch with one anchor-bearing row and one bare row reads back `""`
on the bare node's `workload_id`, confirmed red (literal `"row.workload_id"`)
with the shared-writer default-fill disabled and green with it restored.
`go test ./internal/reducer ./internal/storage/cypher -count=1` covers the
full reducer and shared-writer suites with no regressions.

Data repair: no explicit backfill/retract pass is needed for a CloudResource
whose source facts are still emitted — `DomainAWSResourceMaterialization`/
`DomainAzureResourceMaterialization` re-run every generation the scope carries
current resource facts (the same persistent trigger this file's
`DomainAWSCloudImageMaterialization` row documents), and
`baseCloudResourceUpsertCypher` `MERGE`s on `uid` then unconditionally `SET`s
every property (no `ON CREATE SET` gate) — so the next reprocessing of a still
actively-synced resource overwrites any previously-corrupted literal with the
correct value. See the evidence doc above for the full reasoning, including
the (out-of-scope, pre-existing) staleness exposure for a resource that stops
being observed entirely.

No-Observability-Change: no metric, span, log field, or status field changes
in either the AWS/Azure row builders or the shared writer.
