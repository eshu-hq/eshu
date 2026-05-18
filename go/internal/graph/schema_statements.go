package graph

import "fmt"

// fulltextIndex pairs a primary procedure-based fulltext statement with
// its modern CREATE FULLTEXT INDEX fallback.
type fulltextIndex struct {
	primary  string
	fallback string
}

// SchemaStatements returns the complete ordered list of Cypher statements
// that EnsureSchema would execute. Useful for inspection and testing.
func SchemaStatements() []string {
	// Keep the legacy no-argument helper on Neo4j. Runtime bootstraps that
	// need the configured default call SchemaStatementsForBackend instead.
	stmts, _ := SchemaStatementsForBackend(SchemaBackendNeo4j)
	return stmts
}

// SchemaStatementsForBackend returns the ordered Cypher schema statements for
// a specific graph backend without executing them.
func SchemaStatementsForBackend(backend SchemaBackend) ([]string, error) {
	dialect, err := schemaDialectForBackend(backend)
	if err != nil {
		return nil, err
	}

	stmts := make([]string, 0,
		len(schemaConstraints)+
			len(uidConstraintLabels)+
			len(schemaPerformanceIndexes)+
			len(schemaFulltextIndexes))
	for _, cypher := range schemaConstraints {
		if cypher = dialect.constraint(cypher); cypher != "" {
			stmts = append(stmts, cypher)
		}
	}
	stmts = append(stmts, schemaPerformanceIndexes...)
	if dialect.includeMergeLookupIndexes {
		stmts = append(stmts, nornicDBMergeLookupIndexes...)
		stmts = append(stmts, nornicDBUIDLookupIndexes()...)
	}
	for _, label := range uidConstraintLabels {
		stmts = append(stmts, fmt.Sprintf(
			"CREATE CONSTRAINT %s_uid_unique IF NOT EXISTS FOR (n:%s) REQUIRE n.uid IS UNIQUE",
			labelToSnake(label), label,
		))
	}
	for _, ft := range schemaFulltextIndexes {
		stmts = append(stmts, ft.primary)
	}
	return stmts, nil
}
