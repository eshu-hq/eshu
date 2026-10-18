// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/entity"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestCodeContentReadFailuresAnswerFixedText drives the content-backed code
// route steps through a backend fault, a client cancel, and a stale reader
// (#7674). It swaps queryHandlerTracer through the suite, so it must not be
// parallel.
func TestCodeContentReadFailuresAnswerFixedText(t *testing.T) {
	contentOnly := func(err error) failure.Mounter {
		return &CodeHandler{Content: failingCodeContent{err: err}, Profile: ProfileLocalAuthoritative}
	}
	emptyGraph := fakeGraphReader{
		run:       func(context.Context, string, map[string]any) ([]map[string]any, error) { return nil, nil },
		runSingle: func(context.Context, string, map[string]any) (map[string]any, error) { return nil, nil },
	}
	route := func(name, path, body, message string, handler func(error) failure.Mounter) failure.Route {
		return failure.Route{
			Name: name, Method: http.MethodPost, Path: path, Body: body, Message: message,
			Handler: handler,
		}
	}
	// As in the graph test, only some content routes open a handler span
	// before their read fails; the rest record on the request span.
	failure.Run(t, failure.Suite{
		HandlerTracer: &queryHandlerTracer,
		Routes: []failure.Route{
			route("topic", "/api/v0/code/topics/investigate", `{"topic":"session refresh"}`, codeTopicFailedMessage, contentOnly),
			route("structural inventory", "/api/v0/code/structure/inventory", `{"inventory_kind":"entity","language":"go"}`, structuralInventoryFailedMessage, contentOnly),
			route("hardcoded secrets", "/api/v0/code/security/secrets/investigate", `{}`, hardcodedSecretFailedMessage, contentOnly),
			route("divergence findings", "/api/v0/code/divergence/findings", `{"repo_id":"repo-x","kind":"exact"}`, divergenceFindingsFailedMessage, contentOnly),
			route("divergence report", "/api/v0/code/divergence/report", `{"repo_id":"repo-x"}`, divergenceReportFailedMessage, contentOnly),
			route("divergence investigate", "/api/v0/code/divergence/investigate", `{"repo_id":"repo-x","kind":"exact","fingerprint":"fp"}`, divergenceMembersFailedMessage, contentOnly),
			route("divergence investigate drifted", "/api/v0/code/divergence/investigate", `{"repo_id":"repo-x","kind":"drifted","fingerprint":"fp"}`, divergenceDriftedFailedMessage, contentOnly),
		},
	})
	failure.Run(t, failure.Suite{
		Routes: []failure.Route{
			route("search content index", "/api/v0/code/search", `{"query":"renderApp","limit":10}`, codeSearchContentIndexFailedMessage, contentOnly),
			route("relationship story target", "/api/v0/code/relationships/story", `{"entity_id":"entity-1"}`, relationshipStoryTargetFailedMessage, contentOnly),
			route("call chain endpoints", "/api/v0/code/call-chain", `{"start":"a","end":"b","repo_id":"repo-1"}`, callChainEndpointFailedMessage, contentOnly),
			route(
				"relationships content fallback", "/api/v0/code/relationships", `{"entity_id":"e1"}`, relationshipContentFallbackFailedMessage,
				func(err error) failure.Mounter {
					return &CodeHandler{Neo4j: emptyGraph, Content: failingCodeContent{err: err}}
				},
			),
			route(
				"code flow", "/api/v0/code/flow/taint-path", `{"repo_id":"repo-1","language":"go","symbol":"handle","limit":5}`, codeFlowFailedMessage,
				func(err error) failure.Mounter {
					return &CodeHandler{CodeFlow: failingCodeFlow{err: err}, Profile: ProfileLocalAuthoritative}
				},
			),
			route(
				"dead code scan store", "/api/v0/code/dead-code", `{"language":"go"}`, "dead-code candidate scan failed",
				func(err error) failure.Mounter {
					return &CodeHandler{Content: &readerFenceStore{failScan: err}, Profile: ProfileLocalAuthoritative}
				},
			),
		},
	})
}

