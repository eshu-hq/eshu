// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
	"github.com/eshu-hq/eshu/go/internal/query/selector"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// routeFailureCanary stands in for backend error text: a host, a relation,
// and a credential-shaped token that must never reach a response body
// (#7674).
const routeFailureCanary = `host=db.internal relation "content_files" password=hunter2`

// routeStepFailureCases returns the canary 500, reader-fence 503, and
// client-cancel 499 cases every converted route step must classify.
func routeStepFailureCases() []repoServerFailureCase {
	return []repoServerFailureCase{
		{"backend failure", errors.New("private backend: " + routeFailureCanary), false, http.StatusInternalServerError, true},
		{"reader fence", fmt.Errorf("private read store %s: %w", routeFailureCanary, db.ErrReaderStale), false, http.StatusServiceUnavailable, false},
		{"client cancel", fmt.Errorf("private read store %s: %w", routeFailureCanary, context.Canceled), true, querycontract.StatusClientClosedRequest, false},
	}
}

// routeStepStore fails exactly one named content read with err and answers
// every other read from the embedded fixture store.
type routeStepStore struct {
	content.FakePortContentStore
	step string
	err  error
}

func (s routeStepStore) fail(step string) error {
	if s.step == step {
		return s.err
	}
	return nil
}

func (s routeStepStore) ListRepositories(ctx context.Context) ([]querycontract.RepositoryCatalogEntry, error) {
	if err := s.fail("list_repositories"); err != nil {
		return nil, err
	}
	return s.FakePortContentStore.ListRepositories(ctx)
}

func (s routeStepStore) ResolveRepository(ctx context.Context, selector string) (*querycontract.RepositoryCatalogEntry, error) {
	if err := s.fail("resolve_repository"); err != nil {
		return nil, err
	}
	return s.FakePortContentStore.ResolveRepository(ctx, selector)
}

func (s routeStepStore) ListRepoEntities(ctx context.Context, repoID string, limit int) ([]querycontract.EntityContent, error) {
	if err := s.fail("repo_entities"); err != nil {
		return nil, err
	}
	return s.FakePortContentStore.ListRepoEntities(ctx, repoID, limit)
}

func (s routeStepStore) ListRepoFiles(ctx context.Context, repoID string, limit int) ([]querycontract.FileContent, error) {
	if err := s.fail("repo_files"); err != nil {
		return nil, err
	}
	return s.FakePortContentStore.ListRepoFiles(ctx, repoID, limit)
}

func (s routeStepStore) GetFileContent(ctx context.Context, repoID, relativePath string) (*querycontract.FileContent, error) {
	if err := s.fail("file_content"); err != nil {
		return nil, err
	}
	return s.FakePortContentStore.GetFileContent(ctx, repoID, relativePath)
}

func (s routeStepStore) ListRepositoryRefs(ctx context.Context, repoID string) ([]repository.RepositoryRef, error) {
	if err := s.fail("repository_refs"); err != nil {
		return nil, err
	}
	return s.FakePortContentStore.ListRepositoryRefs(ctx, repoID)
}

func (s routeStepStore) ReadRepositoriesByLanguage(
	ctx context.Context, languages []string, limit, offset int, allScopes bool, repoIDs, scopeIDs []string,
) (querycontract.RepositoryLanguageAggregate, []querycontract.RepositoryLanguageRepository, error) {
	if err := s.fail("read_by_language"); err != nil {
		return querycontract.RepositoryLanguageAggregate{}, nil, err
	}
	return s.FakePortContentStore.ReadRepositoriesByLanguage(ctx, languages, limit, offset, allScopes, repoIDs, scopeIDs)
}

func (s routeStepStore) CountRepositoriesByLanguage(
	ctx context.Context, languages []string, allScopes bool, repoIDs, scopeIDs []string,
) (querycontract.RepositoryLanguageAggregate, error) {
	if err := s.fail("count_by_language"); err != nil {
		return querycontract.RepositoryLanguageAggregate{}, err
	}
	return s.FakePortContentStore.CountRepositoriesByLanguage(ctx, languages, allScopes, repoIDs, scopeIDs)
}

func (s routeStepStore) RepositoryLanguageInventory(
	ctx context.Context, limit, offset int, allScopes bool, repoIDs, scopeIDs []string,
) ([]querycontract.RepositoryLanguageInventoryRow, error) {
	if err := s.fail("language_inventory"); err != nil {
		return nil, err
	}
	return s.FakePortContentStore.RepositoryLanguageInventory(ctx, limit, offset, allScopes, repoIDs, scopeIDs)
}

// stepStore builds a routeStepStore over the one-repository fixture catalog.
func stepStore(step string, err error) routeStepStore {
	return routeStepStore{
		FakePortContentStore: content.FakePortContentStore{
			Repositories: []querycontract.RepositoryCatalogEntry{testutil.RepositoryStatsCatalogEntry()},
		},
		step: step,
		err:  err,
	}
}

