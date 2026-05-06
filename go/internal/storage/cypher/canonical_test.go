package cypher

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildCanonicalWorkloadUpsertStatement(t *testing.T) {
	t.Parallel()

	stmt := BuildCanonicalWorkloadUpsert(CanonicalWorkloadParams{
		RepoID:       "repo-1",
		WorkloadID:   "workload-1",
		WorkloadName: "my-service",
		WorkloadKind: "service",
	}, "finalization/workloads")

	if stmt.Operation != OperationCanonicalUpsert {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalUpsert)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (w:Workload {id: $workload_id})") {
		t.Fatalf("Cypher missing Workload MERGE: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (repo)-[rel:DEFINES]->(w)") {
		t.Fatalf("Cypher missing DEFINES edge: %s", stmt.Cypher)
	}
	if stmt.Parameters["repo_id"] != "repo-1" {
		t.Fatalf("repo_id = %v, want repo-1", stmt.Parameters["repo_id"])
	}
	if stmt.Parameters["evidence_source"] != "finalization/workloads" {
		t.Fatalf("evidence_source = %v", stmt.Parameters["evidence_source"])
	}
}

func TestBuildCanonicalWorkloadInstanceUpsertStatement(t *testing.T) {
	t.Parallel()

	stmt := BuildCanonicalWorkloadInstanceUpsert(CanonicalWorkloadInstanceParams{
		WorkloadID:   "workload-1",
		InstanceID:   "instance-1",
		WorkloadName: "my-service",
		WorkloadKind: "service",
		Environment:  "production",
		RepoID:       "repo-1",
	}, "finalization/workloads")

	if stmt.Operation != OperationCanonicalUpsert {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalUpsert)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (i:WorkloadInstance {id: $instance_id})") {
		t.Fatalf("Cypher missing WorkloadInstance MERGE: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (i)-[rel:INSTANCE_OF]->(w)") {
		t.Fatalf("Cypher missing INSTANCE_OF edge: %s", stmt.Cypher)
	}
	if stmt.Parameters["instance_id"] != "instance-1" {
		t.Fatalf("instance_id = %v", stmt.Parameters["instance_id"])
	}
	if stmt.Parameters["environment"] != "production" {
		t.Fatalf("environment = %v", stmt.Parameters["environment"])
	}
}

func TestBuildCanonicalRuntimePlatformUpsertStatement(t *testing.T) {
	t.Parallel()

	stmt := BuildCanonicalRuntimePlatformUpsert(CanonicalRuntimePlatformParams{
		InstanceID:       "instance-1",
		PlatformID:       "platform:eks:aws:my-cluster:production:us-east-1",
		PlatformName:     "my-cluster",
		PlatformKind:     "eks",
		PlatformProvider: "aws",
		Environment:      "production",
		PlatformRegion:   "us-east-1",
		PlatformLocator:  "arn:aws:eks:us-east-1:123:cluster/my-cluster",
	}, "finalization/workloads")

	if stmt.Operation != OperationCanonicalUpsert {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalUpsert)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (p:Platform {id: $platform_id})") {
		t.Fatalf("Cypher missing Platform MERGE: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (i)-[rel:RUNS_ON]->(p)") {
		t.Fatalf("Cypher missing RUNS_ON edge: %s", stmt.Cypher)
	}
	if stmt.Parameters["platform_id"] != "platform:eks:aws:my-cluster:production:us-east-1" {
		t.Fatalf("platform_id = %v", stmt.Parameters["platform_id"])
	}
	if stmt.Parameters["platform_kind"] != "eks" {
		t.Fatalf("platform_kind = %v", stmt.Parameters["platform_kind"])
	}
}

