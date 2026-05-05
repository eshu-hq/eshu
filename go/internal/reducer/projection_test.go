package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestInferWorkloadKindCronJob(t *testing.T) {
	t.Parallel()
	if got := InferWorkloadKind("my-cron-job", nil); got != "cronjob" {
		t.Fatalf("InferWorkloadKind() = %q, want cronjob", got)
	}
}

func TestInferWorkloadKindWorker(t *testing.T) {
	t.Parallel()
	if got := InferWorkloadKind("data-worker", nil); got != "worker" {
		t.Fatalf("InferWorkloadKind() = %q, want worker", got)
	}
}

func TestInferWorkloadKindConsumer(t *testing.T) {
	t.Parallel()
	if got := InferWorkloadKind("event-consumer", nil); got != "consumer" {
		t.Fatalf("InferWorkloadKind() = %q, want consumer", got)
	}
}

func TestInferWorkloadKindBatch(t *testing.T) {
	t.Parallel()
	if got := InferWorkloadKind("nightly-batch", nil); got != "batch" {
		t.Fatalf("InferWorkloadKind() = %q, want batch", got)
	}
}

func TestInferWorkloadKindServiceFromResourceKinds(t *testing.T) {
	t.Parallel()
	if got := InferWorkloadKind("my-api", []string{"Deployment", "Service"}); got != "service" {
		t.Fatalf("InferWorkloadKind() = %q, want service", got)
	}
}

func TestInferWorkloadKindDefaultService(t *testing.T) {
	t.Parallel()
	if got := InferWorkloadKind("my-app", nil); got != "service" {
		t.Fatalf("InferWorkloadKind() = %q, want service", got)
	}
}

func TestInferWorkloadKindNameTakesPrecedenceOverResourceKinds(t *testing.T) {
	t.Parallel()
	// Name-based inference ("cron") takes precedence over resource kinds.
	if got := InferWorkloadKind("my-cron-task", []string{"Deployment"}); got != "cronjob" {
		t.Fatalf("InferWorkloadKind() = %q, want cronjob", got)
	}
}

func TestInferWorkloadClassificationService(t *testing.T) {
	t.Parallel()

	candidate := WorkloadCandidate{
		RepoName:      "edge-api",
		ResourceKinds: []string{"deployment", "service"},
		Provenance:    []string{"k8s_resource"},
	}

	if got := InferWorkloadClassification(candidate); got != "service" {
		t.Fatalf("InferWorkloadClassification() = %q, want service", got)
	}
}

func TestInferWorkloadClassificationJob(t *testing.T) {
	t.Parallel()

	candidate := WorkloadCandidate{
		RepoName:      "nightly-batch",
		ResourceKinds: []string{"job"},
		Provenance:    []string{"k8s_resource"},
	}

	if got := InferWorkloadClassification(candidate); got != "job" {
		t.Fatalf("InferWorkloadClassification() = %q, want job", got)
	}
}

func TestInferWorkloadClassificationUtility(t *testing.T) {
	t.Parallel()

	candidate := WorkloadCandidate{
		RepoName:   "automation-shared",
		Provenance: []string{"jenkins_pipeline"},
		Confidence: 0.42,
		Namespaces: nil,
	}

	if got := InferWorkloadClassification(candidate); got != "utility" {
		t.Fatalf("InferWorkloadClassification() = %q, want utility", got)
	}
}

func TestInferWorkloadClassificationInfrastructure(t *testing.T) {
	t.Parallel()

	candidate := WorkloadCandidate{
		RepoName:   "network-stack",
		Provenance: []string{"cloudformation_template"},
	}

	if got := InferWorkloadClassification(candidate); got != "infrastructure" {
		t.Fatalf("InferWorkloadClassification() = %q, want infrastructure", got)
	}
}

func TestExtractOverlayEnvironmentsBasic(t *testing.T) {
	t.Parallel()
	paths := []string{
		"deploy/overlays/production/kustomization.yaml",
		"deploy/overlays/staging/kustomization.yaml",
		"deploy/base/deployment.yaml",
	}
	got := ExtractOverlayEnvironments(paths)
	if len(got) != 2 {
		t.Fatalf("ExtractOverlayEnvironments() len = %d, want 2", len(got))
	}
	if got[0] != "production" {
		t.Fatalf("got[0] = %q, want production", got[0])
	}
	if got[1] != "staging" {
		t.Fatalf("got[1] = %q, want staging", got[1])
	}
}

