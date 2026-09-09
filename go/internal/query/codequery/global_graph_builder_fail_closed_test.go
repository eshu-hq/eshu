// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestDirectGlobalGraphSearchDoesNotCallGraph(t *testing.T) {
	t.Parallel()
	graph := &captureGraphQuery{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
		t.Fatal("direct global graph search called GraphQuery")
		return nil, nil
	}}
	_, err := (&CodeHandler{Neo4j: graph}).searchGraphEntitiesWithExact(context.Background(), "", "proof", "", 10, true)
	if !errors.Is(err, querycontract.ErrGlobalGraphEntitySearchUnsupported) {
		t.Fatalf("error = %v, want fail-closed global graph error", err)
	}
}

// captureGraphQuery is a minimal GraphQuery stub that records nothing and
// answers nothing; the tests above fail on any graph call, so the stub only
// has to exist to satisfy the handler struct. Twin of the same-named stub in
// package query (repository_context_tech_fingerprint_test.go): a _test.go
// symbol is not importable across the package boundary (#6060).
type captureGraphQuery struct {
	RunFn func(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}

func (c *captureGraphQuery) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if c.RunFn != nil {
		return c.RunFn(ctx, cypher, params)
	}
	return nil, nil
}

func (c *captureGraphQuery) RunSingle(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return nil, nil
}
