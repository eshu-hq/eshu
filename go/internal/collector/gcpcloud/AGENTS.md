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
6. `redaction.go` - `RedactionPolicyVersion`, label, IAM-member, and DNS value
   fingerprinting.
7. `parse.go` - safe CAI page parsing (drops the raw resource data blob).
8. `envelope.go` - durable fact-envelope construction and validation.
9. `relationship.go` - GCP relationship source fact construction and support
   states.
10. `image_reference.go` - image-reference fact construction and container-name
   fingerprinting.
11. `generation.go` - generation accumulation, dedupe, and fencing.
12. `metrics.go` - scoped OTEL instruments with bounded labels.
13. `extractor.go` - the per-asset-type typed-depth extractor registry
   (`RegisterAssetExtractor`, `AttributeExtraction`, `ExtractContext`).
14. `extractor_bigquery_table.go` - reference extractor for
   `bigquery.googleapis.com/Table`; the model to copy for a new asset type.
15. `extractor_compute_network.go` - typed-depth extractor for
   `compute.googleapis.com/Network` (VPC) emitting contained-subnetwork and
   peering edges.
16. `extractor_bigquery_dataset.go` - typed-depth extractor for
   `bigquery.googleapis.com/Dataset` (location, expiration policies, default KMS
   key, and a redaction-safe access-ACL summary; KMS-key edge plus
   authorizes-view/dataset/routine edges).
17. `extractor_iam_role.go` - typed-depth extractor for
   `iam.googleapis.com/Role` (custom IAM Role: title, launch stage,
   included-permission count, sensitive-permission count with a
   `grants_privilege_escalation` flag, deleted posture, and a fingerprinted
   etag; no outbound edges, since role bindings are inbound and owned by the
   IAM/binding layer).
18. `extractor_service_account_key.go` - typed-depth extractor for
   `iam.googleapis.com/ServiceAccountKey` (key type, algorithm, origin,
   valid-after/before window, disabled posture, and the fingerprinted parent
   service-account email; `service_account_key_of` edge to the parent
   ServiceAccount; never reads private/public key material).
19. `extractor_workload_identity_pool.go` - typed-depth extractor for
   `iam.googleapis.com/WorkloadIdentityPool` (lifecycle state and disabled
   posture; no outbound edges, since providers are inbound children).
20. `extractor_workload_identity_pool_provider.go` - typed-depth extractor for
   `iam.googleapis.com/WorkloadIdentityPoolProvider` (external trust type
   aws/oidc/saml, AWS account id or OIDC issuer URI anchor, attribute-mapping
   key count, attribute-condition presence, disabled posture;
   `workload_identity_provider_of_pool` edge to the parent pool; never reads
   OIDC JWKS/SAML metadata or attribute-mapping/condition expressions).
21. `extractor_secret_version.go` - typed-depth extractor for
   `secretmanager.googleapis.com/SecretVersion` (state, create/destroy time,
   replication type, CMEK posture; `secret_version_of_secret` edge to the parent
   Secret and `secret_version_encrypted_by_kms_key_version` edge to each CMEK
   CryptoKeyVersion; never reads the secret payload).
22. `extractor_api_key.go` - typed-depth extractor for
   `apikeys.googleapis.com/Key` (display name, creation time, restriction type
   browser/server/android/ios, restricted API-target services; no outbound edges;
   never reads the secret keyString or any restriction value — IPs, referrers,
   app fingerprints, bundle ids are reduced to a presence-only restriction type).
23. `extractor_recaptcha_key.go` - typed-depth extractor for
   `recaptchaenterprise.googleapis.com/Key` (display name, creation time,
   platform type web/android/ios/express, web integration type, per-platform
   allow-all posture and allow-list counts, bounded WAF service/feature; no
   outbound edges; never surfaces the platform allow-list entries — domains,
   package names, bundle ids are only counted).
24. `extractor_identity_platform_config.go` - typed-depth extractor for
   `identitytoolkit.googleapis.com/Config` (enabled sign-in methods, MFA state,
   multi-tenant toggle, authorized-domain count; no outbound edges; never reads
   OAuth/IdP client secrets, API keys, blocking-function URIs, or the
   authorized-domain values).