// storyGraph answers the repository base lookup and fails every other graph
// read with err; a nil err answers no rows.
func storyGraph(err error) graph.FakeRepoGraphReader {
	return graph.FakeRepoGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if cypher == repositoryBaseCypher {
				return testutil.RepositoryStatsGraphRow(), nil
			}
			return nil, err
		},
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return nil, err
		},
	}
}

// failingGraph fails every graph read with err.
func failingGraph(err error) graph.FakeRepoGraphReader {
	fail := func(context.Context, string, map[string]any) (map[string]any, error) { return nil, err }
	return graph.FakeRepoGraphReader{
		RunSingleFn: fail,
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return nil, err
		},
	}
}

// routeStep is one repository route step whose failure must answer message.
type routeStep struct {
	name    string
	path    string
	message string
	handler func(err error) *Handler
}

const stepRepoPath = "/api/v0/repositories/repository:r_ok"

func repositoryRouteSteps() []routeStep {
	readme := []querycontract.FileContent{{RelativePath: "README.md"}}
	return []routeStep{
		{"list/content catalog", "/api/v0/repositories", repositoryListContentFailedMessage, func(err error) *Handler {
			return &Handler{Content: stepStore("list_repositories", err)}
		}},
		{"list/count", "/api/v0/repositories", repositoryListCountFailedMessage, func(err error) *Handler {
			return &Handler{Neo4j: failingGraph(err)}
		}},
		{"list/page", "/api/v0/repositories", repositoryListPageFailedMessage, func(err error) *Handler {
			g := failingGraph(err)
			g.RunSingleFn = func(context.Context, string, map[string]any) (map[string]any, error) {
				return map[string]any{"total": int64(1)}, nil
			}
			return &Handler{Neo4j: g}
		}},
		{"catalog/content repositories", "/api/v0/catalog", catalogContentRepositoriesFailedMessage, func(err error) *Handler {
			return &Handler{Content: stepStore("list_repositories", err)}
		}},
		{"catalog/repositories", "/api/v0/catalog", catalogRepositoriesFailedMessage, func(err error) *Handler {
			return &Handler{Neo4j: failingGraph(err)}
		}},
		{"context/lookup", stepRepoPath + "/context", repositoryContextLookupFailedMessage, func(err error) *Handler {
			return &Handler{Neo4j: failingGraph(err)}
		}},
		{"story/lookup", stepRepoPath + "/story", repositoryStoryLookupFailedMessage, func(err error) *Handler {
			return &Handler{Neo4j: failingGraph(err)}
		}},
		{"story/graph summary", stepRepoPath + "/story", repositoryStoryGraphSummaryFailedMessage, func(err error) *Handler {
			return &Handler{Neo4j: storyGraph(err)}
		}},
		{"story/semantic overview", stepRepoPath + "/story", repositoryStorySemanticOverviewFailedMessage, func(err error) *Handler {
			return &Handler{Neo4j: storyGraph(nil), Content: stepStore("repo_entities", err)}
		}},
		{"story/narrative files", stepRepoPath + "/story", repositoryStoryNarrativeFilesFailedMessage, func(err error) *Handler {
			store := stepStore("file_content", err)
			store.RepoFiles = readme
			return &Handler{Neo4j: storyGraph(nil), Content: store}
		}},
		{"story/deployment evidence", stepRepoPath + "/story", repositoryStoryDeploymentEvidenceFailedMessage, func(err error) *Handler {
			store := stepStore("", nil)
			store.DeploymentEvidenceErr = err
			return &Handler{Neo4j: storyGraph(nil), Content: store}
		}},
		{"story/target documentation", stepRepoPath + "/story", repositoryStoryTargetDocumentationFailedMessage, func(err error) *Handler {
			store := stepStore("", nil)
			store.DocumentationFindingsErr = err
			return &Handler{Neo4j: storyGraph(nil), Content: store}
		}},
		{"story/target support", stepRepoPath + "/story", repositoryStoryTargetSupportFailedMessage, func(err error) *Handler {
			store := stepStore("", nil)
			store.TargetSupportErr = err
			return &Handler{Neo4j: storyGraph(nil), Content: store}
		}},
		{"tree/lookup", stepRepoPath + "/tree", repositoryTreeLookupFailedMessage, func(err error) *Handler {
			return &Handler{Content: stepStore("resolve_repository", err)}
		}},
		{"tree/files", stepRepoPath + "/tree", repositoryTreeFilesFailedMessage, func(err error) *Handler {
			return &Handler{Content: stepStore("repo_files", err)}
		}},
		{"tree/refs", stepRepoPath + "/tree?ref=main", repositoryTreeRefsFailedMessage, func(err error) *Handler {
			return &Handler{Content: stepStore("repository_refs", err)}
		}},
		{"content/lookup", stepRepoPath + "/content?path=README.md", repositoryContentLookupFailedMessage, func(err error) *Handler {
			return &Handler{Content: stepStore("resolve_repository", err)}
		}},
		{"content/read", stepRepoPath + "/content?path=README.md", repositoryContentReadFailedMessage, func(err error) *Handler {
			return &Handler{Content: stepStore("file_content", err)}
		}},
		{"content/refs", stepRepoPath + "/content?path=README.md&ref=main", repositoryContentRefsFailedMessage, func(err error) *Handler {
			store := stepStore("repository_refs", err)
			store.RepoFiles = []querycontract.FileContent{{RelativePath: "README.md", Content: "# hi", CommitSHA: "abc123"}}
			return &Handler{Content: store}
		}},
		{"branches/lookup", stepRepoPath + "/branches", repositoryBranchesLookupFailedMessage, func(err error) *Handler {
			return &Handler{Content: stepStore("resolve_repository", err)}
		}},
		{"branches/refs", stepRepoPath + "/branches", repositoryBranchesRefsFailedMessage, func(err error) *Handler {
			return &Handler{Content: stepStore("repository_refs", err)}
		}},
		{"freshness/read", stepRepoPath + "/freshness", repositoryFreshnessReadFailedMessage, func(err error) *Handler {
			return &Handler{Content: stepStore("", nil), Freshness: &testutil.FakeRepositoryFreshnessReader{Err: err}}
		}},
		{"freshness/lookup", stepRepoPath + "/freshness", repositoryFreshnessLookupFailedMessage, func(err error) *Handler {
			reader := &testutil.FakeRepositoryFreshnessReader{Snapshot: testutil.FullyBuiltRepositoryFreshnessSnapshot()}
			return &Handler{Content: stepStore("resolve_repository", err), Freshness: reader}
		}},
		{"by-language/read", "/api/v0/repositories/by-language?language=go", repositoriesByLanguageReadFailedMessage, func(err error) *Handler {
			return &Handler{Content: stepStore("read_by_language", err)}
		}},
		{"by-language/count", "/api/v0/repositories/by-language?language=go&limit=0", repositoriesByLanguageCountFailedMessage, func(err error) *Handler {
			return &Handler{Content: stepStore("count_by_language", err)}
		}},
		{"language-inventory", "/api/v0/repositories/language-inventory", repositoryLanguageInventoryFailedMessage, func(err error) *Handler {
			return &Handler{Content: stepStore("language_inventory", err)}
		}},
	}
}

