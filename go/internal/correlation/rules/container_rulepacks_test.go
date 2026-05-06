package rules

import "testing"

func TestContainerRulePacksShipFirstPartyCoverageForSupportedFamilies(t *testing.T) {
	t.Parallel()

	packs := []RulePack{
		DockerfileRulePack(),
		DockerComposeRulePack(),
		GitHubActionsRulePack(),
		JenkinsRulePack(),
		HelmRulePack(),
		ArgoCDRulePack(),
		KustomizeRulePack(),
		TerraformConfigRulePack(),
		CloudFormationRulePack(),
	}

	for _, pack := range packs {
		pack := pack
		t.Run(pack.Name, func(t *testing.T) {
			t.Parallel()

			if err := pack.Validate(); err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
			if !containsRuleKind(pack.Rules, RuleKindExtractKey) {
				t.Fatalf("pack %q missing extract_key rule", pack.Name)
			}
			if !containsRuleKind(pack.Rules, RuleKindMatch) {
				t.Fatalf("pack %q missing match rule", pack.Name)
			}
			if !containsRuleKind(pack.Rules, RuleKindAdmit) {
				t.Fatalf("pack %q missing admit rule", pack.Name)
			}
			if !containsRuleKind(pack.Rules, RuleKindExplain) {
				t.Fatalf("pack %q missing explain rule", pack.Name)
			}
			if pack.MinAdmissionConfidence < 0.75 {
				t.Fatalf("pack %q MinAdmissionConfidence = %v, want >= 0.75", pack.Name, pack.MinAdmissionConfidence)
			}
			if len(pack.RequiredEvidence) == 0 {
				t.Fatalf("pack %q missing required evidence constraints", pack.Name)
			}
		})
	}
}

func TestContainerRulePacksReturnsStableOrder(t *testing.T) {
	t.Parallel()

	got := ContainerRulePacks()
	want := []string{
		"dockerfile",
		"docker_compose",
		"github_actions",
		"jenkins",
		"helm",
		"argocd",
		"kustomize",
		"terraform_config",
		"cloudformation",
	}
	if len(got) != len(want) {
		t.Fatalf("len(ContainerRulePacks()) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Fatalf("ContainerRulePacks()[%d].Name = %q, want %q", i, got[i].Name, want[i])
		}
	}
}

func TestFirstPartyRulePacksShipCoverageForSupportedFamilies(t *testing.T) {
	t.Parallel()

	got := FirstPartyRulePacks()
	want := []string{
		"dockerfile",
		"docker_compose",
		"github_actions",
		"jenkins",
		"helm",
		"argocd",
		"kustomize",
		"terraform_config",
		"terragrunt",
		"ansible",
		"cloudformation",
	}
	if len(got) != len(want) {
		t.Fatalf("len(FirstPartyRulePacks()) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Fatalf("FirstPartyRulePacks()[%d].Name = %q, want %q", i, got[i].Name, want[i])
		}
	}
}

func containsRuleKind(rules []Rule, want RuleKind) bool {
	for _, rule := range rules {
		if rule.Kind == want {
			return true
		}
	}
	return false
}
