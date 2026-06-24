// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import "time"

// GCP secrets/IAM provider and principal-type constants, mirroring the AWS
// constants for the GCP IAM source-fact family (issue #2347).
const (
	// ProviderGCPIAM tags GCP IAM secrets/IAM source facts, parallel to
	// ProviderAWSIAM.
	ProviderGCPIAM = "gcp_iam"
	// PrincipalTypeGCPServiceAccount is the only GCP IAM principal type the
	// principal/permission mirror admits today: a service-account grantee
	// observed in a Cloud Asset Inventory IAM binding. Human/group/public members
	// are not identities that form trust chains.
	PrincipalTypeGCPServiceAccount = "gcp_service_account"
	// GCPMemberClassServiceAccount is the CAI member class for a service account,
	// matching gcpcloud.MemberClass output.
	GCPMemberClassServiceAccount = "serviceAccount"
	// GCPWorkloadIdentityMemberClassServiceAccount identifies a GKE Workload
	// Identity member in a ServiceAccount IAM binding.
	GCPWorkloadIdentityMemberClassServiceAccount = "gke_serviceAccount"
	// GCPImpersonationModeWorkloadIdentity marks roles/iam.workloadIdentityUser.
	GCPImpersonationModeWorkloadIdentity = "workload_identity"
	// GCPImpersonationModeTokenCreator marks roles/iam.serviceAccountTokenCreator.
	GCPImpersonationModeTokenCreator = "token_creator"
	// GCPImpersonationModeServiceAccountUser marks roles/iam.serviceAccountUser.
	GCPImpersonationModeServiceAccountUser = "service_account_user"
)

// GCPEnvelopeContext carries the scope/generation contract fields for GCP
// secrets/IAM source facts. It mirrors EnvelopeContext but uses GCP-native
// identifiers (project id, location bucket) so the emitted payload speaks GCP
// rather than AWS account/region.
type GCPEnvelopeContext struct {
	ProjectID           string
	LocationBucket      string
	ScopeID             string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
}

// GCPPrincipalObservation is one GCP service-account principal observed as a
// grantee in a CAI IAM binding. Identity is the redaction-safe member
// fingerprint (the caller computes it with the same scheme used for the binding
// member), never a raw email.
type GCPPrincipalObservation struct {
	Context              GCPEnvelopeContext
	PrincipalFingerprint string
	MemberClass          string
	SourceRecordID       string
	SourceURI            string
}

// GCPTrustPolicyObservation is one IAM binding on a GCP ServiceAccount that
// grants another principal an act-as or token-minting role. Target identity is
// the same service-account member fingerprint used by GCP principal/permission
// facts; the raw target email and trusted member string are never stored.
type GCPTrustPolicyObservation struct {
	Context                               GCPEnvelopeContext
	TargetPrincipalFingerprint            string
	TargetServiceAccountEmailDigest       string
	TargetServiceAccountCloudResourceUID  string
	TrustedMemberFingerprint              string
	TrustedMemberClass                    string
	Role                                  string
	ImpersonationMode                     string
	GCPWorkloadIdentitySubjectFingerprint string
	GCPWorkloadIdentityMemberClass        string
	ConditionPresent                      bool
	ConditionFingerprint                  string
	SourceRecordID                        string
	SourceURI                             string
}

// GCPPermissionPolicyObservation is one GCP IAM permission grant: a
// service-account principal granted a role on a resource. It mirrors the AWS
// permission policy observation. Resource identity is the CAI full resource
// name (a stable, non-PII provider identifier); secret-resource and broad-role
// classifications are precomputed booleans the reducer uses for posture.
type GCPPermissionPolicyObservation struct {
	Context              GCPEnvelopeContext
	PrincipalFingerprint string
	PrincipalType        string
	Role                 string
	ResourceFullName     string
	ResourceAssetType    string
	ResourceIsSecret     bool
	BroadRole            bool
	ConditionPresent     bool
	ConditionFingerprint string
	SourceRecordID       string
	SourceURI            string
}