func TestExtractOverlayEnvironmentsDeduplicates(t *testing.T) {
	t.Parallel()
	paths := []string{
		"deploy/overlays/prod/a.yaml",
		"deploy/overlays/prod/b.yaml",
	}
	got := ExtractOverlayEnvironments(paths)
	if len(got) != 1 {
		t.Fatalf("ExtractOverlayEnvironments() len = %d, want 1", len(got))
	}
	if got[0] != "prod" {
		t.Fatalf("got[0] = %q, want prod", got[0])
	}
}

func TestExtractOverlayEnvironmentsSupportsJenkinsAndTerraformPathConventions(t *testing.T) {
	t.Parallel()
	paths := []string{
		"inventory/staging.ini",
		"group_vars/monitoring.yml",
		"env/prod/main.tf",
	}
	got := ExtractOverlayEnvironments(paths)
	if len(got) != 3 {
		t.Fatalf("ExtractOverlayEnvironments() len = %d, want 3", len(got))
	}
	if got[0] != "staging" {
		t.Fatalf("got[0] = %q, want staging", got[0])
	}
	if got[1] != "monitoring" {
		t.Fatalf("got[1] = %q, want monitoring", got[1])
	}
	if got[2] != "prod" {
		t.Fatalf("got[2] = %q, want prod", got[2])
	}
}

func TestExtractOverlayEnvironmentsNoMatch(t *testing.T) {
	t.Parallel()
	paths := []string{"src/main.go", "README.md"}
	got := ExtractOverlayEnvironments(paths)
	if len(got) != 0 {
		t.Fatalf("ExtractOverlayEnvironments() len = %d, want 0", len(got))
	}
}

func TestBuildProjectionRowsEmptyCandidates(t *testing.T) {
	t.Parallel()
	result := BuildProjectionRows(nil, nil)
	if result.Stats.Workloads != 0 {
		t.Fatalf("Stats.Workloads = %d, want 0", result.Stats.Workloads)
	}
	if len(result.WorkloadRows) != 0 {
		t.Fatalf("WorkloadRows len = %d, want 0", len(result.WorkloadRows))
	}
}

func TestBuildProjectionRowsSkipsMissingRepoID(t *testing.T) {
	t.Parallel()
	candidates := []WorkloadCandidate{
		{RepoName: "my-service"},
	}
	result := BuildProjectionRows(candidates, nil)
	if result.Stats.Workloads != 0 {
		t.Fatalf("Stats.Workloads = %d, want 0", result.Stats.Workloads)
	}
}

func TestBuildProjectionRowsSkipsMissingRepoName(t *testing.T) {
	t.Parallel()
	candidates := []WorkloadCandidate{
		{RepoID: "repo-1"},
	}
	result := BuildProjectionRows(candidates, nil)
	if result.Stats.Workloads != 0 {
		t.Fatalf("Stats.Workloads = %d, want 0", result.Stats.Workloads)
	}
}

func TestBuildProjectionRowsSingleCandidate(t *testing.T) {
	t.Parallel()
	candidates := []WorkloadCandidate{
		{
			RepoID:           "repo-1",
			RepoName:         "my-api",
			ResourceKinds:    []string{"Deployment", "Service"},
			Namespaces:       []string{"production"},
			DeploymentRepoID: "",
			Confidence:       0.98,
			Provenance:       []string{"k8s_resource"},
		},
	}
	result := BuildProjectionRows(candidates, nil)

	if result.Stats.Workloads != 1 {
		t.Fatalf("Stats.Workloads = %d, want 1", result.Stats.Workloads)
	}
	if len(result.WorkloadRows) != 1 {
		t.Fatalf("WorkloadRows len = %d, want 1", len(result.WorkloadRows))
	}
	wl := result.WorkloadRows[0]
	if wl.RepoID != "repo-1" {
		t.Fatalf("RepoID = %q, want repo-1", wl.RepoID)
	}
	if wl.WorkloadID != "workload:my-api" {
		t.Fatalf("WorkloadID = %q, want workload:my-api", wl.WorkloadID)
	}
	if wl.WorkloadKind != "service" {
		t.Fatalf("WorkloadKind = %q, want service", wl.WorkloadKind)
	}
	if wl.WorkloadName != "my-api" {
		t.Fatalf("WorkloadName = %q, want my-api", wl.WorkloadName)
	}

	// Instance from namespace fallback.
	if result.Stats.Instances != 1 {
		t.Fatalf("Stats.Instances = %d, want 1", result.Stats.Instances)
	}
	inst := result.InstanceRows[0]
	if inst.InstanceID != "workload-instance:my-api:production" {
		t.Fatalf("InstanceID = %q", inst.InstanceID)
	}
	if inst.Environment != "production" {
		t.Fatalf("Environment = %q", inst.Environment)
	}

	// Repo descriptor.
	if len(result.RepoDescriptors) != 1 {
		t.Fatalf("RepoDescriptors len = %d, want 1", len(result.RepoDescriptors))
	}
	if result.RepoDescriptors[0].WorkloadID != "workload:my-api" {
		t.Fatalf("RepoDescriptor WorkloadID = %q", result.RepoDescriptors[0].WorkloadID)
	}
}

