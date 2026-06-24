package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// catalogGraphRows routes the catalog handler's bounded graph queries to fake
// result sets. Each query is anchored on a distinct fragment so the fake can
// return per-query scalar rows, mirroring the backend-portable assembly the
// handler performs.
type catalogGraphRows struct {
	repositories []map[string]any
	base         []map[string]any
	repo         []map[string]any
	instance     []map[string]any
	evidence     []map[string]any
}

func (rows catalogGraphRows) reader(t *testing.T, wantLimit int) fakeRepoGraphReader {
	t.Helper()
	return fakeRepoGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "MATCH (r:Repository)"):
				return rows.repositories, nil
			case strings.Contains(cypher, "EVIDENCES_REPOSITORY_RELATIONSHIP"):
				return rows.evidence, nil
			case strings.Contains(cypher, "MATCH (inst:WorkloadInstance)"):
				return rows.instance, nil
			case strings.Contains(cypher, "(w:Workload)<-[:DEFINES]-(repo:Repository)"):
				return rows.repo, nil
			case strings.Contains(cypher, "MATCH (w:Workload)"):
				if wantLimit > 0 {
					if got, want := params["limit"], wantLimit; got != want {
						t.Fatalf("workload limit param = %#v, want %#v", got, want)
					}
				}
				return rows.base, nil
			default:
				return nil, nil
			}
		},
	}
}

