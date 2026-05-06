package reducer

import (
	"context"
	"errors"
	"testing"
)

// fakeNeo4jExecutor records all executed statements for assertion.
type fakeNeo4jExecutor struct {
	calls     []fakeExecutorCall
	errOnCall int // 0 = never error, N = error on Nth call (1-indexed)
	err       error
}

type fakeExecutorCall struct {
	Cypher     string
	Parameters map[string]any
}

func (f *fakeNeo4jExecutor) ExecuteCypher(ctx context.Context, cypher string, params map[string]any) error {
	f.calls = append(f.calls, fakeExecutorCall{Cypher: cypher, Parameters: params})
	if f.errOnCall > 0 && len(f.calls) == f.errOnCall {
		return f.err
	}
	return nil
}

func TestWorkloadMaterializerEmptyProjection(t *testing.T) {
	t.Parallel()

	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)

	result, err := m.Materialize(context.Background(), &ProjectionResult{})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if result.WorkloadsWritten != 0 {
		t.Fatalf("WorkloadsWritten = %d, want 0", result.WorkloadsWritten)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("executor calls = %d, want 0", len(executor.calls))
	}
}

func TestWorkloadMaterializerWritesWorkloads(t *testing.T) {
	t.Parallel()

	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)

	projection := &ProjectionResult{
		WorkloadRows: []WorkloadRow{
			{
				RepoID:         "repo-1",
				WorkloadID:     "workload:my-api",
				WorkloadKind:   "service",
				WorkloadName:   "my-api",
				Classification: "service",
				Confidence:     0.97,
				Provenance:     []string{"k8s_resource"},
			},
		},
	}

	result, err := m.Materialize(context.Background(), projection)
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if result.WorkloadsWritten != 1 {
		t.Fatalf("WorkloadsWritten = %d, want 1", result.WorkloadsWritten)
	}
	if len(executor.calls) < 1 {
		t.Fatalf("executor calls = %d, want >= 1", len(executor.calls))
	}
	if !containsCypher(executor.calls, "MERGE (w:Workload {id: row.workload_id})") {
		t.Fatal("missing Workload MERGE cypher")
	}
	if !containsCypher(executor.calls, "MATCH (w:Workload {id: row.workload_id})") {
		t.Fatal("missing indexed Workload MATCH for DEFINES edge")
	}
	if containsCypher(executor.calls[:1], "MATCH (repo:Repository") {
		t.Fatal("workload node upsert should not also match repository")
	}
	rows := executor.calls[0].Parameters["rows"].([]map[string]any)
	if got, want := rows[0]["classification"], "service"; got != want {
		t.Fatalf("classification = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["materialization_confidence"], 0.97; got != want {
		t.Fatalf("materialization_confidence = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["materialization_provenance"], []string{"k8s_resource"}; len(got.([]string)) != len(want) || got.([]string)[0] != want[0] {
		t.Fatalf("materialization_provenance = %#v, want %#v", got, want)
	}
	if !containsCypher(executor.calls, "w.classification = row.classification") {
		t.Fatal("missing workload classification cypher")
	}
}

func TestWorkloadMaterializerWritesInstances(t *testing.T) {
	t.Parallel()

	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)

	projection := &ProjectionResult{
		InstanceRows: []InstanceRow{
			{
				Environment:  "production",
				InstanceID:   "workload-instance:my-api:production",
				RepoID:       "repo-1",
				WorkloadID:   "workload:my-api",
				WorkloadKind: "service",
				WorkloadName: "my-api",
			},
		},
	}

	result, err := m.Materialize(context.Background(), projection)
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if result.InstancesWritten != 1 {
		t.Fatalf("InstancesWritten = %d, want 1", result.InstancesWritten)
	}
	if !containsCypher(executor.calls, "MERGE (i:WorkloadInstance {id: row.instance_id})") {
		t.Fatal("missing WorkloadInstance MERGE cypher")
	}
	if !containsCypher(executor.calls, "MATCH (i:WorkloadInstance {id: row.instance_id})") {
		t.Fatal("missing indexed WorkloadInstance MATCH for INSTANCE_OF edge")
	}
}

