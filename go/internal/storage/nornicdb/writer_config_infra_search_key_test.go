// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nornicdb

import (
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
)

// TestConfigureCanonicalWriterUsesNornicDBInfraSearchKeyShape proves the
// NornicDB seam always yields the NornicDB statement shape, whatever dialect
// the writer was built with: NornicDB cannot evaluate the #7245 key clause.
func TestConfigureCanonicalWriterUsesNornicDBInfraSearchKeyShape(t *testing.T) {
	t.Parallel()

	for _, built := range []*sourcecypher.CanonicalNodeWriter{
		sourcecypher.NewCanonicalNodeWriter(nil, 0, nil),
		sourcecypher.NewCanonicalNodeWriterWithDialect(nil, 0, nil, search.On),
		sourcecypher.NewCanonicalNodeWriterWithDialect(nil, 0, nil, search.Off),
	} {
		got := ConfigureCanonicalWriter(built, DefaultWriterConfig()).InfraSearchKeyDialect()
		if got != search.Off {
			t.Fatalf("ConfigureCanonicalWriter dialect = %v, want %v", got, search.Off)
		}
	}
}
