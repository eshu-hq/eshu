// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	entitycontract "github.com/eshu-hq/eshu/go/internal/query/querycontract/entity"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// entityFailureSuite wraps routes in a suite with no handler tracer: entity
// handlers open no spans of their own, so every check reads the request span.
func entityFailureSuite(routes ...failure.Route) failure.Suite {
	return failure.Suite{Routes: routes}
}

// runEntityFailureRoutes drives every route through every failure case.
func runEntityFailureRoutes(t *testing.T, routes []failure.Route) {
	t.Helper()
	failure.Run(t, entityFailureSuite(routes...))
}

// entityFailureRoute builds the failure route for one entity read step: the
// step answers its fixed message, maps a reader fence to the retryable 503,
// and answers a client cancel with 499.
func entityFailureRoute(
	name, method, path, body, message string,
	build func(err error) *Handler,
) failure.Route {
	return failure.Route{
		Name: name, Method: method, Path: path, Body: body, Message: message,
		Handler: func(err error) failure.Mounter { return build(err) },
	}
}

// entitySentinelCase is a configuration-state 503: err wraps sentinel under
// wrapper text that carries the canary, and the route must answer 503 with
// only the sentinel's own text. It runs as a Fault on a 503-fault route.
func entitySentinelCase(sentinel error) failure.Case {
	return failure.Case{
		Name: "sentinel " + sentinel.Error(),
		Kind: failure.Fault,
		Err:  fmt.Errorf("private wrapper %s: %w", failure.Canary, sentinel),
	}
}

// entityBareSentinelCase is a configuration-state 503 the route raises
// itself, with no wrapper text.
func entityBareSentinelCase(sentinel error) failure.Case {
	return failure.Case{
		Name: "sentinel " + sentinel.Error(),
		Kind: failure.Fault,
		Err:  sentinel,
	}
}

// entitySentinelRoute builds the one-case route for a sentinel 503 on path:
// the sentinel's own text answers 503 with the span marked as a fault.
func entitySentinelRoute(
	name, method, path, body string,
	sentinel error,
	build func() *Handler,
) failure.Route {
	return failure.Route{
		Name: name, Method: method, Path: path, Body: body,
		Message: sentinel.Error(), FaultStatus: http.StatusServiceUnavailable,
		Handler: func(error) failure.Mounter { return build() },
	}
}

// checkEntitySentinel runs the single sentinel case for route and reports a
// wrong answer as a test error.
func checkEntitySentinel(t *testing.T, route failure.Route, tc failure.Case) {
	t.Helper()
	if err := failure.Check(entityFailureSuite(route), route, tc); err != nil {
		t.Error(err)
	}
}

// routeFailureContent is a content store whose reads fail with the
// configured errors; a nil error keeps the embedded fake's empty answer.
type routeFailureContent struct {
	content.FakePortContentStore
	entity        *querycontract.EntityContent
	getEntityErr  error
	byNameErr     error
	byIDsErr      error
	nameSearchErr error
}

func (f routeFailureContent) GetEntityContent(context.Context, string) (*querycontract.EntityContent, error) {
	return f.entity, f.getEntityErr
}

func (f routeFailureContent) SearchEntitiesByName(context.Context, string, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, f.byNameErr
}

func (f routeFailureContent) ListRepoEntitiesByIDs(ctx context.Context, repoID string, ids []string, limit int) ([]querycontract.EntityContent, error) {
	if f.byIDsErr != nil {
		return nil, f.byIDsErr
	}
	return f.FakePortContentStore.ListRepoEntitiesByIDs(ctx, repoID, ids, limit)
}

// ListRepoEntitiesByKeys finds no key match and, unlike the embedded fake,
// ignores a canceled context, so a client-cancel case reaches the step under
// test instead of failing in metadata enrichment first.
func (f routeFailureContent) ListRepoEntitiesByKeys(context.Context, string, []querycontract.EntityContentKey) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (f routeFailureContent) SearchEntityNames(context.Context, entitycontract.EntityNameSearch) ([]querycontract.EntityContent, error) {
	return nil, f.nameSearchErr
}
