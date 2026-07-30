# Cloud Inventory Identity Admission Evidence (#1997, #1998)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Cloud inventory identity admission (issues #1997, #1998)

`DomainCloudInventoryAdmission` admits provider cloud-inventory source facts
(`aws_resource`, `gcp_cloud_resource`, `azure_cloud_resource`) for the current
generation into the shared canonical `cloud_resource_uid` keyspace. The handler
(`cloud_inventory_admission.go`) resolves each record's provider raw identity —
AWS ARN, GCP Cloud Asset Inventory full resource name, Azure ARM resource id —
through the pure `internal/correlation/cloudinventory` resolver, folds records
that share a uid into one admitted canonical CloudResource row, and publishes
reducer-owned read-model facts through `PostgresCloudInventoryAdmissionWriter`
(`cloud_inventory_admission_writer.go`, fact kind
`reducer_cloud_resource_identity`).

The admission slice is graph-neutral. Canonical graph node/edge projection
stays in separate domains; the admission writer publishes the
`reducer_cloud_resource_identity` Postgres read model consumed by the
cloud-inventory API/MCP readback and the shared multi-cloud drift join. The
evidence layer is preserved: declared, applied, and observed are distinct
inputs, and a provider observation never overwrites declared IaC truth — the
admitted row records `management_origin` as the strongest contributing layer
(declared > applied > observed) plus per-layer evidence flags. Blank,
malformed, ambiguous, and unsupported identities are counted in the admission
summary and never fabricated into a uid. Additive evidence loaders may attach
safe readback-only metadata to already admitted resources: tag evidence
surfaces keyed `tag_value_fingerprints`, and Azure identity observations
surface capped `identity_policy_evidence` rows containing only bounded classes
and keyed fingerprints. Tag facts and identity facts never admit resources on
their own, and raw tag values, principal GUIDs, ARM identities, and assignment
scopes do not cross the readback boundary.

No-Regression Evidence: this slice is additive and does not change any existing
hot-path query, lease, batch size, worker count, or graph write. Baseline:
`DomainCloudInventoryAdmission` did not exist; existing reducer domains are
untouched (verified by `go test ./internal/reducer -count=1`). After: one bounded
per-generation admission that loads provider cloud-inventory source facts for a
single `(scope_id, generation_id)`, resolves identity in-memory (one SHA-256 per
record, no I/O), and upserts one canonical fact per admitted uid through the
existing `canonicalReducerFactInsertQuery` `ON CONFLICT (fact_id) DO UPDATE`
path the workload-identity and AWS-drift writers already use. Backend: Postgres
`fact_records` only; no NornicDB/Neo4j graph write. Input shape: provider
inventory records bounded to one scan generation. Terminal counts: exactly one
canonical row per admitted `cloud_resource_uid`; re-admission of the same
generation upserts the same `fact_id` (proven by
`TestPostgresCloudInventoryAdmissionWriterIsIdempotentByUID`) and 8 concurrent
workers converge to one row per uid with no duplicates and no MERGE race (proven
by `TestCloudInventoryAdmissionConcurrentWorkersConverge`, run under `-race`).
Why safe: the `fact_id` is a deterministic hash of `(scope_id, generation_id,
cloud_resource_uid)`, so the conflict key is the partition key — retries and
concurrent workers converge instead of duplicating, and no serialization,
worker-count reduction, or batch-size-1 workaround is used.

Observability Evidence: this slice adds one bounded metric instrument,
`eshu_dp_cloud_inventory_admissions_total` (`CloudInventoryAdmissions`), with
two bounded enum labels only — `provider` (aws/gcp/azure) and `outcome`
(admitted/ambiguous/unsupported/unresolved). No resource id, name, project id,
subscription id, ARN, or raw identity ever reaches a label. Operators diagnose
admission completeness and identity-resolution gaps at 3 AM by reading the
admitted-vs-ambiguous/unsupported/unresolved split per provider, alongside the
existing reducer execution spans/counters and `fact_work_items` status/failure
fields. The durable canonical rows (`reducer_cloud_resource_identity`) carry the
admission summary counts and per-layer evidence flags for read-side audit.
