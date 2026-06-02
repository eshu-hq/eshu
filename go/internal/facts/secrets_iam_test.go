package facts

import "testing"

func TestSecretsIAMFactKindsAndSchemaVersions(t *testing.T) {
	want := []string{
		AWSIAMPrincipalFactKind,
		AWSIAMTrustPolicyFactKind,
		AWSIAMPermissionPolicyFactKind,
		AWSIAMPolicyAttachmentFactKind,
		AWSIAMPermissionBoundaryFactKind,
		AWSIAMInstanceProfileFactKind,
		AWSIAMAccessAnalyzerFindingFactKind,
		KubernetesServiceAccountFactKind,
		KubernetesRBACRoleFactKind,
		KubernetesRBACBindingFactKind,
		KubernetesWorkloadIdentityUseFactKind,
		KubernetesServiceAccountTokenPostureFactKind,
		EKSIRSAAnnotationFactKind,
		EKSPodIdentityAssociationFactKind,
		SecretsIAMCoverageWarningFactKind,
	}

	got := SecretsIAMFactKinds()
	if len(got) != len(want) {
		t.Fatalf("SecretsIAMFactKinds len = %d, want %d: %#v", len(got), len(want), got)
	}
	for index, kind := range want {
		if got[index] != kind {
			t.Fatalf("SecretsIAMFactKinds[%d] = %q, want %q", index, got[index], kind)
		}
		version, ok := SecretsIAMSchemaVersion(kind)
		if !ok || version != SecretsIAMSchemaVersionV1 {
			t.Fatalf("SecretsIAMSchemaVersion(%q) = %q, %v; want %q, true", kind, version, ok, SecretsIAMSchemaVersionV1)
		}
	}
	if version, ok := SecretsIAMSchemaVersion("secrets_iam.unknown"); ok || version != "" {
		t.Fatalf("SecretsIAMSchemaVersion(unknown) = %q, %v; want empty false", version, ok)
	}

	got[0] = "mutated"
	if SecretsIAMFactKinds()[0] != AWSIAMPrincipalFactKind {
		t.Fatal("SecretsIAMFactKinds returned mutable backing slice")
	}
}
