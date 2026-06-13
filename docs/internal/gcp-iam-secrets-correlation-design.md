# GCP IAM correlation into secrets-IAM read models (#2347)

## Why

GCP emitted `gcp_iam_policy_observation` (role→member bindings) but nothing
consumed it into the secrets/IAM read models. AWS feeds the secrets/IAM
trust-chain from distinct collector source facts (`aws_iam_principal`,
`aws_iam_trust_policy`, `aws_iam_permission_policy`). This change mirrors the AWS
principal + permission layers for GCP.

## Scope of this slice

- **Principal layer** — `gcp_iam_principal`: a service-account grantee observed
  in a CAI IAM binding. Identity = the redaction-safe member fingerprint.
- **Permission layer** — `gcp_iam_permission_policy`: a `(principal, role,
  resource)` grant. Carries `resource_is_secret` and `broad_role` flags.
- **Reducer consumption**: the secrets/IAM trust-chain builder indexes both by
  the shared principal fingerprint and projects privilege-posture observations.

The **trust layer** (impersonation / Workload Identity, the
workload→service-account→secret chain) is tracked separately in **#2369**
because it needs service-account email extraction and a K8s↔GCP SA join that the
collector does not parse today.

## Model

- Collector emission: `go/internal/collector/gcpcloud/gcp_secrets_iam.go`
  derives the principal + permission facts from the same CAI bindings that
  produce `gcp_iam_policy_observation`. Only `serviceAccount`-class members
  become principals; human/group/public members are not chain identities. The
  member fingerprint reuses `gcpcloud.FingerprintMember`, so the principal,
  permission, and binding-observation fingerprints align by construction.
- Envelope builders: `go/internal/collector/secretsiam/gcp_envelope.go`
  (`NewGCPPrincipalEnvelope`, `NewGCPPermissionPolicyEnvelope`), GCP-native
  payload (`provider=gcp_iam`, `project_id`), covered by the package leakage
  guard.
- Fact kinds: `facts.GCPIAMPrincipalFactKind`, `facts.GCPIAMPermissionPolicyFactKind`
  in the `SecretsIAMFactKinds()` set, so the projector trigger and the evidence
  loader pick them up with no extra wiring.
- Reducer: `secretsIAMGCPGrantObservations`
  (`go/internal/reducer/secrets_iam_trust_chain_gcp.go`) emits
  `gcp_service_account_secret_access` and `gcp_service_account_broad_role`
  privilege-posture observations.
- Evidence loader: the `principal_fingerprint` join anchor expands a principal
  fact to its grants across active generations.

## Invariants (correlation truth)

- **No fabricated identity**: a permission grant with no matching principal fact
  produces no observation.
- **No silent drop**: a narrow non-secret grant is consumed (indexed/joined) but
  legitimately yields no posture — the same as a benign AWS trust.
- **No raw identity**: only the member fingerprint, role, and CAI resource name
  are stored; never a raw service-account email. The leakage guard enforces this.
- **Join-key consistency**: the principal fingerprint is identical across the
  principal fact, the permission fact, and the `gcp_iam_policy_observation`
  member fingerprint.

## Evidence

No-Regression Evidence: `go test ./internal/reducer -run 'GCP.*Grant|GCPSecret|GCPBroad|GCPNarrow'`,
`go test ./internal/collector/secretsiam -run GCP`, `go test
./internal/collector/gcpcloud -run GCPSecretsIAM`, `go test ./internal/facts
-run SecretsIAM`, `go test ./internal/storage/postgres -run SecretsIAM`. Bench
`BenchmarkSecretsIAMGCPGrantObservations` = 12.7 ms/op for 4,000 grants over
2,000 principals — bounded O(P+G), no new Cypher.

Observability Evidence: GCP grant observations flow through the existing
`eshu_dp_secrets_iam_posture_observations_total` counter (bounded `risk_type` /
`severity` labels).

## Follow-up

- #2369 — GCP impersonation / Workload-Identity trust layer; completes the
  graph-projected workload→service-account→secret chain.