func TestListCatalogReturnsRepositoriesWorkloadsAndServices(t *testing.T) {
	t.Parallel()

	rows := catalogGraphRows{
		repositories: []map[string]any{
			{"id": "repository:r_api", "name": "svc-catalog", "local_path": "/repos/svc-catalog"},
		},
		base: []map[string]any{
			{"id": "workload:svc-catalog", "name": "svc-catalog", "kind": "service"},
			{"id": "workload:nightly-sync", "name": "nightly-sync", "kind": "cronjob"},
		},
		repo: []map[string]any{
			{"id": "workload:svc-catalog", "repo_id": "repository:r_api", "repo_name": "svc-catalog"},
			{"id": "workload:nightly-sync", "repo_id": "repository:r_api", "repo_name": "svc-catalog"},
		},
		instance: []map[string]any{
			{"id": "workload:svc-catalog", "instance_count": int64(2), "environments": []any{"prod", "qa"}},
			{"id": "workload:nightly-sync", "instance_count": int64(1), "environments": []any{"prod"}},
		},
	}

	handler := &RepositoryHandler{
		Neo4j:   rows.reader(t, 3),
		Profile: ProfileLocalAuthoritative,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/catalog?limit=2", nil)
	rec := httptest.NewRecorder()

	handler.listCatalog(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	if got, want := body["count"], float64(3); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	if got, want := body["truncated"], false; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}

	assertCatalogCollectionLength(t, body, "repositories", 1)
	assertCatalogCollectionLength(t, body, "workloads", 2)
	assertCatalogCollectionLength(t, body, "services", 1)

	services := body["services"].([]any)
	service := services[0].(map[string]any)
	assertEnvironmentSet(t, "svc-catalog", catalogEnvironments(service), []string{"prod", "qa"})
	if got, want := service["repo_name"], "svc-catalog"; got != want {
		t.Fatalf("service repo_name = %#v, want %#v", got, want)
	}
}

func TestListCatalogMergesInstanceAndDeploymentEvidenceEnvironments(t *testing.T) {
	t.Parallel()

	rows := catalogGraphRows{
		base: []map[string]any{
			{"id": "workload:svc-catalog", "name": "svc-catalog", "kind": "service"},
			{"id": "workload:svc-pricing", "name": "svc-pricing", "kind": "service"},
			{"id": "workload:svc-empty", "name": "svc-empty", "kind": "service"},
		},
		instance: []map[string]any{
			// Instance-backed service: environments only from WorkloadInstance.
			{"id": "workload:svc-catalog", "instance_count": int64(2), "environments": []any{"prod", "qa"}},
		},
		evidence: []map[string]any{
			// Instance-less service: environments only from TARGETS_ENVIRONMENT
			// deployment evidence, emitted as scalar per-edge rows.
			{"id": "workload:svc-pricing", "environment": "example-qa"},
			{"id": "workload:svc-pricing", "environment": "example-prod"},
			{"id": "workload:svc-pricing", "environment": "example-qa"},
		},
	}

	handler := &RepositoryHandler{
		Neo4j:   rows.reader(t, 0),
		Profile: ProfileLocalAuthoritative,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/catalog?limit=10", nil)
	rec := httptest.NewRecorder()

	handler.listCatalog(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	byName := map[string][]string{}
	for _, raw := range body["services"].([]any) {
		service := raw.(map[string]any)
		byName[service["name"].(string)] = catalogEnvironments(service)
	}

	assertEnvironmentSet(t, "svc-catalog", byName["svc-catalog"], []string{"prod", "qa"})
	assertEnvironmentSet(t, "svc-pricing", byName["svc-pricing"], []string{"example-prod", "example-qa"})
	if envs, ok := byName["svc-empty"]; !ok {
		t.Fatalf("svc-empty service missing from response")
	} else if len(envs) != 0 {
		t.Fatalf("svc-empty environments = %#v, want empty", envs)
	}
}

func TestListCatalogTruncatesEachCollectionByLimit(t *testing.T) {
	t.Parallel()

	rows := catalogGraphRows{
		repositories: []map[string]any{
			{"id": "repository:r_1", "name": "one"},
			{"id": "repository:r_2", "name": "two"},
		},
		base: []map[string]any{
			{"id": "workload:w_1", "name": "one", "kind": "service"},
			{"id": "workload:w_2", "name": "two", "kind": "service"},
		},
	}

	handler := &RepositoryHandler{
		Neo4j:   rows.reader(t, 0),
		Profile: ProfileLocalAuthoritative,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/catalog?limit=1", nil)
	rec := httptest.NewRecorder()

	handler.listCatalog(rec, req)

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := body["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	assertCatalogCollectionLength(t, body, "repositories", 1)
	assertCatalogCollectionLength(t, body, "workloads", 1)
	assertCatalogCollectionLength(t, body, "services", 1)
}

func TestListCatalogIncludesIdentityOnlyServicesFromReadModel(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			workloadIdentities: []CatalogWorkloadIdentityEntry{
				{
					Name:     "svc-catalog",
					RepoID:   "repository:r_api",
					RepoName: "svc-catalog",
				},
			},
		},
		Neo4j: fakeRepoGraphReader{
			runByMatch: map[string][]map[string]any{
				"MATCH (r:Repository)": {},
				"MATCH (w:Workload)":   {},
			},
		},
		Profile: ProfileLocalAuthoritative,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/catalog?limit=10", nil)
	rec := httptest.NewRecorder()

	handler.listCatalog(rec, req)

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	assertCatalogCollectionLength(t, body, "services", 1)
	services := body["services"].([]any)
	service := services[0].(map[string]any)
	if got, want := service["name"], "svc-catalog"; got != want {
		t.Fatalf("service name = %#v, want %#v", got, want)
	}
	if got, want := service["materialization_status"], "identity_only"; got != want {
		t.Fatalf("materialization_status = %#v, want %#v", got, want)
	}
}

// TestCatalogEvidenceEnvironmentCypherIsSingleChain guards the cold-plan fix
// for issue #1731. The deployment-evidence environment query MUST express its
// (workload)<-(repo)<-(artifact)->(env) relationship as a single connected
// path, not two MATCH clauses that both anchor on `repo`. On NornicDB the
// double-MATCH re-anchor cold-plans as a per-repo fanout and takes ~21s at the
// console's limit, while the single-chain shape returns the identical rows in
// single-digit milliseconds. Regressing to the double-MATCH form reintroduces
// the cold catalog timeout, so the shape is pinned here.
func TestCatalogEvidenceEnvironmentCypherIsSingleChain(t *testing.T) {
	t.Parallel()

	if strings.Contains(catalogWorkloadEvidenceEnvironmentCypher, "MATCH (repo") &&
		strings.Count(catalogWorkloadEvidenceEnvironmentCypher, "MATCH ") > 1 {
		t.Fatalf("evidence environment query must be a single connected path, got two MATCH clauses re-anchoring on repo:\n%s", catalogWorkloadEvidenceEnvironmentCypher)
	}
	if !strings.Contains(catalogWorkloadEvidenceEnvironmentCypher, "(w:Workload)<-[:DEFINES]-(repo:Repository)<-[:EVIDENCES_REPOSITORY_RELATIONSHIP]-") {
		t.Fatalf("evidence environment query must traverse workload->repo->artifact->environment as one chain, got:\n%s", catalogWorkloadEvidenceEnvironmentCypher)
	}
}

// TestCatalogWorkloadRepoCypherIsSingleChain guards the cold-plan fix for issue
// #3466. The workload->repository enrichment MUST express its
// (workload)<-[:DEFINES]-(repository) relationship as a single connected path
// anchored on the bounded workload id set, not two MATCH clauses that anchor the
// workload and then re-match (repo:Repository)-[:DEFINES]->(w). On NornicDB the
// double-MATCH re-anchor cold-plans as a full Repository label scan with per-repo
// DEFINES fanout and takes ~36s at the console's catalog limit, while the
// single-chain shape returns the identical rows in single-digit milliseconds.
// Regressing to the double-MATCH form reintroduces the catalog timeout, so the
// shape is pinned here alongside the evidence-environment guard above.
func TestCatalogWorkloadRepoCypherIsSingleChain(t *testing.T) {
	t.Parallel()

	if strings.Count(catalogWorkloadRepoCypher, "MATCH ") > 1 {
		t.Fatalf("workload repository query must be a single connected path, got multiple MATCH clauses re-anchoring on repo:\n%s", catalogWorkloadRepoCypher)
	}
	if !strings.Contains(catalogWorkloadRepoCypher, "(w:Workload)<-[:DEFINES]-(repo:Repository)") {
		t.Fatalf("workload repository query must traverse workload<-repo as one chain, got:\n%s", catalogWorkloadRepoCypher)
	}
	if !strings.Contains(catalogWorkloadRepoCypher, "WHERE w.id IN $ids") {
		t.Fatalf("workload repository query must stay bounded to the workload id set, got:\n%s", catalogWorkloadRepoCypher)
	}
}

// TestListCatalogBoundsEnrichmentQueriesToWorkloadIDs guards issue #3389. The
// three workload enrichment queries (repository, instance environments, and
// deployment-evidence environments) previously scanned the entire Workload,
// WorkloadInstance, and Environment populations regardless of the catalog
// limit. At ~500k-node scale that whole-graph aggregation timed the catalog
// endpoint out even at limit=5. Each enrichment MUST be bounded to the limited
// workload id set returned by the base query and MUST anchor on the Workload
// label with `WHERE w.id IN $ids`, mirroring the bounded-id lookups used
// elsewhere (repository_name_lookup.go, entity_workload_context.go).
func TestListCatalogBoundsEnrichmentQueriesToWorkloadIDs(t *testing.T) {
	t.Parallel()

	type capture struct {
		anchored bool
		ids      []string
	}
	captured := map[string]*capture{}
	recordIDs := func(key, cypher string, params map[string]any) {
		entry := &capture{anchored: strings.Contains(cypher, "WHERE w.id IN $ids")}
		if raw, ok := params["ids"].([]string); ok {
			entry.ids = raw
		}
		captured[key] = entry
	}

	reader := fakeRepoGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "EVIDENCES_REPOSITORY_RELATIONSHIP"):
				recordIDs("evidence", cypher, params)
				return nil, nil
			case strings.Contains(cypher, "MATCH (inst:WorkloadInstance)"):
				recordIDs("instance", cypher, params)
				return nil, nil
			case strings.Contains(cypher, "(w:Workload)<-[:DEFINES]-(repo:Repository)"):
				recordIDs("repo", cypher, params)
				return nil, nil
			case strings.Contains(cypher, "MATCH (w:Workload)"):
				return []map[string]any{
					{"id": "workload:keep_a", "name": "keep-a", "kind": "service"},
					{"id": "workload:keep_b", "name": "keep-b", "kind": "service"},
				}, nil
			default:
				return nil, nil
			}
		},
	}

	handler := &RepositoryHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/catalog?limit=10", nil)
	rec := httptest.NewRecorder()
	handler.listCatalog(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}

	wantIDs := []string{"workload:keep_a", "workload:keep_b"}
	for _, key := range []string{"repo", "instance", "evidence"} {
		entry, ok := captured[key]
		if !ok {
			t.Fatalf("enrichment query %q was not executed", key)
		}
		if !entry.anchored {
			t.Fatalf("enrichment query %q must anchor on (w:Workload) WHERE w.id IN $ids", key)
		}
		if !equalStringSets(entry.ids, wantIDs) {
			t.Fatalf("enrichment query %q ids = %#v, want bounded set %#v", key, entry.ids, wantIDs)
		}
	}
}

