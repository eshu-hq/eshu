// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// unusedStoreQueryer fails any query it is asked. The blank-input
// authorizer test below never reaches the database (blank inputs fail
// closed before any read), so any query through this double is a test
// failure, not an empty result.
type unusedStoreQueryer struct{}

func (unusedStoreQueryer) QueryContext(
	context.Context,
	string,
	...any,
) (*sql.Rows, error) {
	return nil, fmt.Errorf("query must not run for blank authorizer inputs")
}

func TestResolveDurableIncidentRepositoriesQueryShape(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		"fact.fact_kind = 'incident.record'",
		"fact.payload->'service'->>'id'",
		"correlation.fact_kind = 'reducer_incident_repository_correlation'",
		"correlation.payload->>'provider_service_id'",
		"correlation.payload->>'provenance_only' = 'false'",
		"NULLIF(correlation.payload->>'repository_id', '') IS NOT NULL",
		"generation.status = 'active'",
	} {
		if !strings.Contains(resolveDurableIncidentRepositoriesQuery, fragment) {
			t.Fatalf("durable incident repository query missing %q:\n%s", fragment, resolveDurableIncidentRepositoriesQuery)
		}
	}
}

func TestPostgresIncidentRepositoryAuthorizerBlankInputsSkipRead(t *testing.T) {
	t.Parallel()

	authorizer := PostgresIncidentRepositoryAuthorizer{DB: unusedStoreQueryer{}}
	repositories, err := authorizer.ResolveDurableIncidentRepositories(context.Background(), "", "", "")
	if err != nil {
		t.Fatalf("ResolveDurableIncidentRepositories() error = %v, want nil", err)
	}
	if len(repositories) != 0 {
		t.Fatalf("repositories = %#v, want empty", repositories)
	}
}