func TestBuildProjectionRowsMaterializesAPIEndpointRows(t *testing.T) {
	t.Parallel()

	result := BuildProjectionRows([]WorkloadCandidate{
		{
			RepoID:         "repo-service-api",
			RepoName:       "service-api",
			Classification: "service",
			Confidence:     0.96,
			Provenance:     []string{"dockerfile_runtime"},
			APIEndpoints: []APIEndpointSignal{
				{
					Path:         "/widgets",
					Methods:      []string{"get", "post"},
					OperationIDs: []string{"createWidget", "listWidgets"},
					SourceKinds:  []string{"openapi"},
					SourcePaths:  []string{"specs/index.yaml"},
					SpecVersions: []string{"3.1.0"},
					APIVersions:  []string{"v3"},
				},
			},
		},
	}, nil)

	if got, want := len(result.EndpointRows), 1; got != want {
		t.Fatalf("len(EndpointRows) = %d, want %d", got, want)
	}
	endpoint := result.EndpointRows[0]
	if got, want := endpoint.WorkloadID, "workload:service-api"; got != want {
		t.Fatalf("endpoint.WorkloadID = %q, want %q", got, want)
	}
	if got, want := endpoint.Path, "/widgets"; got != want {
		t.Fatalf("endpoint.Path = %q, want %q", got, want)
	}
	if got, want := endpoint.Methods, []string{"get", "post"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("endpoint.Methods = %#v, want %#v", got, want)
	}
	if endpoint.EndpointID == "" {
		t.Fatal("endpoint.EndpointID = empty, want stable graph id")
	}
	if got, want := result.Stats.Endpoints, 1; got != want {
		t.Fatalf("Stats.Endpoints = %d, want %d", got, want)
	}
}

func TestBuildProjectionRowsUsesExplicitWorkloadName(t *testing.T) {
	t.Parallel()

	candidates := []WorkloadCandidate{
		{
			RepoID:           "repo-1",
			RepoName:         "monolith-repo",
			WorkloadName:     "api",
			ResourceKinds:    []string{"Deployment"},
			Namespaces:       []string{"production"},
			DeploymentRepoID: "deploy-repo-1",
			Classification:   "service",
			Confidence:       0.96,
			Provenance:       []string{"argocd_application_source"},
		},
	}
	deploymentEnvs := map[string][]string{
		"deploy-repo-1": {"production"},
	}

	result := BuildProjectionRows(candidates, deploymentEnvs)
	if len(result.WorkloadRows) != 1 {
		t.Fatalf("len(WorkloadRows) = %d, want 1", len(result.WorkloadRows))
	}
	if got, want := result.WorkloadRows[0].WorkloadID, "workload:api"; got != want {
		t.Fatalf("WorkloadID = %q, want %q", got, want)
	}
	if got, want := result.WorkloadRows[0].WorkloadName, "api"; got != want {
		t.Fatalf("WorkloadName = %q, want %q", got, want)
	}
	if len(result.InstanceRows) != 1 {
		t.Fatalf("len(InstanceRows) = %d, want 1", len(result.InstanceRows))
	}
	if got, want := result.InstanceRows[0].InstanceID, "workload-instance:api:production"; got != want {
		t.Fatalf("InstanceID = %q, want %q", got, want)
	}
}

