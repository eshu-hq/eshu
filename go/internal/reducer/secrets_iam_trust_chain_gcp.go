package reducer

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// GCP IAM grant posture risk types, the bounded classifications the GCP IAM
// permission grants produce as secrets/IAM privilege-posture observations
// (issue #2347). They mirror how AWS wildcard trusts become posture observations
// rather than exact chains: a GCP grant is explicit evidence of standing access,
// surfaced as posture until the impersonation/Workload-Identity trust layer
// (#2369) connects a workload to the service account.
const (
	gcpRiskSecretAccessGrant = "gcp_service_account_secret_access"
	gcpRiskBroadRoleGrant    = "gcp_service_account_broad_role"
)

// secretsIAMGCPGrantObservations projects the GCP IAM principal/permission
// source facts into secrets/IAM privilege-posture observations. A grant is
// surfaced when it carries standing privilege worth an operator's attention:
// direct access to a Secret Manager secret (resource_is_secret) or a broad
// primitive role (broad_role). A service-account principal with only narrow,
// non-secret grants is still consumed (indexed and joined) but yields no
// observation, exactly as a benign AWS trust yields none.
//
// The subject is the redaction-safe service-account fingerprint shared by the
// principal and permission facts, so the observation never leaks a raw member
// identity. Each observation requires a matching principal fact for the grant's
// fingerprint, so an orphan permission fact does not fabricate an identity.
func secretsIAMGCPGrantObservations(index secretsIAMIndex) []SecretsIAMPrivilegePostureObservation {
	if len(index.gcpPermissions) == 0 {
		return nil
	}

	fingerprints := make([]string, 0, len(index.gcpPermissions))
	for fingerprint := range index.gcpPermissions {
		fingerprints = append(fingerprints, fingerprint)
	}
	sort.Strings(fingerprints)

	var observations []SecretsIAMPrivilegePostureObservation
	for _, fingerprint := range fingerprints {
		if len(index.gcpPrincipals[fingerprint]) == 0 {
			// No principal fact for this grant's identity: do not invent an
			// identity from a permission fact alone.
			continue
		}
		principalFactID := index.gcpPrincipals[fingerprint][0].FactID
		for _, grant := range index.gcpPermissions[fingerprint] {
			riskType, ok := gcpGrantRiskType(grant)
			if !ok {
				continue
			}
			observations = append(observations, SecretsIAMPrivilegePostureObservation{
				ObservationID: secretsIAMID(
					"privilege_posture_observation",
					riskType,
					fingerprint,
					payloadString(grant.Payload, "role"),
					grant.FactID,
				),
				RiskType:           riskType,
				Severity:           "high",
				State:              SecretsIAMTrustChainStateExact,
				Confidence:         "exact",
				SubjectFingerprint: fingerprint,
				Reason:             gcpGrantReason(riskType),
				EvidenceFactIDs:    uniqueSortedStrings([]string{principalFactID, grant.FactID}),
			})
		}
	}
	return observations
}

// gcpGrantRiskType classifies one GCP permission grant into a bounded posture
// risk type, preferring the secret-access classification when a broad role is
// also granted directly on a secret resource.
func gcpGrantRiskType(grant facts.Envelope) (string, bool) {
	if payloadBool(grant.Payload, "resource_is_secret") {
		return gcpRiskSecretAccessGrant, true
	}
	if payloadBool(grant.Payload, "broad_role") {
		return gcpRiskBroadRoleGrant, true
	}
	return "", false
}

func gcpGrantReason(riskType string) string {
	switch riskType {
	case gcpRiskSecretAccessGrant:
		return "GCP service account has a direct IAM role grant on a Secret Manager secret"
	case gcpRiskBroadRoleGrant:
		return "GCP service account holds a broad primitive role (owner/editor)"
	default:
		return "GCP service account IAM grant"
	}
}
