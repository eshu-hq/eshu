package rules

import "testing"

func TestAWSCloudRuntimeDriftRulePackValidates(t *testing.T) {
	t.Parallel()

	pack := AWSCloudRuntimeDriftRulePack()
	if err := pack.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
	if pack.Name != AWSCloudRuntimeDriftPackName {
		t.Fatalf("pack.Name = %q, want %q", pack.Name, AWSCloudRuntimeDriftPackName)
	}
}

func TestFirstPartyRulePacksIncludeAWSCloudRuntimeDrift(t *testing.T) {
	t.Parallel()

	for _, pack := range FirstPartyRulePacks() {
		if pack.Name == AWSCloudRuntimeDriftPackName {
			return
		}
	}
	t.Fatalf("FirstPartyRulePacks() missing %q", AWSCloudRuntimeDriftPackName)
}
