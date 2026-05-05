package cypher

import (
	"context"
	"fmt"
)

// CypherReader runs a read-only Cypher query and reports whether it produced
// at least one row.
type CypherReader interface {
	QueryCypherExists(ctx context.Context, cypher string, params map[string]any) (bool, error)
}

// CanonicalNodeChecker checks for the existence of canonical code entity nodes
// (Function, Class, File) in the graph. When none exist, the code-call
// materialization handler can short-circuit to avoid expensive label-free
// MATCH scans across millions of SourceLocalRecord nodes.
type CanonicalNodeChecker struct {
	reader CypherReader
}

// NewCanonicalNodeChecker returns a checker backed by the given CypherReader.
func NewCanonicalNodeChecker(reader CypherReader) *CanonicalNodeChecker {
	return &CanonicalNodeChecker{reader: reader}
}

const canonicalCodeTargetExistsCypher = `MATCH (n)
WHERE n:Function OR n:Class OR n:File
RETURN true LIMIT 1`

// HasCanonicalCodeTargets returns true if at least one Function, Class, or
// File node exists in the graph.
func (c *CanonicalNodeChecker) HasCanonicalCodeTargets(ctx context.Context) (bool, error) {
	if c.reader == nil {
		return false, fmt.Errorf("canonical node checker: reader is required")
	}
	return c.reader.QueryCypherExists(ctx, canonicalCodeTargetExistsCypher, nil)
}
