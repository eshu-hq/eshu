package main

import (
	"strings"
	"testing"
)

func TestNeo4jProfileGroupStatementsParsesOptIn(t *testing.T) {
	t.Parallel()

	enabled, err := neo4jProfileGroupStatements(func(key string) string {
		if key == "ESHU_NEO4J_PROFILE_GROUP_STATEMENTS" {
			return "true"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("neo4jProfileGroupStatements() error = %v, want nil", err)
	}
	if !enabled {
		t.Fatal("neo4jProfileGroupStatements() = false, want true")
	}
}

func TestNeo4jProfileGroupStatementsRejectsInvalidBool(t *testing.T) {
	t.Parallel()

	_, err := neo4jProfileGroupStatements(func(key string) string {
		if key == "ESHU_NEO4J_PROFILE_GROUP_STATEMENTS" {
			return "sometimes"
		}
		return ""
	})
	if err == nil {
		t.Fatal("neo4jProfileGroupStatements() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "ESHU_NEO4J_PROFILE_GROUP_STATEMENTS") {
		t.Fatalf("error = %q, want env var name", err.Error())
	}
}
