package query

import (
	"strings"
	"testing"
)

func TestBuildDeadCodeGraphCypherKeepsCandidateReadSimple(t *testing.T) {
	t.Parallel()

	cypher := buildDeadCodeGraphCypher(true, GraphBackendNornicDB)
	for _, want := range []string{
		"MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e:Function)",
		"ORDER BY f.relative_path, e.name, coalesce(e.uid, e.id)",
		"SKIP $skip",
		"LIMIT $limit",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("dead-code cypher missing %q:\n%s", want, cypher)
		}
	}
	for _, notWant := range []string{
		"NOT EXISTS { MATCH (e)<-[:CALLS|IMPORTS|REFERENCES|INHERITS]-() }",
		"NOT ()-[:CALLS|IMPORTS|REFERENCES|INHERITS]->(e)",
		"toLower(f.relative_path)",
		"coalesce(e.enclosing_function, '')",
	} {
		if strings.Contains(cypher, notWant) {
			t.Fatalf("dead-code cypher contains app-layer policy or reachability predicate %q:\n%s", notWant, cypher)
		}
	}
}

func TestBuildDeadCodeIncomingProbeCypherUsesBatchedExactEntityLookup(t *testing.T) {
	t.Parallel()

	cypher := buildDeadCodeIncomingBatchProbeCypher("Function")
	for _, want := range []string{
		"UNWIND $entity_ids AS entity_id",
		"MATCH (e:Function {uid: entity_id})<-[:CALLS|IMPORTS|REFERENCES|INHERITS|EXECUTES]-(source)",
		"RETURN DISTINCT coalesce(e.uid, e.id) as incoming_entity_id",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("incoming-edge cypher missing %q:\n%s", want, cypher)
		}
	}
	if strings.Contains(cypher, "{uid: $entity_id}") {
		t.Fatalf("incoming-edge probe should use batched entity ids in the lookup:\n%s", cypher)
	}
	if strings.Contains(cypher, "Repository") {
		t.Fatalf("incoming-edge probe should not fan out through repository scope:\n%s", cypher)
	}
}

func TestDeadCodeCandidateScanLimitUsesFullWindowForSmallDisplayLimits(t *testing.T) {
	t.Parallel()

	if got, want := deadCodeCandidateScanLimit(50), 2500; got != want {
		t.Fatalf("deadCodeCandidateScanLimit(50) = %d, want %d", got, want)
	}
}
