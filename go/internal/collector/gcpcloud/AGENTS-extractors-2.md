# AGENTS-extractors-2.md — gcpcloud typed-depth extractor catalog (2 of 2)

Read this alongside [`AGENTS.md`](AGENTS.md), which holds the package
invariants, common changes, and what-not-to-change-without-an-ADR rules. This
file is part 2 of the per-asset-type extractor "Read First" catalog, covering
items 47–61, continuing the ascending list from
[`AGENTS-extractors-1.md`](AGENTS-extractors-1.md) (items 14–46). A "#N above"
cross-reference points to the lower-numbered entry, in part 1 when N ≤ 46.

Append a new extractor's entry at the end of this file and keep the numbering
ascending.

## Read First — typed-depth extractors (47–61)

47. `extractor_bigquery_transfer_config.go` - typed-depth extractor for
   `bigquerydatatransfer.googleapis.com/TransferConfig` (BigQuery Data Transfer /
   Scheduled Query config: data source id, schedule, lifecycle state, disabled
   posture, CMEK posture, and a fingerprinted owner email — `ownerInfo.email`, the
   identity the transfer runs as, never emitted raw). Emits a
   `transfer_config_writes_to_dataset` edge to the destination BigQuery Dataset, a
   `transfer_config_encrypted_by_kms_key` edge to the CMEK CryptoKey, and a
   `transfer_config_notifies_topic` edge to the `notificationPubsubTopic` Pub/Sub
   Topic. `destinationDatasetId` is a bare dataset id with no project qualifier,
   and the BigQuery Data Transfer resource exposes no separate destination-project
   field anywhere in the schema — verified against the live datatransfer v1
   discovery document and the googleapis `transfer.proto` (#4469). A cross-project
   transfer config is created inside its destination project (its own resource
   name embeds that project), so the destination dataset resolves against the
   config's own project (`ctx.ProjectID`), which IS the destination project by
   GCP's resource model, for both same-project and cross-region/cross-project copy
   configs; there is no cross-project destination signal in CAI to prefer. The
   `params` map (user query text, source object paths, and the source-side
   project/dataset ids of a copy job) is never decoded and never leaves the parser.
48. `extractor_workflows_workflow.go` - typed-depth extractor for
   `workflows.googleapis.com/Workflow` (deployment state, revision id, call-log
   level, execution-history level, create/update/revision-create time, a
   source-contents presence flag, the normalized CMEK key relative name, and
   the fingerprinted runtime service-account email — verified against the live
   Workflows v1 discovery document); `workflow_encrypted_by_kms_key` edge to
   the CMEK CryptoKey when `cryptoKeyName` resolves to a valid CryptoKey full
   name (an already CAI-prefixed value is kept as-is, mirroring the Dataflow
   Job and Memorystore Redis Instance CMEK normalization; a leading `/` is
   trimmed, mirroring the Filestore Instance and Dataflow Job CMEK helpers).
   The Workflows v1 API documents two project-inferred `cryptoKeyName` forms —
   a `"projects/-/..."` wildcard and a project-less `"locations/..."` form,
   both meaning "infer the project from the workflow's own project" — so both
   are qualified against the workflow's own `ctx.ProjectID` rather than
   producing an edge rooted at the literal `projects/-` segment or silently
   dropping the relationship; the `crypto_key_name` attribute and the CMEK
   edge/anchor are set only together, from the same resolved full name, so an
   unnormalizable or unqualifiable `cryptoKeyName` value (malformed shape,
   wrong-domain prefix, or a project-inferred form with no project to qualify
   against) is dropped entirely rather than surfaced unresolved. The runtime
   service account is carried as a fingerprinted attribute/anchor only, never
   an edge, the same treatment as the Dataflow Job, Dataproc Cluster, and GKE
   Cluster extractors' own service accounts, since an email is not an exactly
   resolvable CAI endpoint; `sourceContents` (the workflow's YAML/JSON
   definition body) is decoded only far enough to set a boolean presence flag,
   and the decoded copy is cleared immediately afterward — no step, argument,
   header, or embedded credential value from that body is ever read, so a
   called service (Cloud Run, Cloud Functions, or an arbitrary HTTP endpoint)
   referenced only inside the workflow definition is out of reach of this
   safe-metadata extractor and is not modeled as an edge; `userEnvVars`,
   `tags`, and `labels` are not re-declared in typed depth since the
   collector's shared label/tag path already captures and fingerprints them.
49. `extractor_kms_key_ring.go` - typed-depth extractor for
   `cloudkms.googleapis.com/KeyRing` (Cloud KMS KeyRing: location derived from
   the resource-name path and creation time — per the live Cloud KMS v1
   `projects.locations.keyRings` REST reference, the KeyRing resource carries
   only `name` and `createTime`, no encryption, label, or child-key field of
   its own). Reuses `assetTypeKMSKeyRing` declared by the sibling CryptoKey
   extractor (`extractor_kms_crypto_key.go`, #4296), never redeclaring it.
   Emits no outbound edges or correlation anchors: every contained CryptoKey
   already resolves the `kms_crypto_key_in_key_ring` edge toward this asset
   type from the CryptoKey side, mirroring the Custom IAM Role and SSL
   Certificate extractors' inbound-only edge shape; never reads key material,
   IAM policy, or any data-plane content.
50. `extractor_org_policy.go` - typed-depth extractor for
   `orgpolicy.googleapis.com/Policy` (Organization Policy / constraint binding:
   constraint name, spec rule-shape summary — total rule count plus per-kind
   counts for allow-values/deny-values/allow-all/deny-all/condition-present
   rules and a count of rules that enforce — spec `inheritFromParent` and
   `reset` booleans, a fingerprinted spec etag, spec `updateTime`, and
   dry-run-spec presence with its own bounded rule count). The constraint name
   and the bound organization/folder/project target are both derived from the
   Policy's own CAI full resource name
   (`//orgpolicy.googleapis.com/{organizations|folders|projects}/<id>/policies/<constraint>`
   per the Cloud Asset Inventory resource-name-format reference), never from
   `resource.data.name` — untrusted parser input; the derivation fails closed
   unless the name carries the exact `//orgpolicy.googleapis.com/` prefix and a
   `<kind>/<id>/policies/<constraint>` shape, so a relative or wrong-service
   name mints no edge. Emits a single `org_policy_applies_to_resource` edge to
   the resolved `cloudresourcemanager.googleapis.com/Organization` /
   `.../Folder` / `.../Project` node, reusing
   `assetTypeCloudResourceManagerProject` from
   `extractor_firebase_project.go` (never redeclaring it) and declaring the
   sibling Organization/Folder asset-type constants here. The rule union's
   allowed/denied VALUE lists, the condition expression text, and any
   custom-constraint `parameters` (a `google.protobuf.Struct` that can carry
   organization-specific identifiers) are decoded only to compute bounded
   counts and are never persisted or surfaced to any fact — only bounded counts
   and booleans leave this extractor, mirroring the Custom IAM Role extractor's
   treatment of its permission list.
51. `extractor_network_endpoint_group.go` - typed-depth extractor for
   `compute.googleapis.com/NetworkEndpointGroup` (network endpoint type
   decoded as a free string — never validated against a hardcoded Go enum,
   since the Compute API is the source of truth for valid values — size,
   default port, zone or region placement, creation time, a serverless
   discriminator plus service/function name for a SERVERLESS NEG's exactly
   one configured cloudRun/appEngine/cloudFunction backend, Private Service
   Connect posture — connection status, producer port, connection id, and a
   fingerprinted target-service hostname — and a bounded annotation count;
   verified against the live Compute v1 discovery document, whose
   `networkEndpointType` enum is `GCE_VM_IP`, `GCE_VM_IP_PORT`,
   `GCE_VM_IP_PORTMAP`, `INTERNET_FQDN_PORT`, `INTERNET_IP_PORT`,
   `NON_GCP_PRIVATE_IP_PORT`, `PRIVATE_SERVICE_CONNECT`, `SERVERLESS`).
   Reuses `assetTypeComputeNetworkEndpointGroup` from
   `extractor_backend_service.go` (the BackendService extractor already
   resolves a backend entry's `group` reference toward this asset type as its
   shared `backend_service_has_backend` edge target; never redeclaring it
   here) and `assetTypeComputeNetwork` / `assetTypeComputeSubnetwork` from
   `extractor_subnetwork.go`. Emits `network_endpoint_group_in_network` and
   `network_endpoint_group_in_subnetwork` edges to the resolved Network and
   Subnetwork; emits no reverse edge toward the enclosing BackendService,
   since that relationship is already emitted, in the opposite direction, by
   `extractor_backend_service.go`. For a SERVERLESS NEG the `serverless_type`
   discriminator is set from sub-object PRESENCE (not from a non-empty name),
   so a URL-mask NEG that carries the `cloudRun`/`appEngine`/`cloudFunction`
   object without a fixed service/function name still classifies; a
   `cloudRun.service` fixed name additionally emits a
   `network_endpoint_group_targets_serverless_service` edge to the
   `run.googleapis.com/Service` resolved in the NEG's own project and region
   (reusing `assetTypeRunService` / `runServiceResourceNamePrefix`, mirroring
   the Eventarc Trigger extractor's Cloud Run edge). An `appEngine`/
   `cloudFunction` reference stays a bounded attribute only, never an edge,
   because an App Engine app id need not equal the project id and a Cloud
   Function reference carries no gen1/gen2 or region qualifier, so neither
   resolves to an exact CAI endpoint from the NEG alone; the
   `urlMask`/`tag`/`version` fields are data-plane routing templates (the same
   treatment as the URL Map extractor's host/path rules) and are never decoded
   into a Go struct field at all. A PRIVATE_SERVICE_CONNECT NEG's
   `pscTargetService` is resolved two mutually-exclusive ways: a Producer
   Service Attachment self-link emits a
   `network_endpoint_group_targets_service_attachment` edge to the
   `compute.googleapis.com/ServiceAttachment` (reusing
   `assetTypeComputeServiceAttachment` and `computeResourceFullNameFromSelfLink`
   from `extractor_forwarding_rule.go`), while a bare Google API hostname names
   no CAI resource and is reduced to a deterministic host fingerprint mirroring
   the Pub/Sub Subscription push-endpoint host-fingerprint treatment. A PSC
   NEG's `pscData.consumerPscAddress` (the allocated VIP) is never decoded
   into a struct field at all, per the Payload Boundaries no-IP-address rule;
   `pscConnectionId` is kept as the raw string the API reports, never parsed
   to a numeric type, since it is a Compute-assigned uint64 that can exceed
   int64/float64 precision. `annotations` is a label-shaped map; only its
   bounded count is surfaced, mirroring the Filestore Instance and Workflows
   Workflow treatment of labels/tags already captured by the collector's
   shared label path.
52. `extractor_security_policy.go` - typed-depth extractor for
   `compute.googleapis.com/SecurityPolicy` (Cloud Armor: policy type
   CLOUD_ARMOR/CLOUD_ARMOR_EDGE/CLOUD_ARMOR_NETWORK, region present only for a
   regional policy, a bounded per-rule priority/action/preview summary and
   rule count, the Adaptive Protection layer-7 DDoS defense enabled posture,
   creation time). Priority is decoded as `json.RawMessage` and parsed with
   `parseFlexibleInt64` (shared with `extractor_firewall.go` /
   `extractor_route.go`), never as a bare int type, because the Compute
   SecurityPolicyRule schema defines priority as a positive value between 0
   and 2147483647 where 0 is the legitimate highest-priority rule, not an
   absent-field sentinel; an absent or null priority is omitted rather than
   fabricated as 0, while a present priority of 0 is kept. Preview is a
   `*bool` so a present `false` (an enforced rule) is distinguishable from an
   absent field. Reuses `assetTypeComputeSecurityPolicy` from the sibling
   Backend Service extractor (`extractor_backend_service.go`), never
   redeclaring it, since that extractor's
   `backend_service_uses_security_policy` /
   `backend_service_uses_edge_security_policy` edges already resolve toward
   this asset type as their target; emits no outbound edges or anchors of its
   own — the same inbound-only edge shape as the Custom IAM Role and SSL
   Certificate extractors. Never reads a rule's match condition,
   network-match packet fields, rate-limit/redirect configuration, or
   description — only the rule's priority, action string, and preview
   posture ever leave the parser.
53. `extractor_dns_policy.go` - typed-depth extractor for
   `dns.googleapis.com/Policy` (inbound-forwarding posture and logging posture
   as explicit tri-state booleans, mirroring the Backend Service extractor's
   `EnableCDN` treatment — the Cloud DNS v1 discovery document defines both as
   plain proto3 booleans that a real CAI page omits at their false default, so
   a `*bool` keeps an explicit false distinct from an absent field; a bounded
   resolvable bound-network count and alternative-name-server count). Emits a
   `dns_policy_applies_to_network` edge to each resolvable
   `networks[].networkUrl` VPC `Network`, reusing `assetTypeComputeNetwork`
   from the sibling VPC Network extractor (`extractor_compute_network.go`),
   never redeclaring it; distinct from `dns.googleapis.com/ManagedZone`
   (`extractor_dns_managed_zone.go`, #28 above): a Policy binds
   inbound-forwarding, logging, and alternative-name-server behavior to a set
   of VPC networks, while a ManagedZone is a DNS namespace with its own
   visibility and peering configuration. The policy's own `description` is
   never decoded into an attribute — free-form operator text, not a bounded
   control-plane field, mirroring the Managed Zone extractor's treatment of
   its own `dnsName` — and alternative name server addresses
   (`alternativeNameServerConfig.targetNameServers[].ipv4Address`/
   `.ipv6Address`) are read only to produce a bounded count; no address value
   ever leaves the parser.
54. `extractor_api_gateway.go` - typed-depth extractor for
   `apigateway.googleapis.com/Gateway` (API Gateway Gateway: display name,
   lifecycle state, region derived from the Gateway's own CAI full resource
   name since the API Gateway v1 Gateway resource carries no separate region
   field of its own, creation/update time, and a fingerprint of the
   `defaultHostname` live DNS name — verified against the live API Gateway v1
   `projects.locations.gateways` REST reference, which reports exactly this
   field set: `name`, `createTime`, `updateTime`, `labels`, `displayName`,
   `apiConfig`, `state`, `defaultHostname`, no additional field). Emits
   `api_gateway_uses_api_config` edge to the resolved
   `apigateway.googleapis.com/ApiConfig` — itself a separately CAI-inventoried
   asset type per the live Cloud Asset Inventory supported-asset-types
   reference, so the reference resolves to a real edge endpoint rather than
   staying attribute-only, mirroring the Spanner Instance extractor's
   InstanceConfig edge; the derivation fails closed, mirroring the Org Policy
   extractor's treatment of its own untrusted resource-name input — an
   already-absolute `apiConfig` value is kept as-is only when it carries the
   exact `//apigateway.googleapis.com/` CAI service prefix (a wrong-domain
   absolute value mints no edge or anchor), and a relative value is prefixed
   only when it matches the documented
   `projects/{project}/locations/global/apis/{api}/configs/{apiConfig}` shape
   (a malformed relative value also mints no edge or anchor). The raw
   `defaultHostname` DNS name is never persisted, only its deterministic
   fingerprint, reusing the Pub/Sub Subscription push-endpoint host-fingerprint
   helper (never redeclaring it); `labels` are not re-declared in typed depth
   since the collector's shared label path already captures and fingerprints
   them.
55. `extractor_interconnect_attachment.go` - typed-depth extractor for
   `compute.googleapis.com/InterconnectAttachment` (region, attachment type
   DEDICATED/PARTNER/PARTNER_PROVIDER/L2_DEDICATED, provisioned bandwidth
   enum, edge availability domain, state, partner ASN decoded via
   `json.RawMessage`/`parseFlexibleInt64` so an absent value — the common case
   for a DEDICATED attachment — is distinguished from a legitimately present
   zero, and creation time); emits `interconnect_attachment_uses_router` to
   the resolved Cloud `Router`, `interconnect_attachment_uses_interconnect` to
   the resolved `Interconnect`, and (only when `l2Forwarding` is present, i.e.
   for a `type: L2_DEDICATED` attachment) `interconnect_attachment_uses_network`
   to the VPC Network named by the nested `l2Forwarding.network` — the
   InterconnectAttachment resource itself carries no top-level `network`
   field, per the live Compute v1 discovery document. Reuses
   `assetTypeComputeInterconnectAttachment` and `assetTypeComputeRouter`, both
   declared by the sibling Cloud Router extractor (`extractor_router.go`,
   #4301), never redeclaring either — that extractor's own
   `router_interface_linked_interconnect_attachment` edge already targets
   `assetTypeComputeInterconnectAttachment` (the attachment itself, not the
   underlying Interconnect). This extractor declares
   `assetTypeComputeInterconnect` here, fresh, for its own
   `interconnect_attachment_uses_interconnect` edge target and for any future
   Interconnect extractor to reuse. `l2Forwarding`'s own
   `tunnelEndpointIpAddress` and `defaultApplianceIpAddress` fields, and its
   per-VLAN-tag `applianceMappings`, are never decoded — every one resolves to
   an IP address. Every candidate/customer/cloud-router IP
   address field the Compute API exposes on this resource
   (`candidateCloudRouterIpAddress`, `candidateCustomerRouterIpAddress`,
   `cloudRouterIpAddress`, `customerRouterIpAddress`, and their IPv6
   counterparts, plus `candidateSubnets` and `ipsecInternalAddresses`) is
   never decoded into Go memory at all — `interconnectAttachmentData`
   declares no struct field for any of them, so encoding/json silently
   ignores those keys during Unmarshal.
56. `extractor_composer_environment.go` - typed-depth extractor for
   `composer.googleapis.com/Environment` (lifecycle state, creation time,
   environment size, resilience mode, Airflow image version, CMEK posture,
   private-environment and private-GKE-endpoint posture, networking connection
   type, workloads-config presence flag, and the fingerprinted node-runtime
   service-account email); `composer_environment_uses_gke_cluster` edge to the
   environment's own GKE `Cluster` (`config.gkeCluster`, reusing
   `assetTypeGKECluster` from the sibling GKE Cluster extractor),
   `composer_environment_uses_network` / `composer_environment_uses_subnetwork`
   edges to `config.nodeConfig.network` / `.subnetwork` (resolved the same way
   the GKE Cluster and Dataproc Cluster extractors resolve their own
   network/subnetwork references),
   `composer_environment_encrypted_by_kms_key` edge to the CMEK `CryptoKey`
   from `config.encryptionConfig.kmsKeyName` (normalized via the shared,
   strict-domain `cmekKeyFullResourceName` helper, which fails closed on an
   already-absolute, wrong-domain CAI full resource name rather than
   accepting it unchanged), and `composer_environment_uses_dag_bucket` edge to
   the DAG/data Cloud Storage bucket parsed from `config.dagGcsPrefix`
   (`gs://{bucket}/dags`, via the shared `gcsBucketFromURI` scheme-guarded
   helper — a non-`gs://` value is rejected rather than mis-parsed) with
   `storageConfig.bucket` as a Composer-3-only fallback. The GKE "default"
   service-account sentinel is never fingerprinted or anchored, mirroring the
   GKE Cluster extractor's own sentinel handling; per-key Airflow config
   override/env-variable values, maintenance-window recurrence, and any
   private-cluster/IP-allocation CIDR value never leave the parser.
57. `extractor_memcache_instance.go` - typed-depth extractor for
   `memcache.googleapis.com/Instance` (Memorystore for Memcached: display
   name, a bounded zone count, node count, per-node cpu count and memory size
   in MB from `nodeConfig`, memcache major version, full version string,
   creation time, state, maintenance version, effective maintenance version,
   and a bounded `memcacheNodes` count); emits the
   `memcache_instance_in_network` edge to the authorized Compute `Network`,
   resolved from `authorizedNetwork` the same way the Memorystore Redis
   Instance extractor (`extractor_redis_instance.go`, #38 above) resolves a
   selfLink or project-qualified/project-less partial. Never reads
   `discoveryEndpoint` or any `memcacheNodes[].host`/`.port` — hostname, IP
   address, or port values, never resource identities; the per-node struct
   declares only `nodeId`, `zone`, and `state` fields, so only the node count
   crosses the redaction boundary.
58. `extractor_certificate_manager_certificate.go` - typed-depth extractor for
   `certificatemanager.googleapis.com/Certificate` (certificate classification
   MANAGED/SELF_MANAGED/MANAGED_IDENTITY derived from which of the mutually
   exclusive `managed`/`selfManaged`/`managedIdentity` blocks is present,
   defaulting to SELF_MANAGED when none is set; scope; managed-provisioning
   state; a bounded managed-domain count; a bounded DNS-authorization count; a
   bounded subject-alternative-name count; a bounded label count; and
   create/update/expiry time). Emits a
   `certificate_manager_certificate_uses_dns_authorization` edge for each
   resolved `managed.dnsAuthorizations[]` entry and a
   `certificate_manager_certificate_uses_issuance_config` edge for a resolved
   `managed.issuanceConfig`, declaring
   `assetTypeCertificateManagerDNSAuthorization` and
   `assetTypeCertificateManagerCertificateIssuanceConfig` here (the first
   extractor to reference either target type, mirroring how the ForwardingRule
   extractor declares proxy-kind asset types for reuse by their own eventual
   typed-depth extractors) and reusing `assetTypeCertificateManagerCertificate`
   and `certificateManagerFullName` from the sibling Target HTTPS Proxy
   extractor (`extractor_target_https_proxy.go`), never redeclaring them. Also
   resolves each `usedBy[].name` entry (`resolveCertManagerCertificateUsedBy`)
   into a `certificate_manager_certificate_used_by_certificate_map_entry` edge
   toward a declared `assetTypeCertificateManagerCertificateMapEntry` (the
   CertificateMap-served path — no other extractor emits an edge for it, since
   CertificateMapEntry has no typed-depth extractor yet) or a
   `certificate_manager_certificate_used_by_target_https_proxy` edge toward the
   directly-referencing TargetHttpsProxy; fails closed on a blank,
   unresolvable, or wrong-domain reference. Never decodes `managed.domains[]`,
   `sanDnsnames`, or the managed-identity `identity` SPIFFE ID into an
   attribute or anchor (the extractor seam carries no redaction key, so —
   mirroring the SSL Certificate extractor's treatment of its own
   `managed.domains[]` and `subjectAlternativeNames` — every domain value and
   the SPIFFE ID are reduced to bounded counts or presence only), and never
   decodes the top-level `pemCertificate`,
   `selfManaged.pemCertificate`/`selfManaged.pemPrivateKey` PEM material, or
   `managed.provisioningIssue`/`managed.authorizationAttemptInfo` free-text
   failure detail.
59. `extractor_alloydb_cluster.go` - typed-depth extractor for
   `alloydb.googleapis.com/Cluster` (display name, uid, state, cluster type,
   database version, subscription type, creation time, CMEK KMS key name,
   encryption type posture, and automated/continuous backup posture — enabled
   flags, location, backup window, time-based retention period or
   quantity-based retention count, and continuous-backup recovery-window
   days). Emits an `alloydb_cluster_in_network` edge to the private Compute
   `Network` from `networkConfig.network` (falling back to the deprecated
   top-level `network` field only when `networkConfig` carries none) and an
   `alloydb_cluster_encrypted_by_kms_key` edge to the CMEK `CryptoKey`,
   reusing `assetTypeComputeNetwork`, `assetTypeKMSCryptoKey`, and the shared
   `cmekKeyFullResourceName` helper rather than redeclaring them. The network
   reference resolves through its own `alloyDBClusterNetworkFullName` (a
   deliberate divergence from the shared `computeFullResourceNameFromSelfLink`
   for the project-qualified case): AlloyDB supports Shared VPC, so the
   cluster's own project id cannot be assumed to be the network's project,
   and a numeric project segment is passed through unresolved rather than
   risking a fabricated edge. Never decodes `initialUser` (the input-only
   database username/password pair) at all — no field of it reaches the
   extractor's input struct — and never decodes
   `encryptionInfo.kmsKeyVersions` (a data-plane key-version identifier list,
   not a control-plane field). An AlloyDB Instance
   (`alloydb.googleapis.com/Instance`) is a separate asset type not covered
   here; its cluster edge will resolve from the Instance side once an
   Instance extractor is added, the same inbound-edge pattern the Bigtable
   Cluster/Instance pair already uses in this package.
60. `extractor_cloud_build_trigger.go` - typed-depth extractor for
   `cloudbuild.googleapis.com/BuildTrigger` (the user-assigned trigger `name`,
   disabled posture, creation time, build-config filename, the API's own
   `eventType` enum, a derived bounded `source_type` —
   `repo`/`github`/`repository_event`/`pubsub`/`webhook`/`source_to_build`/
   `manual` — from whichever event-mechanism field the trigger carries,
   `includeBuildLogs` posture, `approvalConfig.approvalRequired` posture,
   bounded `includedFiles`/`ignoredFiles`/`tags` counts, and the fingerprinted
   trigger service account; `tags` is free-form user text, unlike the shared
   `labels` map the collector already fingerprints, so only its count is kept,
   never the tag strings). `source_type` checks the SCM-event discriminators
   (`triggerTemplate`, `github`, `repositoryEventConfig`) first, then the
   invocation-mechanism discriminators (`pubsubConfig`, `webhookConfig`),
   before falling back to `sourceToBuild` presence or the
   `eventType`-derived `manual` value — per the live Cloud Build v1 discovery
   document, `sourceToBuild` is the build-source reference used only by
   Webhook/Pub-Sub/Manual/Cron triggers, not a distinct firing mechanism, so it
   must never shadow a coexisting `pubsubConfig`/`webhookConfig` (a real
   Pub/Sub or webhook trigger commonly carries both). Reuses
   `assetTypeCloudBuildTrigger` and `assetTypeSourceRepo` (both declared by the
   sibling Cloud Build Build extractor, `extractor_cloud_build.go`, never
   redeclared here), and the shared `cloudBuildSourceRepoFullName` /
   `cloudBuildServiceAccountEmail` / `cloudBuildEdge` helpers it also declares.
   Emits two independent edges: `trigger_source_repo` to the Cloud Source
   Repositories `Repository` for a `triggerTemplate`-sourced trigger, resolving
   a bare `projectId` against the trigger's own project the same way the Build
   extractor's `repoSource.projectId` default resolves (a project id is parsed
   from the trigger's own CAI full resource name via the domain-agnostic
   `eventarcProjectLocation` helper from `extractor_eventarc_trigger.go`, never
   a Cloud-Build-specific parser, since both extractors need only the generic
   `projects/<id>/locations/<id>` segments); and `trigger_source_repository_link`
   to the newly-declared `assetTypeDeveloperConnectGitRepositoryLink`
   (`developerconnect.googleapis.com/GitRepositoryLink`, verified against the
   live Cloud Asset Inventory supported-asset-types reference) for a
   `sourceToBuild.repository` reference, resolved unconditionally alongside
   whichever event-mechanism field fires the build rather than as a
   mutually-exclusive alternative. Cloud Build's `GitRepoSource.repository`
   value is always shaped as
   `projects/<p>/locations/<l>/connections/<c>/repositories/<r>` regardless of
   the underlying `repoType` (Cloud Source Repositories, GitHub, GitLab, or
   Bitbucket connected through Developer Connect), never a
   `sourcerepo.googleapis.com` name, so `developerConnectGitRepositoryLinkFullName`
   is a distinct, dedicated resolver from `cloudBuildSourceRepoFullName` — never
   reused across the two asset types — and fails closed on any value that does
   not match the documented eight-segment shape or carry the exact Developer
   Connect CAI service prefix. A GitHub, GitLab Enterprise, or Bitbucket Server
   source named directly by `github` (not through Developer Connect) has no
   CAI-resolvable target asset type in this graph, so it mints no edge or raw
   owner/name attribute — only the bounded `source_type` enum records which
   mechanism fires the trigger. Never reads the trigger's own `build` template,
   `substitutions`, the free-text CEL `filter`, `webhookConfig.secret` (a
   Secret Manager version reference used only to validate inbound webhook
   signatures), GitHub/GitLab/Bitbucket push/pull-request branch/tag regex
   detail, `gitFileSource`, or `sourceToBuild.uri`/`githubEnterpriseConfig`/
   `bitbucketServerConfig`.
61. `extractor_spanner_database.go` - typed-depth extractor for
   `spanner.googleapis.com/Database` (#4622, follow-up to the Spanner Instance
   extractor, #46 above / #4317): lifecycle state (including
   `READY_OPTIMIZING`, a database still being optimized after a restore),
   database dialect GOOGLE_STANDARD_SQL/POSTGRESQL, version-retention period,
   earliest-version time, create time, default leader region, and
   drop-protection posture as an explicit tri-state (`*bool`, mirroring the
   Backend Service extractor's `EnableCDN` treatment — a present `false` is
   kept). Emits `spanner_database_in_instance` to the parent
   `spanner.googleapis.com/Instance`, derived from the database's own CAI full
   resource name (`.../instances/<i>/databases/<d>`) since the Database
   resource carries no separate parent-instance field of its own — mirroring
   the Bigtable Cluster extractor's parent-Instance derivation. The derivation
   matches the full string against an anchored `spannerDatabaseFullNamePattern`
   regular expression rather than a loose
   `Index("/databases/")`+`Contains("/instances/")` substring check: the loose
   form would also accept an extra segment between the instance and the
   databases marker (`.../instances/<i>/extra/databases/<d>`), silently
   deriving a wrong parent, or a trailing segment after the database id, so
   any full resource name that does not carry the exact documented shape fails
   closed (no edge, no anchor). Emits `spanner_database_encrypted_by_kms_key`
   to every CMEK `CryptoKey` resolved from `encryptionConfig` — both the
   singular `kmsKeyName` and the plural `kmsKeyNames[]` are decoded (they are
   independent fields per the discovery document, not a
   deprecated/replacement pair: `kmsKeyNames[]` covers a multi-region instance
   configuration that needs more than one regional key), each candidate
   normalized through the shared `cmekKeyFullResourceName` helper (never a
   bespoke copy) and deduplicated via `dedupeNonEmpty` so an overlap between
   the singular and plural fields, or within `kmsKeyNames[]` itself, never
   mints a duplicate edge; reuses `assetTypeKMSCryptoKey` from the sibling
   BigQuery Table extractor, never redeclaring it. This completes the
   ownership split the Instance extractor deliberately deferred: CMEK and
   per-database detail live on the Database resource, not the Instance, the
   same way the BigQuery Table extractor owns the Table→Dataset edge rather
   than the Dataset enumerating its Tables. Never decodes `encryptionInfo`
   (Cloud KMS key-version usage detail, not a key resource name — only
   `encryptionConfig.kmsKeyName`/`kmsKeyNames[]` are the CMEK edge/anchor
   source of truth) or `restoreInfo` (the backup/source-database reference for
   a restored database) — neither is declared as a struct field at all, so
   neither is ever decoded into Go memory — verified against the live Spanner
   v1 discovery document's Database schema.
