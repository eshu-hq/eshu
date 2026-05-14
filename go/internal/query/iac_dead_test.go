package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

type fakeIaCDeadContentStore struct {
	fakePortContentStore
	files map[string][]FileContent
}

func (f fakeIaCDeadContentStore) ListRepoFiles(_ context.Context, repoID string, _ int) ([]FileContent, error) {
	return append([]FileContent(nil), f.files[repoID]...), nil
}

func (f fakeIaCDeadContentStore) GetFileContent(_ context.Context, repoID, relativePath string) (*FileContent, error) {
	for _, file := range f.files[repoID] {
		if file.RelativePath == relativePath {
			cloned := file
			return &cloned, nil
		}
	}
	return nil, nil
}

func (f fakeIaCDeadContentStore) MatchRepositories(
	ctx context.Context,
	selector string,
) ([]RepositoryCatalogEntry, error) {
	entries, err := f.fakePortContentStore.MatchRepositories(ctx, selector)
	if err != nil || len(entries) > 0 || len(f.repositories) > 0 {
		return entries, err
	}
	if _, ok := f.files[selector]; !ok {
		return nil, nil
	}
	return []RepositoryCatalogEntry{{ID: selector, Name: selector}}, nil
}

type fakeIaCReachabilityStore struct {
	rows            []IaCReachabilityFindingRow
	hasRows         bool
	observedRepoIDs *[]string
}