func TestWorkloadMaterializerWritesDeploymentSources(t *testing.T) {
	t.Parallel()

	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)

	projection := &ProjectionResult{
		DeploymentSourceRows: []DeploymentSourceRow{
			{
				DeploymentRepoID: "deploy-repo-1",
				Environment:      "production",
				InstanceID:       "workload-instance:my-api:production",
				WorkloadName:     "my-api",
				Confidence:       0.96,
				Provenance:       []string{"argocd_application_source", "dockerfile_runtime"},
			},
		},
	}

	result, err := m.Materialize(context.Background(), projection)
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if result.DeploymentSourcesWritten != 1 {
		t.Fatalf("DeploymentSourcesWritten = %d, want 1", result.DeploymentSourcesWritten)
	}
	if !containsCypher(executor.calls, "MERGE (i)-[rel:DEPLOYMENT_SOURCE]->(deployment_repo)") {
		t.Fatal("missing DEPLOYMENT_SOURCE MERGE cypher")
	}
	rows := executor.calls[0].Parameters["rows"].([]map[string]any)
	if got, want := rows[0]["deployment_confidence"], 0.96; got != want {
		t.Fatalf("deployment_confidence = %#v, want %#v", got, want)
	}
	if !containsCypher(executor.calls, "rel.confidence = row.deployment_confidence") {
		t.Fatal("missing deployment confidence cypher")
	}
}

func TestWorkloadMaterializerPreservesZeroDeploymentConfidence(t *testing.T) {
	t.Parallel()

	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)

	projection := &ProjectionResult{
		DeploymentSourceRows: []DeploymentSourceRow{
			{
				DeploymentRepoID: "deploy-repo-1",
				Environment:      "production",
				InstanceID:       "workload-instance:my-api:production",
				WorkloadName:     "my-api",
				Confidence:       0,
				Provenance:       []string{"unknown"},
			},
		},
	}

	result, err := m.Materialize(context.Background(), projection)
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if result.DeploymentSourcesWritten != 1 {
		t.Fatalf("DeploymentSourcesWritten = %d, want 1", result.DeploymentSourcesWritten)
	}
	rows := executor.calls[0].Parameters["rows"].([]map[string]any)
	if got, want := rows[0]["deployment_confidence"], 0.0; got != want {
		t.Fatalf("deployment_confidence = %#v, want %#v", got, want)
	}
}

func TestWorkloadMaterializerWritesRuntimePlatforms(t *testing.T) {
	t.Parallel()

	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)

	projection := &ProjectionResult{
		RuntimePlatformRows: []RuntimePlatformRow{
			{
				Environment:  "production",
				InstanceID:   "workload-instance:my-api:production",
				PlatformID:   "platform:kubernetes:none:production:production:none",
				PlatformKind: "kubernetes",
				PlatformName: "production",
				RepoID:       "repo-1",
			},
		},
	}

	result, err := m.Materialize(context.Background(), projection)
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if result.RuntimePlatformsWritten != 1 {
		t.Fatalf("RuntimePlatformsWritten = %d, want 1", result.RuntimePlatformsWritten)
	}
	if !containsCypher(executor.calls, "MERGE (i)-[rel:RUNS_ON]->(p)") {
		t.Fatal("missing RUNS_ON MERGE cypher")
	}
	if containsCypher(executor.calls, "CASE") {
		t.Fatal("runtime platform writes should precompute confidence in Go, not Cypher CASE")
	}
	if got := len(executor.calls); got != 2 {
		t.Fatalf("executor calls = %d, want 2 split runtime platform statements", got)
	}
	rows := executor.calls[1].Parameters["rows"].([]map[string]any)
	if got, want := rows[0]["platform_confidence"], 0.9; got != want {
		t.Fatalf("platform_confidence = %#v, want %#v", got, want)
	}
}