// TestCodeUnavailableSentinelsAnswerSentinelText proves every configuration-
// state 503 answers with the sentinel's own fixed text even when the error
// that carries it wraps backend text, and that a client cancel still answers
// 499 (#7674 ruling decision 4). It swaps queryHandlerTracer, so it must not
// be parallel.
func TestCodeUnavailableSentinelsAnswerSentinelText(t *testing.T) {
	wrap := func(sentinel error) error { return fmt.Errorf("backend: %s: %w", codeFailureCanary, sentinel) }
	contentFails := func(err error) *CodeHandler {
		return &CodeHandler{Content: failingCodeContent{err: err}, Profile: ProfileLocalAuthoritative}
	}
	graphFails := func(err error) *CodeHandler { return &CodeHandler{Neo4j: failingCodeGraph(err)} }
	routes := []struct {
		name     string
		path     string
		body     string
		sentinel error
		handler  func(err error) *CodeHandler
	}{
		{"search name index", "/api/v0/code/search", `{"query":"renderApp"}`, entity.ErrEntityNameSearchUnavailable, contentFails},
		{"search index not ready", "/api/v0/code/search", `{"query":"renderApp"}`, ErrContentSubstringIndexesNotReady, contentFails},
		{"symbol backend", "/api/v0/code/symbols/search", `{"symbol":"Foo"}`, errSymbolBackendUnavailable, contentFails},
		{"topic backend", "/api/v0/code/topics/investigate", `{"topic":"t"}`, ErrCodeTopicBackendUnavailable, contentFails},
		{"topic index not ready", "/api/v0/code/topics/investigate", `{"topic":"t"}`, ErrContentSubstringIndexesNotReady, contentFails},
		{"inventory", "/api/v0/code/structure/inventory", `{"inventory_kind":"entity","language":"go"}`, errStructuralInventoryUnavailable, contentFails},
		{"inventory index not ready", "/api/v0/code/structure/inventory", `{"inventory_kind":"entity","language":"go"}`, ErrContentSubstringIndexesNotReady, contentFails},
		{"hardcoded secrets", "/api/v0/code/security/secrets/investigate", `{}`, errHardcodedSecretBackendUnavailable, contentFails},
		{"divergence findings", "/api/v0/code/divergence/findings", `{"repo_id":"repo-x","kind":"exact"}`, errDivergenceFindingsUnavailable, contentFails},
		{"divergence report", "/api/v0/code/divergence/report", `{"repo_id":"repo-x"}`, errDivergenceFindingsUnavailable, contentFails},
		{"call graph metrics", "/api/v0/code/call-graph/metrics", `{"repo_id":"repo-1"}`, errCallGraphMetricsUnavailable, graphFails},
		{"import dependencies", "/api/v0/code/imports/investigate", `{"repo_id":"repo-1"}`, errImportDependencyUnavailable, graphFails},
		{
			"relationships content builder", "/api/v0/code/relationships", `{"entity_id":"e1"}`, errContentRelationshipBuilderNotConfigured,
			func(err error) *CodeHandler {
				return &CodeHandler{
					Neo4j:                fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) { return nil, nil }},
					Content:              resolvedCodeEntity{},
					ContentRelationships: failingContentRelationships{err: err},
				}
			},
		},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			message := route.sentinel.Error()
			rec, ended := serveCodeTraced(t, route.handler(wrap(route.sentinel)), route.path, route.body, false)
			assertNoCodeFailureLeak(t, rec)
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
			}
			if got := codeFailureBodyMessage(t, rec); got != message {
				t.Fatalf("body message = %q, want the sentinel text %q", got, message)
			}
			if rec.Header().Get("Retry-After") != "" {
				t.Fatalf("Retry-After = %q on a configuration-state 503, want none", rec.Header().Get("Retry-After"))
			}
			assertCodeFailureSpan(t, message, false, ended)
		})
		t.Run(route.name+"/client cancel", func(t *testing.T) {
			err := fmt.Errorf("%w: %w", wrap(route.sentinel), context.Canceled)
			rec, ended := serveCodeTraced(t, route.handler(err), route.path, route.body, true)
			assertNoCodeFailureLeak(t, rec)
			if rec.Code != querycontract.StatusClientClosedRequest {
				t.Fatalf("status = %d, want 499; body = %s", rec.Code, rec.Body.String())
			}
			assertCodeFailureSpan(t, route.sentinel.Error(), true, ended)
		})
	}
}

