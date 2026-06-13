package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func gcpPrincipalFact(fingerprint string) facts.Envelope {
	return facts.Envelope{
		FactID:   "principal-" + fingerprint,
		FactKind: facts.GCPIAMPrincipalFactKind,
		Payload: map[string]any{
			"principal_fingerprint": fingerprint,
			"principal_type":        "gcp_service_account",
			"project_id":            "demo-proj",
		},
	}
}

func gcpPermissionFact(factID, fingerprint, role string, isSecret, broad bool) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.GCPIAMPermissionPolicyFactKind,
		Payload: map[string]any{
			"principal_fingerprint":       fingerprint,
			"role":                        role,
			"resource_full_resource_name": "//res/" + role,
			"resource_is_secret":          isSecret,
			"broad_role":                  broad,
		},
	}
}

func findGCPObservation(models SecretsIAMTrustChainReadModels, riskType string) (SecretsIAMPrivilegePostureObservation, bool) {
	for _, obs := range models.PrivilegePostureObservations {
		if obs.RiskType == riskType {
			return obs, true
		}
	}
	return SecretsIAMPrivilegePostureObservation{}, false
}

func TestGCPSecretAccessGrantProducesPostureObservation(t *testing.T) {
	t.Parallel()

	const fp = "sha256:svc-a"
	models := BuildSecretsIAMTrustChainReadModels([]facts.Envelope{
		gcpPrincipalFact(fp),
		gcpPermissionFact("perm-1", fp, "roles/secretmanager.secretAccessor", true, false),
	})

	obs, ok := findGCPObservation(models, gcpRiskSecretAccessGrant)
	if !ok {
		t.Fatalf("expected a %s posture observation, got %+v", gcpRiskSecretAccessGrant, models.PrivilegePostureObservations)
	}
	if obs.State != SecretsIAMTrustChainStateExact {
		t.Fatalf("state = %q, want exact", obs.State)
	}
	if obs.SubjectFingerprint != fp {
		t.Fatalf("subject = %q, want %q", obs.SubjectFingerprint, fp)
	}
	if len(obs.EvidenceFactIDs) != 2 {
		t.Fatalf("evidence fact ids = %v, want principal + permission", obs.EvidenceFactIDs)
	}
}

func TestGCPBroadRoleGrantProducesPostureObservation(t *testing.T) {
	t.Parallel()

	const fp = "sha256:svc-b"
	models := BuildSecretsIAMTrustChainReadModels([]facts.Envelope{
		gcpPrincipalFact(fp),
		gcpPermissionFact("perm-2", fp, "roles/owner", false, true),
	})

	if _, ok := findGCPObservation(models, gcpRiskBroadRoleGrant); !ok {
		t.Fatalf("expected a %s posture observation, got %+v", gcpRiskBroadRoleGrant, models.PrivilegePostureObservations)
	}
}

func TestGCPNarrowNonSecretGrantProducesNoObservation(t *testing.T) {
	t.Parallel()

	const fp = "sha256:svc-c"
	models := BuildSecretsIAMTrustChainReadModels([]facts.Envelope{
		gcpPrincipalFact(fp),
		gcpPermissionFact("perm-3", fp, "roles/storage.objectViewer", false, false),
	})

	if len(models.PrivilegePostureObservations) != 0 {
		t.Fatalf("narrow non-secret grant must not produce posture, got %+v", models.PrivilegePostureObservations)
	}
}

func TestGCPGrantWithoutPrincipalIsNotFabricated(t *testing.T) {
	t.Parallel()

	// A permission fact with no matching principal fact must not fabricate an
	// identity-grant observation.
	models := BuildSecretsIAMTrustChainReadModels([]facts.Envelope{
		gcpPermissionFact("perm-4", "sha256:orphan", "roles/secretmanager.secretAccessor", true, false),
	})

	if len(models.PrivilegePostureObservations) != 0 {
		t.Fatalf("orphan grant must not produce posture, got %+v", models.PrivilegePostureObservations)
	}
}
