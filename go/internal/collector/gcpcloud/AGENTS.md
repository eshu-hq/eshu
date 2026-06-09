# AGENTS.md - internal/collector/gcpcloud guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `docs/public/reference/gcp-cloud-collector-contract.md` - the provider source,
   scope/generation, fact family, payload boundary, telemetry, and fixture
   contract.
3. `docs/public/reference/multi-cloud-collector-contract.md` - the shared
   cloud-collector boundary this package inherits.
4. `types.go` - `CollectorKind`, `Boundary`, `ParentScopeKind`,
   `ResourceObservation`, `WarningObservation`.
5. `normalize.go` - CAI full-resource-name and ancestor normalization.
6. `redaction.go` - `RedactionPolicyVersion`, label and IAM-member fingerprinting.
7. `parse.go` - safe CAI page parsing (drops the raw resource data blob).
8. `envelope.go` - durable fact-envelope construction and validation.
9. `generation.go` - generation accumulation, dedupe, and fencing.
10. `metrics.go` - scoped OTEL instruments with bounded labels.

## Invariants

- GCP cloud data is reported source evidence. Do not materialize graph truth,
  reducer admission, or query behavior in this package.
- Keep the claim boundary explicit: collector instance, parent scope kind and id,
  asset family, content family, location bucket, scope id, generation id, and a
  positive fencing token.
- Preserve the CAI full resource name verbatim. Add normalized asset type,
  project id/number, folder/org ancestors, and location alongside it; never
  replace raw identity.
- Keep stable fact keys deterministic from fact kind, full resource name, asset
  type, content family, and provider update time. Duplicate delivery within a
  generation must converge; stale generations are rejected by fencing token via
  `GenerationTracker`.
- Never put secrets, IAM policy JSON, object contents, startup scripts, public or
  private IP addresses, raw provider response bodies, or the raw CAI resource
  data blob in facts. The parser is the single redaction choke point for the data
  blob.
- Fingerprint IAM members and sensitive label values through the keyed `redact`
  package. Member class is a bounded enum; the raw email is never persisted.
- Keep payload redaction versioned with `RedactionPolicyVersion`.
- Metric labels are bounded enums only (collector kind, claim status, operation,
  parent scope kind, asset family, content family, status class, fact kind,
  warning kind, outcome). Never label-leak resource ids, project ids, names,
  labels, IAM members, URLs, or credential names.
- This package does not call Google Cloud APIs. A future runtime adapter owns SDK
  pagination, retries, throttling, and credential loading.

## Common Changes

- Add a new GCP fact family only after `internal/facts` exposes the fact kind and
  schema version via `GCPFactKinds()` / `GCPSchemaVersion(kind)` and registers it
  in `CoreFactKinds()`.
- Keep every source file under 500 lines; split into a sibling before the cap.
- Update `README.md`, `doc.go`, and this file when the exported surface or
  contract changes, then run `scripts/verify-package-docs.sh`.

## What Not To Change Without An ADR

- Do not make this package call Google Cloud APIs directly.
- Do not add graph writes, reducer admission, or query behavior here.
- Do not introduce a generic `cloud_resource` source fact; GCP facts are
  provider-specific until a schema PR deliberately migrates AWS, GCP, and Azure
  together.
- Do not infer environment, workload, ownership, or deployable-unit truth from
  names, labels, folders, or project aliases in this package.
```
