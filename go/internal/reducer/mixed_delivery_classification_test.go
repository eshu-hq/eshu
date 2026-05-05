package reducer

import "testing"

func TestInferWorkloadClassificationMixedServiceAndCloudFormationPrefersService(t *testing.T) {
	t.Parallel()

	candidate := WorkloadCandidate{
		RepoName:      "service-edge-api",
		ResourceKinds: []string{"deployment", "service"},
		Provenance: []string{
			"k8s_resource",
			"dockerfile_runtime",
			"jenkins_pipeline",
			"github_actions_workflow",
			"cloudformation_template",
		},
	}

	if got := InferWorkloadClassification(candidate); got != "service" {
		t.Fatalf("InferWorkloadClassification() = %q, want service for mixed-delivery service repo", got)
	}
}

func TestInferWorkloadClassificationCloudFormationWithControllerSignalsStaysInfrastructure(t *testing.T) {
	t.Parallel()

	candidate := WorkloadCandidate{
		RepoName: "legacy-automation-stack",
		Provenance: []string{
			"cloudformation_template",
			"jenkins_pipeline",
			"github_actions_workflow",
		},
	}

	if got := InferWorkloadClassification(candidate); got != "infrastructure" {
		t.Fatalf("InferWorkloadClassification() = %q, want infrastructure for CloudFormation/controller-only repo", got)
	}
}
