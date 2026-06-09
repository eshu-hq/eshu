# Hosted Retention And Deletion Policy

Hosted governance uses this policy shape to decide what can be retained,
redacted, hashed, tombstoned, or deleted after source removal, tenant
offboarding, source ACL revocation, semantic evidence expiry, audit expiry, or
graph rebuild. It is a design contract for implementation work; it does not add
runtime enforcement by itself.

The policy preserves Eshu's facts-first model:

- source facts record what Eshu observed;
- tombstones and stale states record that prior evidence is no longer current;
- the reducer owns graph and read-model retraction;
- API, MCP, CLI, and admin reads must expose bounded stale or deletion states
  instead of silently returning false negatives.

## Invariants

- Deletion must not fabricate truth. If an object was observed before removal,
  reads should report `deleted`, `retention_expired`, `source_acl_revoked`,
  `tombstone_only`, or `graph_rebuild_required` until reducer repair and graph
  rebuild prove the new state.
- Privacy deletion must remove sensitive material first: raw content, prompts,
  provider responses, credential values, private URLs, source payloads, and
  direct personal identifiers.
- Durable correctness metadata may remain when policy allows it: stable hashes,
  source classes, credential-source classes, policy revision hashes, generation
  ids, bounded reason codes, counts, timestamps, and redaction markers.
- Retention is evaluated by data class and source scope. Repository deletion,
  tenant offboarding, source ACL revocation, provider profile removal,
  extension revocation, and collector removal can have different deadlines.
- Backups must not become a shadow retention path. Restore proof must replay
  deletion markers and retention expiry before restored state becomes readable.

## Data Class Matrix

| Data class | Retention posture | Deletion behavior |
| --- | --- | --- |
| Raw source content | Hosted policy should keep no raw content beyond the minimum indexing window unless a private operator policy explicitly allows it. | Delete or overwrite with a redaction marker before graph/read-model repair. |
| Redacted excerpts | May be retained only with redaction version, source class, freshness, and policy revision metadata. | Retain only if still allowed by source policy; otherwise delete and keep a tombstone reason. |
| Fact payloads | Metadata facts may remain for correctness; sensitive payload fields must be redacted, hashed, or dropped by the owning fact schema. | Write source-supported tombstones or retention-expired markers; do not delete history in a way that makes prior truth look nonexistent. |
| Semantic observations and code hints | Optional, non-canonical evidence with policy, prompt version, provider profile class, freshness, and redaction metadata. | Mark stale on source change or deletion; delete hint payloads when source ACL, retention, or provider profile policy no longer allows them. |
| Provider prompts and responses | Raw prompt text, response bodies, provider error bodies, request ids, and credential-bearing URLs are not retained in hosted shared artifacts. | Delete immediately if accidentally captured; retain only safe hashes or reason classes needed for audit and debugging. |
| Provider metadata | Low-cardinality provider kind, profile class, credential-source kind, policy state, budget state, and redaction version may remain. | Remove provider profile handles when the profile is removed unless audit policy explicitly allows hashed profile ids. |
| Governance audit events | Retain redacted decision events according to audit retention class. Events name actor class or safe hash, decision, reason, policy revision, and timestamp. | Expire event bodies after the audit retention window; keep aggregate counts only when policy allows. |
| Graph projections | Reducer-owned derived state. Graph nodes and edges are not the source of retention truth. | Retract or rebuild from remaining active facts and tombstones. Do not rely on graph-only deletion as privacy proof. |
| Status rows and admin readbacks | Bounded current state, aggregate counts, low-cardinality modes, and reason codes. | Report deletion progress and stale sections without source names, raw errors, prompts, provider responses, or credential handles. |
| Backups and restore artifacts | Retained by private operator backup policy. Public docs and issue examples must not include backup contents. | Restore must replay deletion, tombstone, and retention-expiry markers before restored data is considered queryable. |

## Deletion And Offboarding Triggers

| Trigger | Required behavior |
| --- | --- |
| Tenant or workspace offboarding | Stop new claims, deny scoped reads, disable provider/extension egress, write an offboarding marker, delete sensitive content and semantic payloads, then run reducer repair and graph rebuild for the affected boundary. |
| Repository removal | Stop ingestion and collector claims for the repository scope, tombstone active repository facts, delete raw content and read-model payloads, and return bounded stale/deleted status until graph repair completes. |
| Source document removal | Treat source-supported delete or permission-hidden states distinctly. Delete content and semantic payloads; preserve tombstone metadata only when policy allows. |
| Source ACL revocation | Do not infer source deletion. Stop reads and provider work for that source, mark evidence `source_acl_revoked`, and keep only policy-allowed metadata for repair and audit. |
| Provider profile removal | Deny new semantic work, mark queued and prior semantic evidence stale, delete prompts/responses if present, and keep only safe profile class or hashed profile id when audit policy allows. |
| Plugin or extension revocation | Stop claim-capable work, mark pending work ineligible with a bounded reason, delete extension-owned content according to source policy, and keep revocation audit metadata. |
| Collector instance removal | Stop scheduling new claims for that collector kind or instance class, reap stale claims, and avoid deleting source facts unless the source scope is also removed or expired. |

