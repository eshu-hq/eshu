package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
)

func TestNeo4jReaderSatisfiesBackendConformanceGraphQuery(t *testing.T) {
	t.Parallel()

	var _ backendconformance.GraphQuery = (*Neo4jReader)(nil)
}