func (f fakeIaCReachabilityStore) ListLatestCleanupFindings(
	_ context.Context,
	repoIDs []string,
	families []string,
	includeAmbiguous bool,
	limit int,
	offset int,
) ([]IaCReachabilityFindingRow, error) {
	if f.observedRepoIDs != nil {
		*f.observedRepoIDs = append([]string(nil), repoIDs...)
	}
	filter := map[string]bool{}
	for _, family := range families {
		filter[family] = true
	}
	rows := make([]IaCReachabilityFindingRow, 0, len(f.rows))
	for _, row := range f.rows {
		if len(filter) > 0 && !filter[row.Family] {
			continue
		}
		if row.Reachability == "ambiguous" && !includeAmbiguous {
			continue
		}
		rows = append(rows, row)
	}
	if offset > len(rows) {
		return nil, nil
	}
	rows = rows[offset:]
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (f fakeIaCReachabilityStore) HasLatestRows(_ context.Context, _ []string, families []string) (bool, error) {
	if f.hasRows {
		return true, nil
	}
	if len(families) == 0 {
		return len(f.rows) > 0, nil
	}
	for _, row := range f.rows {
		for _, family := range families {
			if row.Family == family {
				return true, nil
			}
		}
	}
	return false, nil
}

func (f fakeIaCReachabilityStore) CountLatestCleanupFindings(
	_ context.Context,
	_ []string,
	families []string,
	includeAmbiguous bool,
) (int, error) {
	filter := map[string]bool{}
	for _, family := range families {
		filter[family] = true
	}
	var count int
	for _, row := range f.rows {
		if len(filter) > 0 && !filter[row.Family] {
			continue
		}
		if row.Reachability == "ambiguous" && !includeAmbiguous {
			continue
		}
		if row.Reachability == "unused" || row.Reachability == "ambiguous" {
			count++
		}
	}
	return count, nil
}

func TestHandleDeadIaCPrefersMaterializedReachabilityRows(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Reachability: fakeIaCReachabilityStore{rows: []IaCReachabilityFindingRow{
			{
				ID:           "terraform:terraform-modules:modules/orphan-cache",
				Family:       "terraform",
				RepoID:       "terraform-modules",
				ArtifactPath: "modules/orphan-cache",
				Reachability: "unused",
				Finding:      "candidate_dead_iac",
				Confidence:   0.75,
				Evidence:     []string{"modules/orphan-cache/main.tf: module directory exists"},
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/dead", bytes.NewBufferString(`{
		"repo_ids": ["terraform-modules"],
		"include_ambiguous": true
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	if got, want := data["analysis_status"], "materialized_reachability"; got != want {
		t.Fatalf("analysis_status = %q, want %q", got, want)
	}
	if got, want := data["truth_basis"], "materialized_reducer_rows"; got != want {
		t.Fatalf("truth_basis = %q, want %q", got, want)
	}
	rawFindings := data["findings"].([]any)
	if got, want := len(rawFindings), 1; got != want {
		t.Fatalf("findings len = %d, want %d", got, want)
	}
}

func TestHandleDeadIaCResolvesRepositorySelectorAliasesForMaterializedRows(t *testing.T) {
	t.Parallel()

	var observedRepoIDs []string
	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Content: fakeIaCDeadContentStore{
			fakePortContentStore: fakePortContentStore{
				repositories: []RepositoryCatalogEntry{
					{ID: "repository:r_modules", Name: "terraform-modules"},
				},
			},
		},
		Reachability: fakeIaCReachabilityStore{
			observedRepoIDs: &observedRepoIDs,
			rows: []IaCReachabilityFindingRow{
				{
					ID:           "terraform:repository:r_modules:modules/orphan-cache",
					Family:       "terraform",
					RepoID:       "repository:r_modules",
					ArtifactPath: "modules/orphan-cache",
					Reachability: "unused",
					Finding:      "candidate_dead_iac",
					Confidence:   0.75,
					Evidence:     []string{"modules/orphan-cache/main.tf: module directory exists"},
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/dead", bytes.NewBufferString(`{
		"repo_ids": ["terraform-modules"],
		"include_ambiguous": true
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := observedRepoIDs, []string{"repository:r_modules"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("reachability repoIDs = %#v, want %#v", got, want)
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	if got, want := data["truth_basis"], "materialized_reducer_rows"; got != want {
		t.Fatalf("truth_basis = %q, want %q", got, want)
	}
	if got, want := data["repo_ids"], []any{"repository:r_modules"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("repo_ids = %#v, want %#v", got, want)
	}
	rawFindings := data["findings"].([]any)
	finding := rawFindings[0].(map[string]any)
	if got, want := finding["repo_name"], "terraform-modules"; got != want {
		t.Fatalf("repo_name = %#v, want %#v", got, want)
	}
}

func TestHandleDeadIaCMaterializedRowsReportsPagination(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Reachability: fakeIaCReachabilityStore{rows: []IaCReachabilityFindingRow{
			{ID: "terraform:repo:modules/a", Family: "terraform", RepoID: "repo", ArtifactPath: "modules/a", Reachability: "unused", Finding: "candidate_dead_iac", Confidence: 0.75},
			{ID: "terraform:repo:modules/b", Family: "terraform", RepoID: "repo", ArtifactPath: "modules/b", Reachability: "unused", Finding: "candidate_dead_iac", Confidence: 0.75},
			{ID: "terraform:repo:modules/c", Family: "terraform", RepoID: "repo", ArtifactPath: "modules/c", Reachability: "unused", Finding: "candidate_dead_iac", Confidence: 0.75},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/dead", bytes.NewBufferString(`{
		"repo_ids": ["repo"],
		"limit": 2
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	if got, want := int(data["findings_count"].(float64)), 2; got != want {
		t.Fatalf("findings_count = %d, want %d", got, want)
	}
	if got, want := int(data["total_findings_count"].(float64)), 3; got != want {
		t.Fatalf("total_findings_count = %d, want %d", got, want)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if got, want := int(data["next_offset"].(float64)), 2; got != want {
		t.Fatalf("next_offset = %d, want %d", got, want)
	}
}

func TestHandleDeadIaCMaterializedRowsHonorFamilyFilter(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Reachability: fakeIaCReachabilityStore{rows: []IaCReachabilityFindingRow{
			{
				ID:           "helm:helm-charts:charts/orphan-worker",
				Family:       "helm",
				RepoID:       "helm-charts",
				ArtifactPath: "charts/orphan-worker",
				Reachability: "unused",
				Finding:      "candidate_dead_iac",
				Confidence:   0.75,
			},
			{
				ID:           "terraform:terraform-modules:modules/orphan-cache",
				Family:       "terraform",
				RepoID:       "terraform-modules",
				ArtifactPath: "modules/orphan-cache",
				Reachability: "unused",
				Finding:      "candidate_dead_iac",
				Confidence:   0.75,
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/dead", bytes.NewBufferString(`{
		"repo_ids": ["terraform-modules", "helm-charts"],
		"families": ["terraform"]
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	rawFindings := data["findings"].([]any)
	if got, want := len(rawFindings), 1; got != want {
		t.Fatalf("findings len = %d, want %d", got, want)
	}
	row := rawFindings[0].(map[string]any)
	if got, want := row["family"], "terraform"; got != want {
		t.Fatalf("family = %#v, want %#v", got, want)
	}
}

func TestHandleDeadIaCUsesMaterializedEmptyResultWhenRowsExist(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile:      ProfileLocalAuthoritative,
		Reachability: fakeIaCReachabilityStore{hasRows: true},
		Content: fakeIaCDeadContentStore{
			files: map[string][]FileContent{
				"terraform-modules": {
					{RepoID: "terraform-modules", RelativePath: "modules/orphan-cache/main.tf", Content: `resource "aws_elasticache_cluster" "this" {}`},
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/dead", bytes.NewBufferString(`{
		"repo_ids": ["terraform-modules"]
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	if got, want := data["truth_basis"], "materialized_reducer_rows"; got != want {
		t.Fatalf("truth_basis = %q, want %q", got, want)
	}
	if got, want := int(data["findings_count"].(float64)), 0; got != want {
		t.Fatalf("findings_count = %d, want %d", got, want)
	}
}

func TestHandleDeadIaCRequiresExplicitScope(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{Profile: ProfileLocalAuthoritative, Content: fakeIaCDeadContentStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/dead", bytes.NewBufferString(`{}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}