// TestRepositoryRouteStepFailuresAnswerFixedMessages is the #7674 regression
// for every repository route step that answered a read failure with the
// backend error text in the body ("query failed: <err>") and no span signal.
// Each step must answer its own fixed message with the error on the span, a
// reader fence the retryable 503 with Retry-After, and a client cancel 499
// with only the client-cancel span event.
func TestRepositoryRouteStepFailuresAnswerFixedMessages(t *testing.T) {
	t.Parallel()

	for _, step := range repositoryRouteSteps() {
		for _, tc := range routeStepFailureCases() {
			t.Run(step.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				serveRepoServerFailure(t, step.handler(tc.err), step.path, step.message, tc)
			})
		}
	}
}

// TestRepositoryRouteStepMessagesAreDistinct pins that no two route steps
// share a message, so the body alone names the failing read.
func TestRepositoryRouteStepMessagesAreDistinct(t *testing.T) {
	t.Parallel()

	seen := map[string]string{}
	for _, step := range repositoryRouteSteps() {
		if other, ok := seen[step.message]; ok {
			t.Fatalf("steps %q and %q share the message %q", other, step.name, step.message)
		}
		seen[step.message] = step.name
	}
}

// repoFailureDetail returns the "detail" field of a querycontract.WriteError
// body.
func repoFailureDetail(t *testing.T, raw string) string {
	t.Helper()
	var body struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("decode body %q: %v", raw, err)
	}
	return body.Detail
}

// TestWriteSelectorAnswerKeepsWrapperTextOut pins the #7674 selector answer:
// the path selector wrote err.Error() under a computed 400/404 status, so a
// wrapper around a typed selector error would reach the body. A not-found or
// ambiguous selector answers only the typed error's own text, and any other
// error answers the fixed lookup-failure 500.
func TestWriteSelectorAnswerKeepsWrapperTextOut(t *testing.T) {
	t.Parallel()

	notFound := selector.NotFoundError{Selector: "payments"}
	ambiguous := selector.AmbiguousError{Selector: "payments", Matches: []string{"repository:r_a", "repository:r_b"}}
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantDetail string
	}{
		{"not found", fmt.Errorf("private wrapper %s: %w", routeFailureCanary, notFound), http.StatusNotFound, notFound.Error()},
		{"ambiguous", fmt.Errorf("private wrapper %s: %w", routeFailureCanary, ambiguous), http.StatusBadRequest, ambiguous.Error()},
		{"unclassified", errors.New("private wrapper " + routeFailureCanary), http.StatusInternalServerError, selector.LookupFailureMessage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			writeSelectorAnswer(rec, httptest.NewRequest(http.MethodGet, "/", nil), tc.err)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if got := repoFailureDetail(t, rec.Body.String()); got != tc.wantDetail {
				t.Fatalf("detail = %q, want %q", got, tc.wantDetail)
			}
		})
	}
}