func TestWorkloadMaterializerWritesAPIEndpoints(t *testing.T) {
	t.Parallel()

	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)

	projection := &ProjectionResult{
		EndpointRows: []APIEndpointRow{
			{
				EndpointID:   "endpoint:repo-service-api:1234",
				RepoID:       "repo-service-api",
				WorkloadID:   "workload:service-api",
				WorkloadName: "service-api",
				Path:         "/widgets",
				Methods:      []string{"get", "post"},
				OperationIDs: []string{"createWidget", "listWidgets"},
				SourceKinds:  []string{"openapi"},
				SourcePaths:  []string{"specs/index.yaml"},
				SpecVersions: []string{"3.1.0"},
				APIVersions:  []string{"v3"},
			},
		},
	}

	result, err := m.Materialize(context.Background(), projection)
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if result.EndpointsWritten != 1 {
		t.Fatalf("EndpointsWritten = %d, want 1", result.EndpointsWritten)
	}
	if !containsCypher(executor.calls, "MERGE (endpoint:Endpoint {id: row.endpoint_id})") {
		t.Fatal("missing Endpoint MERGE cypher")
	}
	if !containsCypher(executor.calls, "MERGE (repo)-[repo_rel:EXPOSES_ENDPOINT]->(endpoint)") {
		t.Fatal("missing Repository EXPOSES_ENDPOINT MERGE cypher")
	}
	if !containsCypher(executor.calls, "MERGE (workload)-[workload_rel:EXPOSES_ENDPOINT]->(endpoint)") {
		t.Fatal("missing Workload EXPOSES_ENDPOINT MERGE cypher")
	}
	if got := len(executor.calls); got != 3 {
		t.Fatalf("executor calls = %d, want 3 split endpoint statements", got)
	}
	if containsCypher([]fakeExecutorCall{executor.calls[0]}, "MATCH (repo:Repository") ||
		containsCypher([]fakeExecutorCall{executor.calls[0]}, "MATCH (workload:Workload") {
		t.Fatal("endpoint node upsert should not also match repo or workload")
	}
	rows := executor.calls[0].Parameters["rows"].([]map[string]any)
	if got, want := rows[0]["path"], "/widgets"; got != want {
		t.Fatalf("path = %#v, want %#v", got, want)
	}
}

func TestWorkloadMaterializerFullPipeline(t *testing.T) {
	t.Parallel()

	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)

	projection := &ProjectionResult{
		Stats: ProjectionStats{Workloads: 1, Instances: 1, DeploymentSources: 1},
		WorkloadRows: []WorkloadRow{
			{RepoID: "repo-1", WorkloadID: "workload:my-api", WorkloadKind: "service", WorkloadName: "my-api"},
		},
		InstanceRows: []InstanceRow{
			{Environment: "prod", InstanceID: "workload-instance:my-api:prod", RepoID: "repo-1", WorkloadID: "workload:my-api", WorkloadKind: "service", WorkloadName: "my-api"},
		},
		DeploymentSourceRows: []DeploymentSourceRow{
			{DeploymentRepoID: "deploy-repo-1", Environment: "prod", InstanceID: "workload-instance:my-api:prod", WorkloadName: "my-api"},
		},
		RuntimePlatformRows: []RuntimePlatformRow{
			{Environment: "prod", InstanceID: "workload-instance:my-api:prod", PlatformID: "platform:kubernetes:none:prod:prod:none", PlatformKind: "kubernetes", PlatformName: "prod", RepoID: "repo-1"},
		},
	}

	result, err := m.Materialize(context.Background(), projection)
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if result.WorkloadsWritten != 1 {
		t.Fatalf("WorkloadsWritten = %d, want 1", result.WorkloadsWritten)
	}
	if result.InstancesWritten != 1 {
		t.Fatalf("InstancesWritten = %d, want 1", result.InstancesWritten)
	}
	if result.DeploymentSourcesWritten != 1 {
		t.Fatalf("DeploymentSourcesWritten = %d, want 1", result.DeploymentSourcesWritten)
	}
	if result.RuntimePlatformsWritten != 1 {
		t.Fatalf("RuntimePlatformsWritten = %d, want 1", result.RuntimePlatformsWritten)
	}
	// Split write phases keep node upserts separate from relationship writes.
	if len(executor.calls) != 7 {
		t.Fatalf("executor calls = %d, want 7", len(executor.calls))
	}
}

