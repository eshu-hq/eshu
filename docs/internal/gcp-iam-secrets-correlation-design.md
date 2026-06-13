# GCP IAM correlation into secrets-IAM read models (#2347, #2369)

## Why

GCP emitted `gcp_iam_policy_observation` (role→member bindings) but nothing
consumed it into the secrets/IAM read models. AWS feeds the secrets/IAM
trust-chain from distinct collector source facts (`aws_iam_principal`,
`aws_iam_trust_policy`, `aws_iam_permission_policy`). This change mirrors the AWS
principal + permission layers for GCP, then adds the ServiceAccount
impersonation trust layer needed for GKE Workload Identity.

## Scope of this slice

- **Principal layer** — `gcp_iam_principal`: a service-account grantee observed
  in a CAI IAM binding. Identity = the redaction-safe member fingerprint.
- **Permission layer** — `gcp_iam_permission_policy`: a `(principal, role,
  resource)` grant. Carries `resource_is_secret` and `broad_role` flags.
- **Reducer consumption**: the secrets/IAM trust-chain builder indexes both by
  the shared principal fingerprint and projects privilege-posture observations.
- **Trust layer** — `gcp_iam_trust_policy`: IAM bindings on
  `iam.googleapis.com/ServiceAccount` resources that grant
  `serviceAccountTokenCreator`, `serviceAccountUser`, or `workloadIdentityUser`.
  The target ServiceAccount email is used only to compute the target member
  fingerprint and email digest.
- **GKE annotation layer** — `k8s_gcp_workload_identity_binding`: Kubernetes
  ServiceAccount annotations joined to an explicitly configured workload pool.
- **Exact GCP path**: the reducer admits a workload→Kubernetes ServiceAccount→GCP
  ServiceAccount path only when the annotation digest/subject, GCP trust fact,
  GCP principal, and a GCP Secret Manager `secretmanager.versions.access` grant
  all agree.

## Model

- Collector emission: `go/internal/collector/gcpcloud/gcp_secrets_iam.go`
  derives the principal + permission facts from the same CAI bindings that
  produce `gcp_iam_policy_observation`. Only `serviceAccount`-class members
  become principals; human/group/public members are not chain identities. The
  member fingerprint reuses `gcpcloud.FingerprintMember`, so the principal,
  permission, and binding-observation fingerprints align by construction.
- Envelope builders: `go/internal/collector/secretsiam/gcp_envelope.go` and
  `gcp_trust_envelope.go` (`NewGCPPrincipalEnvelope`,
  `NewGCPTrustPolicyEnvelope`, `NewGCPPermissionPolicyEnvelope`), GCP-native
  payload (`provider=gcp_iam`, `project_id`), covered by the package leakage
  guard.
- Fact kinds: `facts.GCPIAMPrincipalFactKind`,
  `facts.GCPIAMTrustPolicyFactKind`, `facts.GCPIAMPermissionPolicyFactKind`,
  and `facts.KubernetesGCPWorkloadIdentityBindingFactKind` are in the
  `SecretsIAMFactKinds()` set, so projector triggers and the evidence loader
  pick them up with no broad active-table scan.
- Reducer: `secretsIAMGCPGrantObservations`
  (`go/internal/reducer/secrets_iam_trust_chain_gcp.go`) emits
  `gcp_service_account_secret_access` and `gcp_service_account_broad_role`
  privilege-posture observations for standing Secret Manager resource grants and
  broad roles, and `secretsIAMGCPExactChainsForServiceAccount` emits exact GCP
  identity chains and GCP secret access paths for bounded roles that include
  `secretmanager.versions.access`.
- Evidence loader: `principal_fingerprint`, `target_principal_fingerprint`,
  `gcp_service_account_email_digest`, `target_service_account_email_digest`, and
  `gcp_workload_identity_subject_fingerprint` anchors expand the bounded packet
  across active generations.

## Invariants (correlation truth)

- **No fabricated identity**: a permission grant with no matching principal fact
  produces no observation.
- **No silent drop**: a narrow non-secret grant is consumed (indexed/joined) but
  legitimately yields no posture — the same as a benign AWS trust.
- **No raw identity**: only member fingerprints, target email digests, bounded
  roles/modes, and CAI resource names for permission resources are stored. Raw
  service-account emails, workload pools, namespaces, Kubernetes ServiceAccount
  names, and IAM member strings stay out of trust and GKE binding payloads.
- **Join-key consistency**: the principal fingerprint is identical across the
  principal fact, the permission fact, and the `gcp_iam_policy_observation`
  member fingerprint. The trust layer uses the same target principal fingerprint
  and the same target email digest as the Kubernetes GKE binding.

## Evidence

No-Regression Evidence: `go test ./internal/reducer -run 'GCP.*(Grant|Trust|Secret)|GCPSecret|GCPBroad|GCPNarrow'`,
`go test ./internal/collector/secretsiam -run GCP`, `go test
./internal/collector/gcpcloud -run 'GCP|ServiceAccountEmail'`, `go test
./internal/collector/kuberneteslive ./cmd/collector-kubernetes-live -run
'GCP|WorkloadIdentity'`, `go test ./internal/facts -run SecretsIAM`, `go test
./internal/storage/postgres -run 'GCPWorkloadIdentity|SecretsIAM'`. Bench
`BenchmarkSecretsIAMGCPGrantObservations` = 12.7 ms/op for 4,000 grants over
2,000 principals — bounded O(P+G), no new Cypher.

Observability Evidence: GCP grant observations flow through the existing
`eshu_dp_secrets_iam_posture_observations_total` counter (bounded `risk_type` /
`severity` labels).

## Follow-up

- Graph promotion for GCP secrets/IAM identity hops remains separate. The read
  models carry a redaction-safe GCP ServiceAccount CloudResource uid hint, but
  this design writes no graph labels, graph edges, Cypher, DDL, API route, MCP
  tool, Helm value, or runtime service.
