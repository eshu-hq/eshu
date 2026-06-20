package queryplan

import (
	"strings"
	"testing"
)

func TestValidateManifestAcceptsAnchoredCypherAndReadModelCaveat(t *testing.T) {
	manifest := Manifest{
		Version:     1,
		RequiredIDs: []string{"deployable_unit_relationships", "supply_chain_impact_readiness"},
		Entries: []Entry{
			{
				ID:        "deployable_unit_relationships",
				Domain:    "deployable",
				Backend:   "nornicdb",
				QueryKind: "cypher",
				Source: SourceRef{
					File:     "go/internal/query/repository_deployable_unit_relationships.go",
					Symbol:   "fetchDeployableUnitRelationshipRows",
					LineHint: 14,
				},
				Cypher: `
					MATCH (r:Repository {id: $repo_id})-[rel:CORRELATES_DEPLOYABLE_UNIT]->(target:Repository)
					RETURN target.id AS target_id
					ORDER BY target_id
					LIMIT $limit
				`,
				RequiredAnchors: []Anchor{{Label: "Repository", Property: "id"}},
				RequiredSchema:  []string{"repository_id"},
				RequiredLimits:  []string{"$limit"},
				RequiresOrder:   true,
				Plan: PlanExpectation{
					Operators:          []string{"NodeIndexSeek", "Expand"},
					ForbiddenOperators: []string{"AllNodesScan", "CartesianProduct"},
				},
			},
			{
				ID:        "supply_chain_impact_readiness",
				Domain:    "supply_chain",
				Backend:   "postgres",
				QueryKind: "sql_read_model",
				Source: SourceRef{
					File:   "go/internal/query/supply_chain_impact.go",
					Symbol: "ListSupplyChainImpactFindings",
				},
				Caveats: []string{"impact readiness is reducer-owned SQL/read-model evidence, not hot Cypher"},
			},
		},
	}

	if err := ValidateManifest(manifest, schemaStatements()); err != nil {
		t.Fatalf("ValidateManifest() error = %v", err)
	}
}

func TestValidateManifestRejectsMissingRequiredHotPath(t *testing.T) {
	manifest := Manifest{
		Version:     1,
		RequiredIDs: []string{"deployable_unit_relationships"},
		Entries:     []Entry{},
	}

	err := ValidateManifest(manifest, schemaStatements())
	if err == nil || !strings.Contains(err.Error(), "missing required hot path deployable_unit_relationships") {
		t.Fatalf("ValidateManifest() error = %v, want missing required hot path", err)
	}
}

func TestValidateManifestRejectsUnboundedVariableLengthTraversal(t *testing.T) {
	manifest := singleCypherManifest(`
		MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS*]->(n:File)
		RETURN n.path AS path
		ORDER BY path
		LIMIT $limit
	`)

	err := ValidateManifest(manifest, schemaStatements())
	if err == nil || !strings.Contains(err.Error(), "unbounded variable-length traversal") {
		t.Fatalf("ValidateManifest() error = %v, want unbounded traversal failure", err)
	}
}

func TestValidateManifestAcceptsCountStarAggregation(t *testing.T) {
	manifest := singleCypherManifest(`
		MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
		RETURN count(*) AS files
		ORDER BY files
		LIMIT $limit
	`)

	if err := ValidateManifest(manifest, schemaStatements()); err != nil {
		t.Fatalf("ValidateManifest() error = %v", err)
	}
}

func TestValidateManifestRejectsUnlabeledAnchors(t *testing.T) {
	manifest := singleCypherManifest(`
		MATCH (n)
		WHERE n.id = $entity_id
		RETURN n
		LIMIT 1
	`)

	err := ValidateManifest(manifest, schemaStatements())
	if err == nil || !strings.Contains(err.Error(), "unlabeled MATCH anchor") {
		t.Fatalf("ValidateManifest() error = %v, want unlabeled anchor failure", err)
	}
}

func TestValidateManifestRejectsOffsetWithoutOrdering(t *testing.T) {
	manifest := singleCypherManifest(`
		MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
		RETURN f.path AS path
		SKIP $offset
		LIMIT $limit
	`)

	err := ValidateManifest(manifest, schemaStatements())
	if err == nil || !strings.Contains(err.Error(), "SKIP without ORDER BY") {
		t.Fatalf("ValidateManifest() error = %v, want SKIP ordering failure", err)
	}
}

func TestValidateManifestRejectsForbiddenPlanOperators(t *testing.T) {
	manifest := singleCypherManifest(`
		MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
		RETURN f.path AS path
		ORDER BY path
		LIMIT $limit
	`)
	manifest.Entries[0].Plan = PlanExpectation{
		Operators:          []string{"NodeIndexSeek", "AllNodesScan"},
		ForbiddenOperators: []string{"AllNodesScan", "CartesianProduct"},
	}

	err := ValidateManifest(manifest, schemaStatements())
	if err == nil || !strings.Contains(err.Error(), "forbidden plan operator AllNodesScan") {
		t.Fatalf("ValidateManifest() error = %v, want forbidden plan operator failure", err)
	}
}

func TestValidateManifestRejectsMissingSchemaEvidence(t *testing.T) {
	manifest := singleCypherManifest(`
		MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
		RETURN f.path AS path
		ORDER BY path
		LIMIT $limit
	`)
	manifest.Entries[0].RequiredSchema = []string{"missing_repo_id_index"}

	err := ValidateManifest(manifest, schemaStatements())
	if err == nil || !strings.Contains(err.Error(), "missing schema evidence missing_repo_id_index") {
		t.Fatalf("ValidateManifest() error = %v, want missing schema evidence failure", err)
	}
}

func singleCypherManifest(cypher string) Manifest {
	return Manifest{
		Version:     1,
		RequiredIDs: []string{"hot_path"},
		Entries: []Entry{
			{
				ID:        "hot_path",
				Domain:    "code",
				Backend:   "nornicdb",
				QueryKind: "cypher",
				Source: SourceRef{
					File:   "go/internal/query/example.go",
					Symbol: "example",
				},
				Cypher:          cypher,
				RequiredAnchors: []Anchor{{Label: "Repository", Property: "id"}},
				RequiredSchema:  []string{"repository_id"},
				RequiredLimits:  []string{"$limit"},
				RequiresOrder:   strings.Contains(cypher, "SKIP"),
			},
		},
	}
}

func schemaStatements() []string {
	return []string{
		"CREATE CONSTRAINT repository_id IF NOT EXISTS FOR (r:Repository) REQUIRE r.id IS UNIQUE",
	}
}