func TestBuildProjectionRowsWithDeploymentEnvironments(t *testing.T) {
	t.Parallel()
	candidates := []WorkloadCandidate{
		{
			RepoID:           "repo-1",
			RepoName:         "my-api",
			ResourceKinds:    []string{"Deployment"},
			DeploymentRepoID: "deploy-repo-1",
			Confidence:       0.95,
			Provenance:       []string{"argocd_application_source"},
		},
	}
	deploymentEnvs := map[string][]string{
		"deploy-repo-1": {"staging", "production"},
	}
	result := BuildProjectionRows(candidates, deploymentEnvs)

	if result.Stats.Instances != 2 {
		t.Fatalf("Stats.Instances = %d, want 2", result.Stats.Instances)
	}
	if result.Stats.DeploymentSources != 2 {
		t.Fatalf("Stats.DeploymentSources = %d, want 2", result.Stats.DeploymentSources)
	}

	// Check deployment source rows.
	if len(result.DeploymentSourceRows) != 2 {
		t.Fatalf("DeploymentSourceRows len = %d, want 2", len(result.DeploymentSourceRows))
	}
	if result.DeploymentSourceRows[0].DeploymentRepoID != "deploy-repo-1" {
		t.Fatalf("DeploymentRepoID = %q", result.DeploymentSourceRows[0].DeploymentRepoID)
	}
}

func TestBuildProjectionRowsUsesMultipleDeploymentSources(t *testing.T) {
	t.Parallel()

	candidates := []WorkloadCandidate{
		{
			RepoID:            "repo-1",
			RepoName:          "my-api",
			DeploymentRepoIDs: []string{"deploy-current", "deploy-next"},
			Classification:    "service",
			Confidence:        0.96,
			Provenance:        []string{"argocd_applicationset_deploy_source", "helm_deployment"},
		},
	}
	deploymentEnvs := map[string][]string{
		"deploy-current": {"current"},
		"deploy-next":    {"poc", "new"},
	}

	result := BuildProjectionRows(candidates, deploymentEnvs)

	if got, want := len(result.InstanceRows), 3; got != want {
		t.Fatalf("len(InstanceRows) = %d, want %d", got, want)
	}
	if got, want := len(result.DeploymentSourceRows), 3; got != want {
		t.Fatalf("len(DeploymentSourceRows) = %d, want %d", got, want)
	}
	if !hasDeploymentSourceRow(result.DeploymentSourceRows, "workload-instance:my-api:current", "deploy-current") {
		t.Fatal("missing current deployment source row")
	}
	if !hasDeploymentSourceRow(result.DeploymentSourceRows, "workload-instance:my-api:poc", "deploy-next") {
		t.Fatal("missing poc deployment source row")
	}
	if !hasDeploymentSourceRow(result.DeploymentSourceRows, "workload-instance:my-api:new", "deploy-next") {
		t.Fatal("missing new deployment source row")
	}
	if !hasRuntimePlatformRow(result.RuntimePlatformRows, "workload-instance:my-api:current", "kubernetes") {
		t.Fatal("missing current kubernetes runtime platform")
	}
	if !hasRuntimePlatformRow(result.RuntimePlatformRows, "workload-instance:my-api:poc", "kubernetes") {
		t.Fatal("missing poc kubernetes runtime platform")
	}
	if !hasRuntimePlatformRow(result.RuntimePlatformRows, "workload-instance:my-api:new", "kubernetes") {
		t.Fatal("missing new kubernetes runtime platform")
	}
}

func TestBuildProjectionRowsUsesRepoIDOverlaysWhenNoDeploymentRepo(t *testing.T) {
	t.Parallel()
	candidates := []WorkloadCandidate{
		{
			RepoID:         "repo-self-deploy",
			RepoName:       "self-deploy-api",
			ResourceKinds:  []string{"Deployment"},
			Classification: "service",
			Confidence:     0.98,
			Provenance:     []string{"k8s_resource"},
		},
	}
	deploymentEnvs := map[string][]string{
		"repo-self-deploy": {"staging", "production"},
	}
	result := BuildProjectionRows(candidates, deploymentEnvs)

	if result.Stats.Instances != 2 {
		t.Fatalf("Stats.Instances = %d, want 2", result.Stats.Instances)
	}
	if result.InstanceRows[0].Environment != "staging" {
		t.Fatalf("InstanceRows[0].Environment = %q, want staging", result.InstanceRows[0].Environment)
	}
	if result.InstanceRows[1].Environment != "production" {
		t.Fatalf("InstanceRows[1].Environment = %q, want production", result.InstanceRows[1].Environment)
	}
}

