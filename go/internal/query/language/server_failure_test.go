// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// languageSearchFailureContent fails the content-index language search.
type languageSearchFailureContent struct {
	content.FakePortContentStore
	err error
}

func (f languageSearchFailureContent) SearchEntitiesByLanguageAndType(
	context.Context, string, string, string, string, int,
) ([]querycontract.EntityContent, error) {
	return nil, f.err
}

// TestLanguageQueryReadFailuresAnswerFixedText drives each of the four
// language-query read branches through a backend fault, a client cancel, an
// inner cancel, and a stale reader (#7674).
//
// Not parallel: failure.Run swaps the package-level languageHandlerTracer.
func TestLanguageQueryReadFailuresAnswerFixedText(t *testing.T) {
	graphFails := func(err error) failure.Mounter {
		return &Handler{
			Neo4j: graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, err
			}},
			Profile: querycontract.ProfileLocalAuthoritative,
		}
	}
	contentFails := func(err error) failure.Mounter {
		return &Handler{Content: languageSearchFailureContent{err: err}, Profile: querycontract.ProfileLocalAuthoritative}
	}
	body := func(entityType string) string {
		return `{"language":"go","entity_type":"` + entityType + `","query":"Foo"}`
	}
	routes := []failure.Route{
		{Name: "guard", Body: body("guard"), Handler: graphFails},
		{Name: "graph backed", Body: body("function"), Handler: graphFails},
		{Name: "graph first content backed", Body: body("terraform_module"), Handler: graphFails},
		{Name: "content backed", Body: body("terragrunt_local"), Handler: contentFails},
	}
	for i := range routes {
		routes[i].Method = http.MethodPost
		routes[i].Path = "/api/v0/code/language-query"
		routes[i].Message = languageQueryFailedMessage
	}
	failure.Run(t, failure.Suite{HandlerTracer: &languageHandlerTracer, Routes: routes})
}