func TestWorkloadMaterializerPropagatesExecutorError(t *testing.T) {
	t.Parallel()

	executor := &fakeNeo4jExecutor{
		errOnCall: 1,
		err:       errors.New("neo4j connection refused"),
	}
	m := NewWorkloadMaterializer(executor)

	projection := &ProjectionResult{
		WorkloadRows: []WorkloadRow{
			{RepoID: "repo-1", WorkloadID: "workload:my-api", WorkloadKind: "service", WorkloadName: "my-api"},
		},
	}

	_, err := m.Materialize(context.Background(), projection)
	if err == nil {
		t.Fatal("Materialize() error = nil, want non-nil")
	}
	if !errors.Is(err, executor.err) {
		t.Fatalf("error = %q, want wrapped neo4j connection refused", err.Error())
	}
}

func TestWorkloadMaterializerRequiresExecutor(t *testing.T) {
	t.Parallel()

	m := NewWorkloadMaterializer(nil)
	projection := &ProjectionResult{
		WorkloadRows: []WorkloadRow{
			{RepoID: "repo-1", WorkloadID: "workload:my-api", WorkloadKind: "service", WorkloadName: "my-api"},
		},
	}

	_, err := m.Materialize(context.Background(), projection)
	if err == nil {
		t.Fatal("Materialize() error = nil, want non-nil")
	}
}

func TestWorkloadMaterializerNilExecutorWithEmptyProjectionIsNoop(t *testing.T) {
	t.Parallel()

	m := NewWorkloadMaterializer(nil)
	result, err := m.Materialize(context.Background(), &ProjectionResult{})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if result.WorkloadsWritten != 0 {
		t.Fatalf("WorkloadsWritten = %d, want 0", result.WorkloadsWritten)
	}
}

func TestWorkloadMaterializerWritesRepoDependencies(t *testing.T) {
	t.Parallel()

	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)

	result, err := m.MaterializeDependencies(
		context.Background(),
		[]RepoDependencyRow{
			{DependencyName: "svc-b", RepoID: "repo-a", TargetRepoID: "repo-b"},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("MaterializeDependencies() error = %v", err)
	}
	if result.RepoDependenciesWritten != 1 {
		t.Fatalf("RepoDependenciesWritten = %d, want 1", result.RepoDependenciesWritten)
	}
	if !containsCypher(executor.calls, "MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)") {
		t.Fatal("missing repo DEPENDS_ON MERGE cypher")
	}
}

func TestWorkloadMaterializerWritesWorkloadDependencies(t *testing.T) {
	t.Parallel()

	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)

	result, err := m.MaterializeDependencies(
		context.Background(),
		nil,
		[]WorkloadDependencyRow{
			{WorkloadID: "workload:svc-a", TargetWorkloadID: "workload:svc-b"},
		},
	)
	if err != nil {
		t.Fatalf("MaterializeDependencies() error = %v", err)
	}
	if result.WorkloadDependenciesWritten != 1 {
		t.Fatalf("WorkloadDependenciesWritten = %d, want 1", result.WorkloadDependenciesWritten)
	}
	if !containsCypher(executor.calls, "MERGE (source)-[rel:DEPENDS_ON]->(target)") {
		t.Fatal("missing workload DEPENDS_ON MERGE cypher")
	}
}

// containsCypher checks if any executor call contains the given cypher fragment.
func containsCypher(calls []fakeExecutorCall, fragment string) bool {
	for _, call := range calls {
		if contains(call.Cypher, fragment) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