func TestBuildProjectionRowsRuntimePlatformFromResourceKinds(t *testing.T) {
	t.Parallel()
	candidates := []WorkloadCandidate{
		{
			RepoID:        "repo-1",
			RepoName:      "my-api",
			ResourceKinds: []string{"Deployment", "Service"},
			Namespaces:    []string{"production"},
			Confidence:    0.98,
			Provenance:    []string{"k8s_resource"},
		},
	}
	result := BuildProjectionRows(candidates, nil)

	if len(result.RuntimePlatformRows) != 1 {
		t.Fatalf("RuntimePlatformRows len = %d, want 1", len(result.RuntimePlatformRows))
	}
	plat := result.RuntimePlatformRows[0]
	if plat.PlatformKind != "kubernetes" {
		t.Fatalf("PlatformKind = %q, want kubernetes", plat.PlatformKind)
	}
	if plat.InstanceID != "workload-instance:my-api:production" {
		t.Fatalf("InstanceID = %q", plat.InstanceID)
	}
	if plat.PlatformName != "production" {
		t.Fatalf("PlatformName = %q, want production", plat.PlatformName)
	}
}

func TestBuildProjectionRowsInfersKubernetesPlatformFromDeploymentSourceEvidence(t *testing.T) {
	t.Parallel()

	candidates := []WorkloadCandidate{
		{
			RepoID:           "repo-service",
			RepoName:         "service-api",
			DeploymentRepoID: "repo-delivery",
			Classification:   "service",
			Confidence:       0.96,
			Provenance:       []string{"dockerfile_runtime", "argocd_applicationset_deploy_source"},
		},
	}
	deploymentEnvs := map[string][]string{
		"repo-delivery": {"bg-prod", "bg-qa"},
	}

	result := BuildProjectionRows(candidates, deploymentEnvs)

	if got := len(result.RuntimePlatformRows); got != 2 {
		t.Fatalf("len(RuntimePlatformRows) = %d, want 2", got)
	}
	for _, row := range result.RuntimePlatformRows {
		if row.PlatformKind != "kubernetes" {
			t.Fatalf("PlatformKind = %q, want kubernetes", row.PlatformKind)
		}
		if row.PlatformID == "" {
			t.Fatal("PlatformID is empty")
		}
	}
}

func TestBuildProjectionRowsAddsProvisionedInfrastructurePlatforms(t *testing.T) {
	t.Parallel()

	candidates := []WorkloadCandidate{
		{
			RepoID:              "repo-service",
			RepoName:            "service-api",
			DeploymentRepoID:    "repo-delivery",
			ProvisioningRepoIDs: []string{"repo-infra"},
			ProvisioningEvidenceKinds: map[string][]string{
				"repo-infra": {TerraformPlatformEvidenceKind("ecs", "service")},
			},
			Classification: "service",
			Confidence:     0.96,
			Provenance:     []string{"dockerfile_runtime", "argocd_applicationset_deploy_source"},
		},
	}
	deploymentEnvs := map[string][]string{
		"repo-delivery": {"prod"},
		"repo-infra":    {"prod", "qa"},
	}
	infraPlatforms := map[string][]InfrastructurePlatformRow{
		"repo-infra": {
			{
				PlatformID:       "platform:ecs:aws:cluster/runtime-main:none:none",
				PlatformName:     "runtime-main",
				PlatformKind:     "ecs",
				PlatformProvider: "aws",
				PlatformLocator:  "cluster/runtime-main",
			},
		},
	}

	result := BuildProjectionRowsWithInfrastructurePlatforms(candidates, deploymentEnvs, infraPlatforms)

	if got := len(result.InstanceRows); got != 2 {
		t.Fatalf("len(InstanceRows) = %d, want 2", got)
	}
	if !hasRuntimePlatformRow(result.RuntimePlatformRows, "workload-instance:service-api:prod", "kubernetes") {
		t.Fatal("missing kubernetes runtime platform for prod deployment environment")
	}
	if !hasRuntimePlatformRow(result.RuntimePlatformRows, "workload-instance:service-api:prod", "ecs") {
		t.Fatal("missing ecs runtime platform for prod infrastructure environment")
	}
	if !hasRuntimePlatformRow(result.RuntimePlatformRows, "workload-instance:service-api:qa", "ecs") {
		t.Fatal("missing ecs runtime platform for qa infrastructure environment")
	}
}