func TestBuildCanonicalRepoRelationshipUpsertStatement(t *testing.T) {
	t.Parallel()

	stmt := BuildCanonicalRepoRelationshipUpsert(CanonicalRepoRelationshipParams{
		RepoID:           "repo-a",
		TargetRepoID:     "repo-b",
		RelationshipType: "DEPLOYS_FROM",
		EvidenceType:     "argocd_application_source",
		ResolvedID:       "resolved-1",
		GenerationID:     "gen-1",
		EvidenceCount:    3,
		EvidenceKinds:    []string{"ARGOCD_APPLICATION_SOURCE", "HELM_VALUES_REFERENCE"},
		ResolutionSource: "inferred",
		Confidence:       0.93,
		Rationale:        "deployment config references service repository",
	}, "resolver/cross-repo")

	if stmt.Operation != OperationCanonicalUpsert {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalUpsert)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (source_repo:Repository {id: $repo_id})") {
		t.Fatalf("Cypher missing source Repository MERGE: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (target_repo:Repository {id: $target_repo_id})") {
		t.Fatalf("Cypher missing target Repository MERGE: %s", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "FOREACH") {
		t.Fatalf("Cypher must not rely on FOREACH typed routing: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (source_repo)-[rel:DEPLOYS_FROM]->(target_repo)") {
		t.Fatalf("Cypher missing DEPLOYS_FROM edge: %s", stmt.Cypher)
	}
	if stmt.Parameters["relationship_type"] != "DEPLOYS_FROM" {
		t.Fatalf("relationship_type = %v", stmt.Parameters["relationship_type"])
	}
	if stmt.Parameters["evidence_type"] != "argocd_application_source" {
		t.Fatalf("evidence_type = %v", stmt.Parameters["evidence_type"])
	}
	if !strings.Contains(stmt.Cypher, "rel.evidence_type = $evidence_type") {
		t.Fatalf("Cypher missing evidence_type write: %s", stmt.Cypher)
	}
	for _, fragment := range []string{
		"rel.resolved_id = $resolved_id",
		"rel.generation_id = $generation_id",
		"rel.evidence_count = $evidence_count",
		"rel.evidence_kinds = $evidence_kinds",
		"rel.resolution_source = $resolution_source",
		"rel.rationale = $rationale",
		"rel.confidence = $confidence",
	} {
		if !strings.Contains(stmt.Cypher, fragment) {
			t.Fatalf("Cypher missing evidence metadata write %q: %s", fragment, stmt.Cypher)
		}
	}
	for key, want := range map[string]any{
		"resolved_id":       "resolved-1",
		"generation_id":     "gen-1",
		"evidence_count":    3,
		"resolution_source": "inferred",
		"confidence":        0.93,
		"rationale":         "deployment config references service repository",
	} {
		if got := stmt.Parameters[key]; got != want {
			t.Fatalf("%s = %#v, want %#v", key, got, want)
		}
	}
	if got, want := stmt.Parameters["evidence_kinds"], []string{"ARGOCD_APPLICATION_SOURCE", "HELM_VALUES_REFERENCE"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("evidence_kinds = %#v, want %#v", got, want)
	}
}

func TestBuildCanonicalRunsOnUpsertStatementUsesWorkloadInstanceShape(t *testing.T) {
	t.Parallel()

	stmt := BuildCanonicalRunsOnUpsert(CanonicalRunsOnParams{
		RepoID:     "repo-a",
		PlatformID: "platform:eks:aws:cluster-1:prod:us-east-1",
	}, "resolver/cross-repo")

	if stmt.Operation != OperationCanonicalUpsert {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalUpsert)
	}
	if !strings.Contains(stmt.Cypher, "WorkloadInstance") {
		t.Fatalf("Cypher missing WorkloadInstance match: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (i)-[rel:RUNS_ON]->(p)") {
		t.Fatalf("Cypher missing RUNS_ON edge: %s", stmt.Cypher)
	}
	if stmt.Parameters["platform_id"] != "platform:eks:aws:cluster-1:prod:us-east-1" {
		t.Fatalf("platform_id = %v", stmt.Parameters["platform_id"])
	}
}

func TestBuildCanonicalInfrastructurePlatformUpsertStatement(t *testing.T) {
	t.Parallel()

	stmt := BuildCanonicalInfrastructurePlatformUpsert(CanonicalInfrastructurePlatformParams{
		RepoID:              "repo-1",
		PlatformID:          "platform:eks:aws:infra-cluster:staging:us-west-2",
		PlatformName:        "infra-cluster",
		PlatformKind:        "eks",
		PlatformProvider:    "aws",
		PlatformEnvironment: "staging",
		PlatformRegion:      "us-west-2",
		PlatformLocator:     "arn:aws:eks:us-west-2:123:cluster/infra-cluster",
	}, "finalization/workloads")

	if stmt.Operation != OperationCanonicalUpsert {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalUpsert)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (p:Platform {id: $platform_id})") {
		t.Fatalf("Cypher missing Platform MERGE: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (repo)-[rel:PROVISIONS_PLATFORM]->(p)") {
		t.Fatalf("Cypher missing PROVISIONS_PLATFORM edge: %s", stmt.Cypher)
	}
	if stmt.Parameters["platform_environment"] != "staging" {
		t.Fatalf("platform_environment = %v", stmt.Parameters["platform_environment"])
	}
}

func TestBuildCanonicalDeploymentSourceUpsertStatement(t *testing.T) {
	t.Parallel()

	stmt := BuildCanonicalDeploymentSourceUpsert(CanonicalDeploymentSourceParams{
		InstanceID:       "instance-1",
		DeploymentRepoID: "deploy-repo-1",
	}, "finalization/workloads")

	if stmt.Operation != OperationCanonicalUpsert {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalUpsert)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (i)-[rel:DEPLOYMENT_SOURCE]->(deployment_repo)") {
		t.Fatalf("Cypher missing DEPLOYMENT_SOURCE edge: %s", stmt.Cypher)
	}
	if stmt.Parameters["deployment_repo_id"] != "deploy-repo-1" {
		t.Fatalf("deployment_repo_id = %v", stmt.Parameters["deployment_repo_id"])
	}
}

func TestBuildCanonicalRepoDependencyUpsertStatement(t *testing.T) {
	t.Parallel()

	stmt := BuildCanonicalRepoDependencyUpsert(CanonicalRepoDependencyParams{
		RepoID:       "repo-a",
		TargetRepoID: "repo-b",
		EvidenceType: "docker_compose_depends_on",
	}, "finalization/workloads")

	if stmt.Operation != OperationCanonicalUpsert {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalUpsert)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (source_repo:Repository {id: $repo_id})") {
		t.Fatalf("Cypher missing source Repository MERGE: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (target_repo:Repository {id: $target_repo_id})") {
		t.Fatalf("Cypher missing target Repository MERGE: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)") {
		t.Fatalf("Cypher missing DEPENDS_ON edge: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "rel.evidence_type = $evidence_type") {
		t.Fatalf("Cypher missing evidence_type write: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "rel.relationship_type = 'DEPENDS_ON'") {
		t.Fatalf("Cypher missing relationship_type write: %s", stmt.Cypher)
	}
	if stmt.Parameters["repo_id"] != "repo-a" {
		t.Fatalf("repo_id = %v", stmt.Parameters["repo_id"])
	}
	if stmt.Parameters["target_repo_id"] != "repo-b" {
		t.Fatalf("target_repo_id = %v", stmt.Parameters["target_repo_id"])
	}
	if stmt.Parameters["evidence_type"] != "docker_compose_depends_on" {
		t.Fatalf("evidence_type = %v", stmt.Parameters["evidence_type"])
	}
}

func TestBuildCanonicalWorkloadDependencyUpsertStatement(t *testing.T) {
	t.Parallel()

	stmt := BuildCanonicalWorkloadDependencyUpsert(CanonicalWorkloadDependencyParams{
		WorkloadID:       "wl-a",
		TargetWorkloadID: "wl-b",
	}, "finalization/workloads")

	if stmt.Operation != OperationCanonicalUpsert {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalUpsert)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (source)-[rel:DEPENDS_ON]->(target)") {
		t.Fatalf("Cypher missing workload DEPENDS_ON edge: %s", stmt.Cypher)
	}
	if stmt.Parameters["workload_id"] != "wl-a" {
		t.Fatalf("workload_id = %v", stmt.Parameters["workload_id"])
	}
}

func TestBuildCanonicalCodeCallUpsertStatement(t *testing.T) {
	t.Parallel()

	stmt := BuildCanonicalCodeCallUpsert(CanonicalCodeCallParams{
		CallerEntityID: "entity:function:caller",
		CalleeEntityID: "entity:function:callee",
		CallKind:       "jsx_component",
	}, "parser/code-calls")

	if stmt.Operation != OperationCanonicalUpsert {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalUpsert)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (source)-[rel:REFERENCES]->(target)") {
		t.Fatalf("Cypher missing REFERENCES edge: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "rel.call_kind = $call_kind") {
		t.Fatalf("Cypher missing call_kind write: %s", stmt.Cypher)
	}
	if stmt.Parameters["caller_entity_id"] != "entity:function:caller" {
		t.Fatalf("caller_entity_id = %v, want entity:function:caller", stmt.Parameters["caller_entity_id"])
	}
	if stmt.Parameters["callee_entity_id"] != "entity:function:callee" {
		t.Fatalf("callee_entity_id = %v, want entity:function:callee", stmt.Parameters["callee_entity_id"])
	}
	if stmt.Parameters["call_kind"] != "jsx_component" {
		t.Fatalf("call_kind = %v, want jsx_component", stmt.Parameters["call_kind"])
	}
}

func TestBuildCanonicalCodeCallUpsertStatementUsesCallsForRegularFunctions(t *testing.T) {
	t.Parallel()

	stmt := BuildCanonicalCodeCallUpsert(CanonicalCodeCallParams{
		CallerEntityID: "entity:function:caller",
		CalleeEntityID: "entity:function:callee",
		CallKind:       "function_call",
	}, "parser/code-calls")

	if stmt.Operation != OperationCanonicalUpsert {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalUpsert)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (source)-[rel:CALLS]->(target)") {
		t.Fatalf("Cypher missing CALLS edge: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "rel.call_kind = $call_kind") {
		t.Fatalf("Cypher missing call_kind write: %s", stmt.Cypher)
	}
}

func TestBuildCanonicalCodeCallUpsertStatementUsesMetaclassEdges(t *testing.T) {
	t.Parallel()

	stmt := BuildCanonicalCodeCallUpsert(CanonicalCodeCallParams{
		CallerEntityID:   "entity:class:logged",
		CalleeEntityID:   "entity:class:meta",
		RelationshipType: "USES_METACLASS",
	}, "parser/python-metaclass")

	if stmt.Operation != OperationCanonicalUpsert {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalUpsert)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (source)-[rel:USES_METACLASS]->(target)") {
		t.Fatalf("Cypher missing USES_METACLASS edge: %s", stmt.Cypher)
	}
	if stmt.Parameters["caller_entity_id"] != "entity:class:logged" {
		t.Fatalf("caller_entity_id = %v, want entity:class:logged", stmt.Parameters["caller_entity_id"])
	}
	if stmt.Parameters["callee_entity_id"] != "entity:class:meta" {
		t.Fatalf("callee_entity_id = %v, want entity:class:meta", stmt.Parameters["callee_entity_id"])
	}
	if stmt.Parameters["relationship_type"] != "USES_METACLASS" {
		t.Fatalf("relationship_type = %v, want USES_METACLASS", stmt.Parameters["relationship_type"])
	}
}

func TestBuildRetractInfrastructurePlatformEdgesStatement(t *testing.T) {
	t.Parallel()

	stmt := BuildRetractInfrastructurePlatformEdges([]string{"repo-1", "repo-2"}, "finalization/workloads")

	if stmt.Operation != OperationCanonicalRetract {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
	}
	if !strings.Contains(stmt.Cypher, "PROVISIONS_PLATFORM") {
		t.Fatalf("Cypher missing PROVISIONS_PLATFORM: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "DELETE rel") {
		t.Fatalf("Cypher missing DELETE: %s", stmt.Cypher)
	}
	repoIDs, ok := stmt.Parameters["repo_ids"].([]string)
	if !ok {
		t.Fatalf("repo_ids type = %T, want []string", stmt.Parameters["repo_ids"])
	}
	if len(repoIDs) != 2 {
		t.Fatalf("repo_ids len = %d, want 2", len(repoIDs))
	}
}

func TestBuildRetractRepoDependencyEdgesStatement(t *testing.T) {
	t.Parallel()

	stmt := BuildRetractRepoDependencyEdges([]string{"repo-1"}, "finalization/workloads")

	if stmt.Operation != OperationCanonicalRetract {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
	}
	if !strings.Contains(stmt.Cypher, "DEPENDS_ON") {
		t.Fatalf("Cypher missing DEPENDS_ON: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "source_repo:Repository") {
		t.Fatalf("Cypher missing Repository match: %s", stmt.Cypher)
	}
}

func TestBuildRetractWorkloadDependencyEdgesStatement(t *testing.T) {
	t.Parallel()

	stmt := BuildRetractWorkloadDependencyEdges([]string{"repo-1"}, "finalization/workloads")

	if stmt.Operation != OperationCanonicalRetract {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
	}
	if !strings.Contains(stmt.Cypher, "DEPENDS_ON") {
		t.Fatalf("Cypher missing DEPENDS_ON: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "source:Workload") {
		t.Fatalf("Cypher missing Workload match: %s", stmt.Cypher)
	}
}

func TestBuildRetractCodeCallEdgesStatement(t *testing.T) {
	t.Parallel()

	stmt := BuildRetractCodeCallEdges([]string{"repo-1"}, "parser/code-calls")

	if stmt.Operation != OperationCanonicalRetract {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
	}
	if !strings.Contains(stmt.Cypher, "CALLS") {
		t.Fatalf("Cypher missing CALLS: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "REFERENCES") {
		t.Fatalf("Cypher missing REFERENCES: %s", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "USES_METACLASS") {
		t.Fatalf("Cypher unexpectedly includes USES_METACLASS: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "source.repo_id IN $repo_ids") {
		t.Fatalf("Cypher missing repo_id filter: %s", stmt.Cypher)
	}
}

func TestBuildRetractCodeCallEdgesMetaclassStatement(t *testing.T) {
	t.Parallel()

	stmt := BuildRetractCodeCallEdges([]string{"repo-1"}, "parser/python-metaclass")

	if stmt.Operation != OperationCanonicalRetract {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
	}
	if !strings.Contains(stmt.Cypher, "USES_METACLASS") {
		t.Fatalf("Cypher missing USES_METACLASS: %s", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "CALLS") {
		t.Fatalf("Cypher unexpectedly includes CALLS: %s", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "REFERENCES") {
		t.Fatalf("Cypher unexpectedly includes REFERENCES: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "source.repo_id IN $repo_ids") {
		t.Fatalf("Cypher missing repo_id filter: %s", stmt.Cypher)
	}
}

func TestBuildDeleteOrphanPlatformNodesStatement(t *testing.T) {
	t.Parallel()

	stmt := BuildDeleteOrphanPlatformNodes("finalization/workloads")

	if stmt.Operation != OperationCanonicalRetract {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
	}
	if !strings.Contains(stmt.Cypher, "NOT (p)--()") {
		t.Fatalf("Cypher missing orphan check: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "DELETE p") {
		t.Fatalf("Cypher missing DELETE p: %s", stmt.Cypher)
	}
}
