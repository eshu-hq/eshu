package runtime

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGovernanceProofDisablesGeneratedIdentityBootstrap(t *testing.T) {
	t.Parallel()

	content := readRepositoryFile(t, "../../..", "deploy/helm/eshu/ci/governance-two-team-k8s.values.yaml")
	var values map[string]any
	if err := yaml.Unmarshal([]byte(content), &values); err != nil {
		t.Fatalf("parse governance-two-team-k8s.values.yaml: %v", err)
	}

	api := helmMap(values["api"])
	env := helmMap(api["env"])
	if got, _ := env["ESHU_AUTH_BOOTSTRAP_MODE"].(string); got != "disabled" {
		t.Fatalf("api.env.ESHU_AUTH_BOOTSTRAP_MODE = %q, want disabled: the proof uses a static shared API key and must not require a generated-credential encryption key", got)
	}
}