func TestBuildProjectionRowsAddsMultipleProvisionedInfrastructurePlatforms(t *testing.T) {
	t.Parallel()

	candidates := []WorkloadCandidate{
		{
			RepoID:              "repo-service",
			RepoName:            "service-api",
			ProvisioningRepoIDs: []string{"repo-infra"},
			ProvisioningEvidenceKinds: map[string][]string{
				"repo-infra": {
					TerraformPlatformEvidenceKind("ecs", "service"),
					TerraformPlatformEvidenceKind("eks", "cluster"),
				},
			},
			Classification: "service",
			Confidence:     0.96,
			Provenance:     []string{"dockerfile_runtime"},
		},
	}
	deploymentEnvs := map[string][]string{
		"repo-infra": {"current", "poc"},
	}
	infraPlatforms := map[string][]InfrastructurePlatformRow{
		"repo-infra": {
			{
				PlatformID:       "platform:ecs:aws:cluster/current-runtime:none:none",
				PlatformName:     "current-runtime",
				PlatformKind:     "ecs",
				PlatformProvider: "aws",
				PlatformLocator:  "cluster/current-runtime",
			},
			{
				PlatformID:       "platform:eks:aws:cluster/poc-runtime:none:none",
				PlatformName:     "poc-runtime",
				PlatformKind:     "eks",
				PlatformProvider: "aws",
				PlatformLocator:  "cluster/poc-runtime",
			},
		},
	}

	result := BuildProjectionRowsWithInfrastructurePlatforms(candidates, deploymentEnvs, infraPlatforms)

	if !hasRuntimePlatformRow(result.RuntimePlatformRows, "workload-instance:service-api:current", "ecs") {
		t.Fatal("missing current ecs runtime platform")
	}
	if !hasRuntimePlatformRow(result.RuntimePlatformRows, "workload-instance:service-api:current", "eks") {
		t.Fatal("missing current eks runtime platform")
	}
	if !hasRuntimePlatformRow(result.RuntimePlatformRows, "workload-instance:service-api:poc", "ecs") {
		t.Fatal("missing poc ecs runtime platform")
	}
	if !hasRuntimePlatformRow(result.RuntimePlatformRows, "workload-instance:service-api:poc", "eks") {
		t.Fatal("missing poc eks runtime platform")
	}
}

func TestBuildProjectionRowsSkipsGenericProvisionedDependencyPlatforms(t *testing.T) {
	t.Parallel()

	candidates := []WorkloadCandidate{
		{
			RepoID:              "repo-service",
			RepoName:            "service-api",
			ProvisioningRepoIDs: []string{"repo-infra"},
			ProvisioningEvidenceKinds: map[string][]string{
				"repo-infra": {string(relationships.EvidenceKindTerraformConfigPath)},
			},
			Classification: "service",
			Confidence:     0.96,
			Provenance:     []string{"dockerfile_runtime"},
		},
	}
	deploymentEnvs := map[string][]string{
		"repo-infra": {"prod"},
	}
	infraPlatforms := map[string][]InfrastructurePlatformRow{
		"repo-infra": {
			{
				PlatformID:       "platform:lambda:aws:function/shared-dependency:none:none",
				PlatformName:     "shared-dependency",
				PlatformKind:     "lambda",
				PlatformProvider: "aws",
				PlatformLocator:  "function/shared-dependency",
			},
		},
	}

	result := BuildProjectionRowsWithInfrastructurePlatforms(candidates, deploymentEnvs, infraPlatforms)

	if got := len(result.RuntimePlatformRows); got != 0 {
		t.Fatalf("len(RuntimePlatformRows) = %d, want 0 for generic dependency evidence", got)
	}
	if got := len(result.InstanceRows); got != 0 {
		t.Fatalf("len(InstanceRows) = %d, want 0 when only generic dependency evidence exists", got)
	}
}

func TestBuildProjectionRowsSkipsAmbiguousProvisionedInfrastructurePlatforms(t *testing.T) {
	t.Parallel()

	candidates := []WorkloadCandidate{
		{
			RepoID:              "repo-service",
			RepoName:            "service-api",
			ProvisioningRepoIDs: []string{"repo-infra"},
			Classification:      "service",
			Confidence:          0.96,
			Provenance:          []string{"dockerfile_runtime"},
		},
	}
	deploymentEnvs := map[string][]string{
		"repo-infra": {"prod"},
	}
	infraPlatforms := map[string][]InfrastructurePlatformRow{
		"repo-infra": {
			{PlatformID: "platform:ecs:aws:cluster/runtime-main:none:none", PlatformName: "runtime-main", PlatformKind: "ecs"},
			{PlatformID: "platform:lambda:aws:cluster/jobs:none:none", PlatformName: "jobs", PlatformKind: "lambda"},
		},
	}

	result := BuildProjectionRowsWithInfrastructurePlatforms(candidates, deploymentEnvs, infraPlatforms)

	if got := len(result.RuntimePlatformRows); got != 0 {
		t.Fatalf("len(RuntimePlatformRows) = %d, want 0 for ambiguous infrastructure platforms", got)
	}
	if got := len(result.InstanceRows); got != 0 {
		t.Fatalf("len(InstanceRows) = %d, want 0 when only ambiguous infrastructure evidence exists", got)
	}
}

