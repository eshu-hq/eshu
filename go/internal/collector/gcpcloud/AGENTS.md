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
16. `extractor_forwarding_rule.go` - typed-depth extractor for
   `compute.googleapis.com/ForwardingRule` (load-balancer forwarding rule:
   region, load-balancing scheme with a derived external posture flag, IP
   protocol, port range/ports, IP version, all-ports and network-tier posture,
   creation time; typed edges to the resolved target - backend service, target
   pool, or a target-proxy kind - plus enclosing network and subnetwork; never
   decodes the reserved `IPAddress` field, mirroring the Static Address
   extractor's treatment of its own address value).
17. `extractor_bigquery_dataset.go` - typed-depth extractor for
   `bigquery.googleapis.com/Dataset` (location, expiration policies, default KMS
   key, and a redaction-safe access-ACL summary; KMS-key edge plus
   authorizes-view/dataset/routine edges).
18. `extractor_iam_role.go` - typed-depth extractor for
   `iam.googleapis.com/Role` (custom IAM Role: title, launch stage,
   included-permission count, sensitive-permission count with a
   `grants_privilege_escalation` flag, deleted posture, and a fingerprinted
   etag; no outbound edges, since role bindings are inbound and owned by the
   IAM/binding layer).
19. `extractor_service_account_key.go` - typed-depth extractor for
   `iam.googleapis.com/ServiceAccountKey` (key type, algorithm, origin,
   valid-after/before window, disabled posture, and the fingerprinted parent
   service-account email; `service_account_key_of` edge to the parent
   ServiceAccount; never reads private/public key material).
20. `extractor_workload_identity_pool.go` - typed-depth extractor for
   `iam.googleapis.com/WorkloadIdentityPool` (lifecycle state and disabled
   posture; no outbound edges, since providers are inbound children).
21. `extractor_workload_identity_pool_provider.go` - typed-depth extractor for
   `iam.googleapis.com/WorkloadIdentityPoolProvider` (external trust type
   aws/oidc/saml, AWS account id or OIDC issuer URI anchor, attribute-mapping
   key count, attribute-condition presence, disabled posture;
   `workload_identity_provider_of_pool` edge to the parent pool; never reads
   OIDC JWKS/SAML metadata or attribute-mapping/condition expressions).
22. `extractor_secret_version.go` - typed-depth extractor for
   `secretmanager.googleapis.com/SecretVersion` (state, create/destroy time,
   replication type, CMEK posture; `secret_version_of_secret` edge to the parent
   Secret and `secret_version_encrypted_by_kms_key_version` edge to each CMEK
   CryptoKeyVersion; never reads the secret payload).
23. `extractor_api_key.go` - typed-depth extractor for
   `apikeys.googleapis.com/Key` (display name, creation time, restriction type
   browser/server/android/ios, restricted API-target services; no outbound edges;
   never reads the secret keyString or any restriction value — IPs, referrers,
   app fingerprints, bundle ids are reduced to a presence-only restriction type).
24. `extractor_recaptcha_key.go` - typed-depth extractor for
   `recaptchaenterprise.googleapis.com/Key` (display name, creation time,
   platform type web/android/ios/express, web integration type, per-platform
   allow-all posture and allow-list counts, bounded WAF service/feature; no
   outbound edges; never surfaces the platform allow-list entries — domains,
   package names, bundle ids are only counted).
25. `extractor_identity_platform_config.go` - typed-depth extractor for
   `identitytoolkit.googleapis.com/Config` (enabled sign-in methods, MFA state,
   multi-tenant toggle, authorized-domain count; no outbound edges; never reads
   OAuth/IdP client secrets, API keys, blocking-function URIs, or the
   authorized-domain values).
26. `extractor_log_bucket.go` - typed-depth extractor for
   `logging.googleapis.com/LogBucket` (retention days, locked and
   analytics-enabled posture, creation time, CMEK posture;
   `log_bucket_encrypted_by_kms_key` edge to the CMEK CryptoKey; only the
   CryptoKey resource name leaves the parser, no key material).
27. `extractor_log_sink.go` - typed-depth extractor for
   `logging.googleapis.com/LogSink` (destination type, filter presence, disabled
   posture, exclusion count, creation time, fingerprinted writer-identity email;
   export edge to the destination Storage Bucket / BigQuery Dataset / Pub/Sub
   Topic / Log Bucket; raw filter and writer email never leave the parser).
28. `extractor_dns_managed_zone.go` - typed-depth extractor for
   `dns.googleapis.com/ManagedZone` (visibility, DNSSEC state, creation time,
   private-network count, forwarding enabled/target-count, peering flag;
   visible-from-network edge to each private-visibility VPC Network and
   peers-with-network edge to the peering target Network; the zone's own
   `dnsName` and forwarding target-name-server IPs/hostnames never leave the
   parser — distinct from the `dns.googleapis.com/ResourceRecordSet` asset type,
   which flows through the separate `gcp_dns_record` fact family).
29. `extractor_storage_bucket.go` - typed-depth extractor for
   `storage.googleapis.com/Bucket` (placement, storage class, timestamps,
   uniform-bucket-level-access and public-access-prevention posture, versioning,
   a bounded lifecycle-rule count, and retention-policy posture; CMEK edge to the
   Cloud KMS CryptoKey and usage-logging export edge to the destination log
   bucket; the bucket ACL/IAM policy, object contents, and notification
   configuration are never decoded).
30. `extractor_kms_crypto_key.go` - typed-depth extractor for
   `cloudkms.googleapis.com/CryptoKey` (purpose, version-template protection
   level and algorithm, rotation schedule present only for keys that rotate,
   primary-version lifecycle state, creation time; `kms_crypto_key_in_key_ring`
   edge to the parent `cloudkms.googleapis.com/KeyRing` derived from the
   CryptoKey's own resource-name path since Cloud KMS reports no separate
   KeyRing field; never reads key material, key state history, or any
   data-plane content).
31. `extractor_gke_cluster.go` - typed-depth extractor for
   `container.googleapis.com/Cluster` (location, status, master/node version,
   release channel, create time, private-cluster and master-authorized-networks
   posture, workload identity pool, addon posture, and a per-node-pool summary
   with machine type, fingerprinted node service-account email, OAuth scope
   count, autoscaling posture, and initial node count); `gke_cluster_uses_network`
   / `gke_cluster_uses_subnetwork` edges to the cluster's Network/Subnetwork;
   master-authorized-network CIDR values, node-pool OAuth scope values, and the
   GKE "default" service-account sentinel never reach the output.
32. `extractor_sql_instance.go` - typed-depth extractor for
   `sqladmin.googleapis.com/Instance` (database version, region, state,
   instance type, tier, availability type, disk size, public-IP posture,
   SSL mode, authorized-network count, backup/PITR posture, CMEK key name,
   replica count, creation time; `sql_instance_in_network` edge to the private
   Compute Network, `sql_instance_encrypted_by_kms_key` edge to the CMEK
   CryptoKey, and `sql_instance_has_replica`/`sql_instance_replica_of`
   replica-topology edges; a bare `masterInstanceName`/`replicaNames` entry
   with no project qualifier — the common same-project sqladmin API shape — is
   resolved against the instance's own project, and `kmsKeyName` is
   normalized so an already CAI-prefixed value is never double-prefixed;
   never reads a public/private IP address or an authorized-network
   CIDR/label — only `ipv4Enabled` and the authorized-network count are
   kept).
33. `extractor_vpn_tunnel.go` - typed-depth extractor for
   `compute.googleapis.com/VpnTunnel` (region, IKE version, tunnel status,
   HA/Classic gateway-interface indexes, and bounded local/remote
   traffic-selector counts; `vpn_tunnel_uses_vpn_gateway` edge for an HA VPN
   tunnel's own gateway, `vpn_tunnel_uses_target_vpn_gateway` edge for a
   Classic VPN tunnel's target gateway, `vpn_tunnel_peers_with_vpn_gateway`
   edge to either an HA peer-to-peer gateway or an external peer gateway, and
   `vpn_tunnel_uses_router` edge to the Cloud Router used for BGP dynamic
   routing when configured; reuses `assetTypeComputeVPNGateway` from the
   sibling Cloud VPN Gateway extractor (#4302, `extractor_vpn_gateway.go`)
   and `assetTypeComputeRouter` from the Cloud Router extractor (#4301,
   `extractor_router.go`) below, never redeclaring either; never reads the
   tunnel's own `peerIp`, `sharedSecret`, `sharedSecretHash`, or
   `detailedStatus` fields, and traffic-selector CIDR values are reduced to
   counts, never persisted).
34. `extractor_backend_service.go` - typed-depth extractor for
   `compute.googleapis.com/BackendService` and
   `compute.googleapis.com/RegionBackendService` (protocol, load-balancing
   scheme, port name, timeout, CDN posture as an explicit tri-state so a false
   is kept, session affinity, region omitted for a global backend service,
   backend-entry count, creation time); `backend_service_uses_security_policy`
   edge to the Cloud Armor SecurityPolicy, `backend_service_uses_edge_security_policy`
   edge to the separate Cloud Armor edge SecurityPolicy (`securityPolicy` and
   `edgeSecurityPolicy` are two distinct Compute resource-URL fields, both
   decoded), `backend_service_uses_health_check` edge to each HealthCheck, and
   a shared `backend_service_has_backend` edge to each backend's InstanceGroup
   or NetworkEndpointGroup (the same relationship type for both group kinds,
   distinguished by `target_asset_type`, mirroring the ForwardingRule
   extractor's shared target-proxy relationship type); this is the other side
   of the edge the ForwardingRule extractor already resolves toward
   `assetTypeComputeBackendService` (declared in `extractor_forwarding_rule.go`
   and reused here, never redeclared); the CAI search/analysis APIs report one
   `BackendService` asset type for both regional and global scope, but the
   list/export/monitor/query path this collector uses emits regional backend
   services under the distinct `RegionBackendService` asset type instead, so
   both asset types register to this same extractor function; IAP OAuth
   client id/secret and CDN cache-key/signed-URL key material are never
   decoded, and per-backend balancing-mode/capacity/utilization tuning fields
   are dropped by omission.
35. `extractor_url_map.go` - typed-depth extractor for
   `compute.googleapis.com/UrlMap` (bounded host-rule, path-matcher,
   path-rule, and route-rule counts, creation time); `url_map_default_service`
   edge from the map's own `defaultService`, `url_map_path_matcher_default_service`
   edge from each pathMatcher's `defaultService`, `url_map_path_rule_service`
   edge from each pathMatcher's `pathRules[].service`,
   `url_map_route_rule_service` edge from each pathMatcher's
   `routeRules[].service` (the advanced-routing alternative/complement to
   `pathRules`), and `url_map_route_rule_weighted_service` edge from each
   entry of `routeRules[].routeAction.weightedBackendServices[].backendService`
   — each resolved to either `compute.googleapis.com/BackendService` or
   `compute.googleapis.com/BackendBucket` by the referenced resource segment;
   never decodes `hostRules[].hosts`, `pathMatchers[].pathRules[].paths`, or
   `pathMatchers[].routeRules[].matchRules` (nor routeAction's non-backend
   traffic-shaping controls, such as `weight`) — raw host/path routing
   patterns and traffic-shaping controls are dropped, only bounded counts and
   the resolvable backend references leave the parser; relationship
   observations are deduped by (type, target) since the same backend can be
   referenced from more than one place on a URL map.
36. `extractor_router.go` - typed-depth extractor for
   `compute.googleapis.com/Router` (region, BGP ASN and advertise mode, a
   bounded per-peer summary of name/peer-ASN/interface-name, a bounded
   per-NAT summary of name/IP-allocate-option/source-subnetwork-ranges,
   encrypted-interconnect-router posture, creation time; `router_in_network`
   edge to the enclosing Network plus a typed edge per interface to its
   linked VPN tunnel, linked Interconnect attachment, or subnetwork; a BGP
   peer's `interfaceName` never becomes an edge endpoint directly — only the
   interface's own linked resource resolves — and no BGP peer/interface IP
   address or NAT IP resource reference ever reaches the extraction output;
   `routerBgpPeerData`, `routerNatData`, and `routerInterfaceData` never
   declare struct fields for the CAI IP/CIDR fields at all, so those values
   are never decoded into Go memory in the first place).
37. `extractor_vpn_gateway.go` - typed-depth extractor for
   `compute.googleapis.com/VpnGateway` (Cloud HA-VPN gateway: region, stack
   type, gateway IP version, creation time, and a bounded VPN-interface count;
   `vpn_gateway_in_network` edge to the enclosing Compute `Network`); a
   regional-only asset type with no global variant; per-interface id, the
   interface `ipAddress`/`ipv6Address`, and any `interconnectAttachment`
   reference are never decoded into Go memory at all — only the interface
   count crosses the redaction boundary, so no public or private IP address
   reaches a fact; distinct from `compute.googleapis.com/TargetVpnGateway`
   (the older Classic VPN target-gateway resource referenced by
   `ForwardingRule.target` and handled by `extractor_forwarding_rule.go`).
38. `extractor_redis_instance.go` - typed-depth extractor for
   `redis.googleapis.com/Instance` (Memorystore for Redis: location id, Redis
   version, tier, memory size, connect mode, transit-encryption mode,
   auth-enabled posture, state, creation time, replica count, read-replicas
   mode, CMEK key name, persistence mode; `redis_instance_in_network` edge to
   the authorized Compute Network and `redis_instance_encrypted_by_kms_key`
   edge to the CMEK CryptoKey; never reads `host`, `port`, `readEndpoint`,
   `readEndpointPort`, `reservedIpRange`, or `secondaryIpRange` — all IP
   address, port, or CIDR values, never resource identities).
39. `extractor_health_check.go` - typed-depth extractor for
   `compute.googleapis.com/HealthCheck` (protocol type HTTP/HTTPS/TCP/SSL/
   HTTP2/GRPC, check interval, timeout, healthy/unhealthy thresholds, creation
   time, and the port plus port specification read from whichever
   protocol-specific sub-object matches type; reuses `assetTypeComputeHealthCheck`
   from the sibling Backend Service extractor (`extractor_backend_service.go`),
   never redeclaring it, since that extractor's `backend_service_uses_health_check`
   edge already resolves toward this asset type as its target); no outbound
   edges or anchors, since backend services are the inbound side of the only
   graph-relevant relationship; never reads `requestPath`, `host`, `response`,
   `proxyHeader`, or `grpcServiceName` — all data-plane routing/matching
   values, never resource identities.
40. `extractor_ssl_certificate.go` - typed-depth extractor for
   `compute.googleapis.com/SslCertificate` (certificate type MANAGED/SELF_MANAGED,
   managed-certificate provisioning status, a bounded managed-domain count, a
   bounded subject-alternative-name count present only for a self-managed
   certificate after issuance, expiry time, creation time; an omitted `type`
   is derived to SELF_MANAGED per the Compute sslCertificates schema, not
   dropped, and deriving it reads no key material); declares
   `assetTypeComputeSSLCertificate` for the Target HTTPS Proxy / Target SSL
   Proxy extractors to reuse as their `sslCertificates[]` edge target; emits no
   outbound edges itself, since the certificate's graph value is inbound —
   mirroring the Custom IAM Role extractor's inbound-only edge shape; never
   decodes `managed.domains[]` or `subjectAlternativeNames` into an attribute or
   anchor (the extractor seam carries no redaction key, so — mirroring the
   Managed Zone extractor's treatment of its own `dnsName` and the reCAPTCHA
   Enterprise Key extractor's treatment of allowed-domain entries — every
   domain value is reduced to a bounded count only), and never reads the
   `selfManaged.certificate` or `selfManaged.privateKey` PEM material.
41. `extractor_target_https_proxy.go` - typed-depth extractor for
   `compute.googleapis.com/TargetHttpsProxy` (QUIC negotiation override
   posture and creation time; `target_https_proxy_uses_url_map` edge to the
   resolved UrlMap and `target_https_proxy_uses_ssl_policy` edge to the
   resolved SslPolicy when present). Serving-certificate resolution honors the
   Compute API's mutually-exclusive precedence: when `certificateMap` is set it
   emits a `target_https_proxy_uses_certificate_map` edge to the Certificate
   Manager CertificateMap and suppresses the classic `sslCertificates` list
   (the API ignores `sslCertificates` when a map is set, so emitting those
   edges would be stale); otherwise each `sslCertificates` entry is routed by
   domain — a Compute self-link to a `target_https_proxy_uses_ssl_certificate`
   edge, a Certificate Manager self-link to a
   `target_https_proxy_uses_certificate_manager_certificate` edge. Declares
   `assetTypeComputeSSLPolicy` and the two
   `certificatemanager.googleapis.com/*` asset types (all-caps initialisms per
   Effective Go), and reuses `assetTypeComputeSSLCertificate` from
   `extractor_ssl_certificate.go` (never redeclaring it),
   `assetTypeComputeTargetHTTPSProxy` from `extractor_forwarding_rule.go`, and
   `assetTypeComputeUrlMap` from `extractor_url_map.go`; the reverse edge from
   a ForwardingRule to this proxy is already emitted by
   `extractor_forwarding_rule.go` (`forwarding_rule_targets_target_proxy`)
   since CAI's TargetHttpsProxy resource.data carries no back-reference to the
   forwarding rule that targets it — the reference is one-directional — so this
   extractor emits no forwarding-rule edge of its own; no certificate key
   material, private key, or response body is ever decoded, since
   `sslCertificates`, `certificateMap`, and `sslPolicy` carry only resource
   self-links.
42. `extractor_bigtable_instance.go` - typed-depth extractor for
   `bigtableadmin.googleapis.com/Instance` (display name, state, instance type
   PRODUCTION/DEVELOPMENT, edition). The Bigtable Admin v2 Instance resource
   carries only instance-level metadata — it has no clusters, encryption, or
   `kmsKeyName` field — so this extractor emits no outbound edges or anchors;
   clusters are a separate CAI asset type handled by
   `extractor_bigtable_cluster.go`. Instance labels are already carried by the
   shared envelope label path (see `envelope.go`) and are not re-declared as a
   typed attribute.
43. `extractor_bigtable_cluster.go` - typed-depth extractor for
   `bigtableadmin.googleapis.com/Cluster` (location, state, serve nodes, node
   scaling factor, default storage type, and the CMEK key name from
   `encryptionConfig.kmsKeyName`). Emits `bigtable_cluster_in_instance` to the
   parent Instance — derived from the cluster's own resource-name path, since a
   Cluster resource name embeds its parent
   (`.../instances/<i>/clusters/<c>`), mirroring the Secret Version extractor's
   parent-derivation — and `bigtable_cluster_encrypted_by_kms_key` to the CMEK
   CryptoKey (an already CAI-prefixed value is kept as-is, a bare value is
   prefixed, mirroring the Memorystore Redis Instance CMEK normalization);
   surfaces the parent Instance and CMEK key resource names as correlation
   anchors; reuses `assetTypeBigtableInstance` from the sibling Instance
   extractor (never redeclaring it); never reads table schema or row data.
44. `extractor_dataflow_job.go` - typed-depth extractor for
   `dataflow.googleapis.com/Job` (job type BATCH/STREAMING, current state,
   location, create/start time, `jobMetadata.sdkVersion` version and
   `sdkSupportStatus` lifecycle enum, the fingerprinted runtime worker
   service-account email, and the `environment.serviceKmsKeyName` CMEK key
   name); `dataflow_job_uses_network` and `dataflow_job_uses_subnetwork` edges
   resolved from the first worker pool that reports a network or subnetwork
   reference — both endpoints come from that same single pool, never
   cross-latched across pool indices, so a network from one pool and a
   subnetwork from another are never paired into a placement that never
   co-occurred (reusing `computeFullResourceNameFromSelfLink` from the Compute
   Network extractor, with a bare subnetwork short name resolved against the
   pool's own zone), `dataflow_job_uses_staging_bucket` edge to the GCS bucket
   parsed from `environment.tempStoragePrefix` via `dataflowStagingBucket`,
   which handles the two Dataflow-documented resource forms
   (`storage.googleapis.com/{bucket}/{object}` and
   `{bucket}.storage.googleapis.com/{object}`) plus `gs://` defensively and
   drops the object path in every form (reusing `gcsBucketFromURI` and
   `storageBucketResourceNamePrefixFmt` from the BigQuery Table extractor), and
   `dataflow_job_encrypted_by_kms_key` edge to the CMEK CryptoKey (reusing
   `assetTypeKMSCryptoKey` / `cloudKMSResourceNamePrefix`, with the same
   already-prefixed-or-bare normalization as the Dataproc Cluster and Redis
   Instance CMEK edges); never reads pipeline parameter values,
   `environment.userAgent`, `environment.sdkPipelineOptions` option values, or
   any step-graph content — only the object path after the staging bucket is
   dropped as a data-plane locator, mirroring the BigQuery Table extractor's
   external-source treatment.
45. `extractor_filestore_instance.go` - typed-depth extractor for
   `file.googleapis.com/Instance` (Filestore Instance: state, tier, creation
   time, a bounded file-share count, the first `networks[]` entry's connect
   mode, CMEK key name, a bounded label count); a `filestore_instance_in_network`
   edge for every `networks[]` entry toward the attached Compute Network (a
   bare short network name is promoted to the project-less global partial and
   resolved the same way the GKE Cluster extractor resolves its own network
   reference) and a `filestore_instance_encrypted_by_kms_key` edge to the CMEK
   CryptoKey (an already CAI-prefixed `kmsKeyName` is kept as-is, mirroring the
   Memorystore Redis Instance CMEK normalization); never reads
   `networks[].reservedIpRange` or `networks[].modes` — the former is a CIDR
   range and never decoded into Go memory at all. The typed-depth `attributes`
   map carries only a bounded `file_share_count` and `label_count`, never
   per-file-share name/capacity or label entries; the labels themselves are
   still captured and value-fingerprinted per `redaction_policy_version` by the
   collector's shared label path (the extractor just does not re-copy them into
   typed depth).

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
