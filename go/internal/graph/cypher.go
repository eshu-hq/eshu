package graph

import "context"

// CypherStatement captures one executable Cypher statement with its
// parameters. This is the graph-package equivalent of the storage/cypher
// Statement type, defined here to avoid an import cycle between graph
// and storage/cypher.
type CypherStatement struct {
	Cypher     string
	Parameters map[string]any
}

// CypherExecutor executes Cypher statements against a graph database.
// Implementations may be a real backend driver or a recording mock for
// tests. This mirrors the storage/cypher Executor interface but lives in
// the graph package to avoid import cycles.
type CypherExecutor interface {
	ExecuteCypher(context.Context, CypherStatement) error
}