func TestBuildProjectionRowsDeduplicatesWorkloads(t *testing.T) {
	t.Parallel()
	candidates := []WorkloadCandidate{
		{RepoID: "repo-1", RepoName: "my-api", Namespaces: []string{"prod"}, Confidence: 0.98},
		{RepoID: "repo-1", RepoName: "my-api", Namespaces: []string{"staging"}, Confidence: 0.98},
	}
	result := BuildProjectionRows(candidates, nil)

	if result.Stats.Workloads != 1 {
		t.Fatalf("Stats.Workloads = %d, want 1 (deduplicated)", result.Stats.Workloads)
	}
	if result.Stats.Instances != 2 {
		t.Fatalf("Stats.Instances = %d, want 2", result.Stats.Instances)
	}
}

func TestBuildProjectionRowsDeduplicatesInstances(t *testing.T) {
	t.Parallel()
	candidates := []WorkloadCandidate{
		{RepoID: "repo-1", RepoName: "my-api", Namespaces: []string{"prod"}, Confidence: 0.98},
		{RepoID: "repo-1", RepoName: "my-api", Namespaces: []string{"prod"}, Confidence: 0.98},
	}
	result := BuildProjectionRows(candidates, nil)

	if result.Stats.Instances != 1 {
		t.Fatalf("Stats.Instances = %d, want 1 (deduplicated)", result.Stats.Instances)
	}
}

func TestBuildProjectionRowsNoRuntimePlatformWithoutKubernetesKinds(t *testing.T) {
	t.Parallel()
	candidates := []WorkloadCandidate{
		{
			RepoID:        "repo-1",
			RepoName:      "my-lib",
			ResourceKinds: []string{},
			Namespaces:    []string{"production"},
			Confidence:    0.85,
			Provenance:    []string{"dockerfile_runtime"},
		},
	}
	result := BuildProjectionRows(candidates, nil)

	if len(result.RuntimePlatformRows) != 0 {
		t.Fatalf("RuntimePlatformRows len = %d, want 0", len(result.RuntimePlatformRows))
	}
}

func TestBuildProjectionRowsSkipsUtilityCandidates(t *testing.T) {
	t.Parallel()

	result := BuildProjectionRows([]WorkloadCandidate{
		{
			RepoID:         "repo-utility",
			RepoName:       "automation-shared",
			Classification: "utility",
			Confidence:     0.42,
			Provenance:     []string{"jenkins_pipeline"},
		},
	}, nil)

	if got := len(result.WorkloadRows); got != 0 {
		t.Fatalf("len(WorkloadRows) = %d, want 0 for utility candidate", got)
	}
	if got := len(result.InstanceRows); got != 0 {
		t.Fatalf("len(InstanceRows) = %d, want 0 for utility candidate", got)
	}
}

func TestBuildProjectionRowsSkipsCandidatesWithoutConfidence(t *testing.T) {
	t.Parallel()

	result := BuildProjectionRows([]WorkloadCandidate{
		{
			RepoID:         "repo-missing-confidence",
			RepoName:       "missing-confidence-api",
			Classification: "service",
			ResourceKinds:  []string{"deployment"},
			Namespaces:     []string{"production"},
		},
	}, nil)

	if got := len(result.WorkloadRows); got != 0 {
		t.Fatalf("len(WorkloadRows) = %d, want 0 for missing-confidence candidate", got)
	}
	if got := len(result.InstanceRows); got != 0 {
		t.Fatalf("len(InstanceRows) = %d, want 0 for missing-confidence candidate", got)
	}
}

func TestBuildProjectionRowsSkipsCandidatesBelowMaterializationConfidenceFloor(t *testing.T) {
	t.Parallel()

	result := BuildProjectionRows([]WorkloadCandidate{
		{
			RepoID:         "repo-low-confidence",
			RepoName:       "dockerfile-only-api",
			Classification: "service",
			Confidence:     0.80,
			Provenance:     []string{"dockerfile_runtime"},
			ResourceKinds:  []string{"deployment"},
			Namespaces:     []string{"production"},
		},
	}, nil)

	if got := len(result.WorkloadRows); got != 0 {
		t.Fatalf("len(WorkloadRows) = %d, want 0 for below-threshold candidate", got)
	}
	if got := len(result.InstanceRows); got != 0 {
		t.Fatalf("len(InstanceRows) = %d, want 0 for below-threshold candidate", got)
	}
}

