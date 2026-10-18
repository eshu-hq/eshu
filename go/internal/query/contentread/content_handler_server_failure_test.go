// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// failingContentReadStore fails every content read and search the five
// content routes issue after selector resolution with err.
type failingContentReadStore struct {
	content.FakePortContentStore
	err error
}

func (s failingContentReadStore) GetFileContent(context.Context, string, string) (*querycontract.FileContent, error) {
	return nil, s.err
}

func (s failingContentReadStore) GetFileLines(context.Context, string, string, int, int) (*querycontract.FileContent, error) {
	return nil, s.err
}

func (s failingContentReadStore) GetEntityContent(context.Context, string) (*querycontract.EntityContent, error) {
	return nil, s.err
}

func (s failingContentReadStore) SearchFileContent(context.Context, string, string, int) ([]querycontract.FileContent, error) {
	return nil, s.err
}

func (s failingContentReadStore) SearchFileContentAnyRepo(context.Context, string, int) ([]querycontract.FileContent, error) {
	return nil, s.err
}

func (s failingContentReadStore) SearchEntityContent(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, s.err
}

func (s failingContentReadStore) SearchEntityContentAnyRepo(context.Context, string, int) ([]querycontract.EntityContent, error) {
	return nil, s.err
}

// TestContentRoutesServerFailureAnswersFixedMessage is the #7626 regression
// for the content reads that run after the selector resolved. A failed read
// answered 500 with err.Error() as the body, which carries backend text, and
// left the span untouched. It must answer the route's fixed message with a
// span error, a reader fence must answer the retryable 503, and a client
// cancel must answer 499 without a span error. The content routes open no
// handler span of their own, so the suite names no HandlerTracer and the
// helper checks the request span.
func TestContentRoutesServerFailureAnswersFixedMessage(t *testing.T) {
	t.Parallel()

	route := func(name, path, body, message string) failure.Route {
		return failure.Route{
			Name: name, Method: http.MethodPost, Path: path, Body: body, Message: message,
			Handler: func(err error) failure.Mounter {
				return &ContentHandler{Content: failingContentReadStore{err: err}}
			},
		}
	}
	failure.Run(t, failure.Suite{Routes: []failure.Route{
		route("file read", "/api/v0/content/files/read", `{"repo_id":"repository:r_ok","relative_path":"src/app.go"}`, contentFileReadFailedMessage),
		route("file lines", "/api/v0/content/files/lines", `{"repo_id":"repository:r_ok","relative_path":"src/app.go","start_line":1,"end_line":2}`, contentFileReadFailedMessage),
		route("entity read", "/api/v0/content/entities/read", `{"entity_id":"content-entity:e_1"}`, contentEntityReadFailedMessage),
		route("file search repo", "/api/v0/content/files/search", `{"repo_id":"repository:r_ok","query":"handler"}`, contentFileSearchFailedMessage),
		route("file search any repo", "/api/v0/content/files/search", `{"query":"handler"}`, contentFileSearchFailedMessage),
		route("entity search repo", "/api/v0/content/entities/search", `{"repo_id":"repository:r_ok","query":"handler"}`, contentEntitySearchFailedMessage),
		route("entity search any repo", "/api/v0/content/entities/search", `{"query":"handler"}`, contentEntitySearchFailedMessage),
	}})
}
