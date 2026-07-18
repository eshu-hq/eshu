// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"strings"
	"testing"
)

func TestBindProductionCypherRejectsDriftAndMissingShapes(t *testing.T) {
	production := "MATCH (r:Repository {id: $repo_id}) RETURN r.id"
	manifest := Manifest{Entries: []Entry{{
		ID:           "QP-HOT",
		QueryKind:    queryKindCypher,
		CypherSHA256: ProductionCypherSHA256(production),
	}}}

	bound, err := BindProductionCypher(manifest, map[string]string{"QP-HOT": production})
	if err != nil {
		t.Fatalf("BindProductionCypher() error = %v", err)
	}
	if got := bound.Entries[0].Cypher; got != production {
		t.Fatalf("bound Cypher = %q, want exact production text %q", got, production)
	}

	manifest.Entries[0].CypherSHA256 = strings.Repeat("0", 64)
	_, err = BindProductionCypher(manifest, map[string]string{"QP-HOT": production})
	if err == nil || !strings.Contains(err.Error(), "production Cypher SHA-256 mismatch") {
		t.Fatalf("BindProductionCypher() error = %v, want drift rejection", err)
	}

	manifest.Entries[0].CypherSHA256 = ProductionCypherSHA256(production)
	_, err = BindProductionCypher(manifest, nil)
	if err == nil || !strings.Contains(err.Error(), "missing production Cypher") {
		t.Fatalf("BindProductionCypher() error = %v, want missing production shape", err)
	}
}

func TestBindProductionCypherRejectsCopiedOrUnregisteredShapes(t *testing.T) {
	production := "MATCH (r:Repository {id: $repo_id}) RETURN r.id"
	manifest := Manifest{Entries: []Entry{{
		ID:           "QP-HOT",
		QueryKind:    queryKindCypher,
		Cypher:       production,
		CypherSHA256: ProductionCypherSHA256(production),
	}}}

	_, err := BindProductionCypher(manifest, map[string]string{"QP-HOT": production})
	if err == nil || !strings.Contains(err.Error(), "manifest must not copy production Cypher") {
		t.Fatalf("BindProductionCypher() error = %v, want copied-query rejection", err)
	}

	manifest.Entries[0].Cypher = ""
	_, err = BindProductionCypher(manifest, map[string]string{
		"QP-HOT":   production,
		"QP-EXTRA": "MATCH (n) RETURN n",
	})
	if err == nil || !strings.Contains(err.Error(), "unregistered production Cypher QP-EXTRA") {
		t.Fatalf("BindProductionCypher() error = %v, want extra production shape rejection", err)
	}
}