func TestBuildProjectionRowsCarriesClassificationConfidenceAndProvenance(t *testing.T) {
	t.Parallel()

	candidates := []WorkloadCandidate{
		{
			RepoID:           "repo-edge-api",
			RepoName:         "edge-api",
			DeploymentRepoID: "repo-platform-deploy",
			Classification:   "service",
			Confidence:       0.96,
			Provenance:       []string{"argocd_application_source", "dockerfile_runtime"},
			ResourceKinds:    []string{"deployment"},
		},
	}
	deploymentEnvs := map[string][]string{
		"repo-platform-deploy": {"production"},
	}

	result := BuildProjectionRows(candidates, deploymentEnvs)
	if got := len(result.WorkloadRows); got != 1 {
		t.Fatalf("len(WorkloadRows) = %d, want 1", got)
	}
	workload := result.WorkloadRows[0]
	if got, want := workload.Classification, "service"; got != want {
		t.Fatalf("Classification = %q, want %q", got, want)
	}
	if got, want := workload.Confidence, 0.96; got != want {
		t.Fatalf("Confidence = %f, want %f", got, want)
	}
	if got, want := workload.Provenance, []string{"argocd_application_source", "dockerfile_runtime"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Provenance = %v, want %v", got, want)
	}
	if got := len(result.DeploymentSourceRows); got != 1 {
		t.Fatalf("len(DeploymentSourceRows) = %d, want 1", got)
	}
	if got, want := result.DeploymentSourceRows[0].Confidence, 0.96; got != want {
		t.Fatalf("Deployment source confidence = %f, want %f", got, want)
	}
}

func TestBuildProjectionRowsDoesNotInventDefaultEnvironmentInstance(t *testing.T) {
	t.Parallel()

	candidates := []WorkloadCandidate{
		{
			RepoID:         "repo-sample-service",
			RepoName:       "sample-service-api",
			Classification: "service",
			Confidence:     0.85,
			Provenance:     []string{"dockerfile_runtime", "jenkins_pipeline"},
		},
	}

	result := BuildProjectionRows(candidates, nil)

	if got := result.Stats.Workloads; got != 1 {
		t.Fatalf("Stats.Workloads = %d, want 1", got)
	}
	if got := result.Stats.Instances; got != 0 {
		t.Fatalf("Stats.Instances = %d, want 0 when no environment evidence exists", got)
	}
	if got := len(result.InstanceRows); got != 0 {
		t.Fatalf("len(InstanceRows) = %d, want 0 when no environment evidence exists", got)
	}
	if got := len(result.RuntimePlatformRows); got != 0 {
		t.Fatalf("len(RuntimePlatformRows) = %d, want 0 when no environment evidence exists", got)
	}
	if got := len(result.DeploymentSourceRows); got != 0 {
		t.Fatalf("len(DeploymentSourceRows) = %d, want 0 when no runtime instance exists", got)
	}
}

func TestBuildProjectionRowsSkipsNamespaceFallbackForNonEnvironmentNamespace(t *testing.T) {
	t.Parallel()

	candidates := []WorkloadCandidate{
		{
			RepoID:         "repo-service-gha",
			RepoName:       "service-gha",
			Classification: "service",
			Confidence:     0.98,
			Namespaces:     []string{"service-gha"},
			Provenance:     []string{"k8s_resource", "dockerfile_runtime"},
		},
	}

	result := BuildProjectionRows(candidates, nil)

	if got := result.Stats.Workloads; got != 1 {
		t.Fatalf("Stats.Workloads = %d, want 1", got)
	}
	if got := result.Stats.Instances; got != 0 {
		t.Fatalf("Stats.Instances = %d, want 0 for non-environment namespace fallback", got)
	}
	if got := len(result.InstanceRows); got != 0 {
		t.Fatalf("len(InstanceRows) = %d, want 0 for non-environment namespace fallback", got)
	}
	if got := len(result.RuntimePlatformRows); got != 0 {
		t.Fatalf("len(RuntimePlatformRows) = %d, want 0 for non-environment namespace fallback", got)
	}
}

func hasRuntimePlatformRow(rows []RuntimePlatformRow, instanceID, platformKind string) bool {
	for _, row := range rows {
		if row.InstanceID == instanceID && row.PlatformKind == platformKind {
			return true
		}
	}
	return false
}

func hasDeploymentSourceRow(rows []DeploymentSourceRow, instanceID, deploymentRepoID string) bool {
	for _, row := range rows {
		if row.InstanceID == instanceID && row.DeploymentRepoID == deploymentRepoID {
			return true
		}
	}
	return false
}