// failingCodeContent fails every content read a code route makes with err; a
// read it does not override answers the embedded fake's empty result.
type failingCodeContent struct {
	content.FakePortContentStore
	err error
}

func (s failingCodeContent) GetEntityContent(context.Context, string) (*EntityContent, error) {
	return nil, s.err
}

func (s failingCodeContent) SearchEntitiesByName(context.Context, string, string, string, int) ([]EntityContent, error) {
	return nil, s.err
}

func (s failingCodeContent) SearchEntityNames(context.Context, entity.EntityNameSearch) ([]EntityContent, error) {
	return nil, s.err
}

func (s failingCodeContent) SearchSymbols(context.Context, SymbolSearchRequest) ([]EntityContent, error) {
	return nil, s.err
}

func (s failingCodeContent) InvestigateCodeTopic(context.Context, CodeTopicInvestigationRequest) ([]CodeTopicEvidenceRow, error) {
	return nil, s.err
}

func (s failingCodeContent) InspectStructuralInventory(context.Context, StructuralInventoryRequest) ([]EntityContent, error) {
	return nil, s.err
}

func (s failingCodeContent) CountStructuralInventoryByFile(
	context.Context, StructuralInventoryRequest,
) ([]StructuralInventoryFileCount, error) {
	return nil, s.err
}

func (s failingCodeContent) InvestigateHardcodedSecrets(
	context.Context, HardcodedSecretInvestigationRequest,
) ([]HardcodedSecretFindingRow, error) {
	return nil, s.err
}

func (s failingCodeContent) DivergenceGroupStats(context.Context, string, codedivergence.Kind, int) ([]codedivergence.GroupStat, error) {
	return nil, s.err
}

func (s failingCodeContent) DivergenceMembers(
	context.Context, string, codedivergence.Kind, []string,
) (map[string][]codedivergence.Member, error) {
	return nil, s.err
}

func (s failingCodeContent) DivergenceMembersByEntityID(context.Context, string, []string) (map[string]codedivergence.Member, error) {
	return nil, s.err
}

func (s failingCodeContent) DriftedFindingStats(context.Context, string) ([]codedivergence.GroupStat, error) {
	return nil, s.err
}

func (s failingCodeContent) DriftedFindingRows(context.Context, string, []string) (map[string]codedivergence.DriftedRow, error) {
	return nil, s.err
}

// failingCodeFlow fails the code-flow read with err.
type failingCodeFlow struct{ err error }

func (s failingCodeFlow) ListCodeFlow(context.Context, codemodel.CodeFlowFilter) (codemodel.CodeFlowReadModel, error) {
	return codemodel.CodeFlowReadModel{}, s.err
}

// failingContentRelationships fails the content relationship build with err.
type failingContentRelationships struct{ err error }

func (s failingContentRelationships) BuildContentRelationships(
	context.Context, querycontract.ContentStore, EntityContent, *slog.Logger,
) (querycontract.ContentRelationshipSet, error) {
	return querycontract.ContentRelationshipSet{}, s.err
}

// resolvedCodeEntity resolves every entity id to one entity in repo-1.
type resolvedCodeEntity struct{ content.FakePortContentStore }

func (resolvedCodeEntity) GetEntityContent(_ context.Context, entityID string) (*EntityContent, error) {
	return &EntityContent{EntityID: entityID, RepoID: "repo-1"}, nil
}