## Tombstones And Query Truth

Tombstones preserve accuracy without retaining sensitive material. They should
carry only safe fields:

- fact kind or source class;
- stable hash or generation id;
- policy revision hash;
- tombstone reason such as `source_deleted`, `retention_expired`,
  `source_acl_revoked`, or `offboarded`;
- observed and effective timestamps;
- reducer repair and graph rebuild status.

Query surfaces must not flatten tombstones into an empty result. When a user
asks about a removed scope, API and MCP reads should return a bounded envelope
with a stale or deleted truth label, a reason code, and next checks such as
`check_deletion_status`, `wait_for_reducer_repair`, or `request_reindex`.

## Freshness, Repair, And Rebuild

Retention changes follow this order:

1. Stop new governed work for the affected boundary.
2. Delete or redact sensitive payloads from content, semantic evidence, status,
   and read-model stores.
3. Write tombstones or retention-expiry markers for fact families that support
   deletion semantics.
4. Reopen reducer domains that consume the affected facts.
5. Rebuild graph projections and read models from active facts plus tombstones.
6. Mark deletion complete only after queue state, reducer status, and graph
   repair agree.

Generation freshness must remain visible during the transition. A stale answer
is acceptable when labeled; a fresh-looking answer based on removed evidence is
not.

## Status And Readbacks

API, MCP, and admin status surfaces should expose only bounded deletion state:

- retention mode: `metadata_only`, `configured`, `disabled`, `not_configured`,
  `stale`, or `invalid`;
- deletion state: `not_requested`, `pending`, `running`, `blocked`,
  `repairing_graph`, `complete`, or `failed`;
- aggregate counts by data class and reason code;
- policy revision hash and safe source class;
- oldest pending age and last completed timestamp;
- reason codes such as `retention_policy_missing`,
  `deletion_policy_missing`, `source_acl_revoked`, `source_deleted`,
  `retention_expired`, `graph_rebuild_required`, `backup_retention_pending`,
  or `audit_retention_expired`.

Status must not expose tenant names, workspace names, repository names, source
identifiers, file paths, prompts, provider responses, credential handles,
private URLs, token values, backup contents, or raw policy documents.

## Safe Operator Examples

Check deletion progress with only the service endpoint and bearer token loaded
from private operator storage:

```bash
export ESHU_SERVICE_URL=https://eshu.example.com
# Load ESHU_API_KEY from a secret manager or private shell first.
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" \
  "$ESHU_SERVICE_URL/api/v0/status/governance"
```

For incident records or support tickets, include only:

- deletion state;
- safe source class;
- policy revision hash;
- aggregate counts;
- reason code;
- queue or graph repair status.

Do not include raw source identifiers, private endpoints, payload excerpts,
credential handles, provider request ids, prompts, responses, personal
identifiers, or backup object locators.

## Planned Proof Matrix

| Scenario | Required proof |
| --- | --- |
| Repository removal | Active reads return a stale/deleted envelope until reducer repair and graph rebuild complete; removed content is not readable. |
| Tenant or workspace offboarding | Claims stop, scoped reads deny, provider and extension egress stop, and status reports aggregate progress only. |
| Source ACL revocation | Reads and provider work stop with `source_acl_revoked`; the system does not infer source deletion. |
| Semantic evidence deletion | Prompt, response, and hint payloads disappear; safe metadata can remain only with policy state, freshness, and redaction version. |
| Audit retention expiry | Detailed audit bodies expire while aggregate counts and retention-expired reason codes remain when policy allows. |
| Graph rebuild after deletion | Rebuilt graph excludes retracted nodes and edges, while API/MCP reads expose tombstone or stale truth for prior evidence. |
| Backup restore | Restored data replays deletion and retention-expiry markers before any query surface is marked current. |

No-Regression Evidence: strict docs build proves this page and navigation render.
Implementation PRs must add focused tests for the relevant scenario before
changing runtime behavior.

No-Observability-Change: this page adds a design contract only. It emits no
metrics, spans, logs, status rows, facts, graph writes, API responses, MCP
payloads, audit events, or deletion jobs.