25. `extractor_log_bucket.go` - typed-depth extractor for
   `logging.googleapis.com/LogBucket` (retention days, locked and
   analytics-enabled posture, creation time, CMEK posture;
   `log_bucket_encrypted_by_kms_key` edge to the CMEK CryptoKey; only the
   CryptoKey resource name leaves the parser, no key material).
26. `extractor_log_sink.go` - typed-depth extractor for
   `logging.googleapis.com/LogSink` (destination type, filter presence, disabled
   posture, exclusion count, creation time, fingerprinted writer-identity email;
   export edge to the destination Storage Bucket / BigQuery Dataset / Pub/Sub
   Topic / Log Bucket; raw filter and writer email never leave the parser).
27. `extractor_dns_managed_zone.go` - typed-depth extractor for
   `dns.googleapis.com/ManagedZone` (visibility, DNSSEC state, creation time,
   private-network count, forwarding enabled/target-count, peering flag;
   visible-from-network edge to each private-visibility VPC Network and
   peers-with-network edge to the peering target Network; the zone's own
   `dnsName` and forwarding target-name-server IPs/hostnames never leave the
   parser — distinct from the `dns.googleapis.com/ResourceRecordSet` asset type,
   which flows through the separate `gcp_dns_record` fact family).
28. `extractor_storage_bucket.go` - typed-depth extractor for
   `storage.googleapis.com/Bucket` (placement, storage class, timestamps,
   uniform-bucket-level-access and public-access-prevention posture, versioning,
   a bounded lifecycle-rule count, and retention-policy posture; CMEK edge to the
   Cloud KMS CryptoKey and usage-logging export edge to the destination log
   bucket; the bucket ACL/IAM policy, object contents, and notification
   configuration are never decoded).
29. `extractor_kms_crypto_key.go` - typed-depth extractor for
   `cloudkms.googleapis.com/CryptoKey` (purpose, version-template protection
   level and algorithm, rotation schedule present only for keys that rotate,
   primary-version lifecycle state, creation time; `kms_crypto_key_in_key_ring`
   edge to the parent `cloudkms.googleapis.com/KeyRing` derived from the
   CryptoKey's own resource-name path since Cloud KMS reports no separate
   KeyRing field; never reads key material, key state history, or any
   data-plane content).
30. `extractor_gke_cluster.go` - typed-depth extractor for
   `container.googleapis.com/Cluster` (location, status, master/node version,
   release channel, create time, private-cluster and master-authorized-networks
   posture, workload identity pool, addon posture, and a per-node-pool summary
   with machine type, fingerprinted node service-account email, OAuth scope
   count, autoscaling posture, and initial node count); `gke_cluster_uses_network`
   / `gke_cluster_uses_subnetwork` edges to the cluster's Network/Subnetwork;
   master-authorized-network CIDR values, node-pool OAuth scope values, and the
   GKE "default" service-account sentinel never reach the output.

## Invariants

- GCP cloud data is reported source evidence. This package may emit typed source
  facts for parsed resources, provider relationships, label-backed tag
  observations, IAM policy observations, DNS record observations,
  image-reference observations, and collection warnings. Do not materialize
  graph truth, reducer admission, or query behavior here.
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
- Typed depth is per-asset-type: register one `AssetAttributeExtractor` per asset
  type in its own `extractor_<type>.go` file via `init()`; never grow a shared
  parser switch. An extractor receives the raw resource.data transiently and
  returns only bounded, redaction-safe attributes, correlation anchors, and typed
  relationships. Drop data-plane locators (object paths inside source URIs,
  request bodies); keep only resource identities (bucket, dataset, KMS key names).
  Adding a new asset type's attributes does not bump the `gcp_cloud_resource`
  schema version — the `attributes`/`correlation_anchors` fields are generic.
- Fingerprint IAM members, DNS record values, and sensitive label values through
  the keyed `redact` package. Fingerprint container names before image-reference
  emission. Member class is a bounded enum; raw identities, DNS record values,
  and container names are never persisted.
- Keep payload redaction versioned with `RedactionPolicyVersion`.
- Metric labels are bounded enums only (collector kind, claim status, operation,
  parent scope kind, asset family, content family, status class, fact kind,
  warning kind, outcome). Never label-leak resource ids, project ids, names,
  labels, IAM members, DNS names, image references, URLs, or credential names.
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