// TestListCatalogSkipsEnrichmentWhenNoWorkloads guards that the bounded
// enrichment queries are not issued at all when the base query returns no
// workloads, so an empty catalog never pays for a graph round trip.
func TestListCatalogSkipsEnrichmentWhenNoWorkloads(t *testing.T) {
	t.Parallel()

	enrichmentRan := false
	reader := fakeRepoGraphReader{
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "EVIDENCES_REPOSITORY_RELATIONSHIP"),
				strings.Contains(cypher, "MATCH (inst:WorkloadInstance)"),
				strings.Contains(cypher, "(w:Workload)<-[:DEFINES]-(repo:Repository)"):
				enrichmentRan = true
				return nil, nil
			default:
				return nil, nil
			}
		},
	}

	handler := &RepositoryHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/catalog?limit=10", nil)
	rec := httptest.NewRecorder()
	handler.listCatalog(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if enrichmentRan {
		t.Fatalf("enrichment queries must not run when there are no workloads")
	}
}

func equalStringSets(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	set := map[string]struct{}{}
	for _, v := range got {
		set[v] = struct{}{}
	}
	for _, v := range want {
		if _, ok := set[v]; !ok {
			return false
		}
	}
	return true
}

func catalogEnvironments(service map[string]any) []string {
	envs := []string{}
	rawEnvs, ok := service["environments"].([]any)
	if !ok {
		return envs
	}
	for _, env := range rawEnvs {
		envs = append(envs, env.(string))
	}
	return envs
}

func assertEnvironmentSet(t *testing.T, name string, got, want []string) {
	t.Helper()
	gotSet := map[string]struct{}{}
	for _, env := range got {
		gotSet[env] = struct{}{}
	}
	if len(gotSet) != len(want) {
		t.Fatalf("%s environments = %#v, want set %#v", name, got, want)
	}
	for _, env := range want {
		if _, ok := gotSet[env]; !ok {
			t.Fatalf("%s environments = %#v, missing %q", name, got, env)
		}
	}
}

func assertCatalogCollectionLength(t *testing.T, body map[string]any, key string, want int) {
	t.Helper()
	rows, ok := body[key].([]any)
	if !ok {
		t.Fatalf("%s = %#v, want array", key, body[key])
	}
	if len(rows) != want {
		t.Fatalf("len(%s) = %d, want %d", key, len(rows), want)
	}
}
