// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package declaration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
)

// The scale shape follows the #8816 evidence note
// (docs/internal/evidence/8816-runtime-declaration-graph.md): ten declarations
// per repository, one in twelve runtimes per slot so node is a third of the
// graph, and one declaration in ten with no resolved major (alias, unresolved
// or range, rotating). ESHU_8817_PERF_REPOS sets the repository count (default
// 5,000 = 50,000 declarations); ESHU_8817_P95_BUDGET_MS sets the p95 the
// scenarios must stay under (default: the local capability budget, 1,000 ms).

var perfRuntimes = []string{"node", "python", "go", "ruby", "rust", "java", "node", "php", "dotnet", "node", "elixir", "swift"}

var perfSourceKinds = []string{"nvmrc", "tool_versions", "package_json_engines", "go_mod_go", "rust_toolchain"}

func perfEnvInt(name string, fallback int) int {
	if raw := os.Getenv(name); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			return v
		}
	}
	return fallback
}

func perfCorpus(repos int) []map[string]any {
	rows := make([]map[string]any, 0, repos*10)
	for i := 0; i < repos; i++ {
		repo := liveRepoID(i)
		for j := 0; j < 10; j++ {
			runtime := perfRuntimes[(i*10+j)%len(perfRuntimes)]
			kind := perfSourceKinds[(i+j)%len(perfSourceKinds)]
			path := ".tool-versions"
			if j%3 == 1 {
				path = "services/svc" + strconv.Itoa(j) + "/.nvmrc"
			}
			scope := "."
			if strings.HasPrefix(path, "services/") {
				scope = "services/svc" + strconv.Itoa(j)
			}
			if j == 9 {
				specs := []struct{ raw, kind string }{{"lts/*", "alias"}, {"${NODE_VERSION}", "unresolved"}, {">=18 <21", "range"}}
				spec := specs[i%len(specs)]
				rows = append(rows, liveRow(repo, path, j+1, 0, runtime, kind, spec.raw, spec.kind, scope))
				continue
			}
			major := (i + j) % 30
			rows = append(rows, liveRow(repo, path, j+1, 0, runtime, kind, strconv.Itoa(major)+".1.0", "exact", scope, major, 1, 0))
		}
	}
	return rows
}

type perfScenario struct {
	name string
	body string
	// auth is non-nil for a scoped caller.
	scoped []string
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

func (s perfScenario) run(t *testing.T, h http.Handler, iterations int) (cold time.Duration, samples []time.Duration, bodyBytes int, rows int) {
	t.Helper()
	call := func() (time.Duration, int, int) {
		req := httptest.NewRequest(http.MethodPost, "/api/v0/runtime-declarations/query", strings.NewReader(s.body))
		if s.scoped != nil {
			req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
				Mode: auth.AuthModeScoped, TenantID: "t", WorkspaceID: "t", SubjectClass: "team",
				SubjectIDHash: "sha256:t", PolicyRevisionHash: "sha256:p", AllowedRepositoryIDs: s.scoped,
			}))
		}
		rec := httptest.NewRecorder()
		start := time.Now()
		h.ServeHTTP(rec, req)
		elapsed := time.Since(start)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", s.name, rec.Code, rec.Body.String())
		}
		return elapsed, rec.Body.Len(), decodeList(t, rec.Body.Bytes()).Count
	}
	cold, bodyBytes, rows = call()
	for i := 0; i < 5; i++ {
		call()
	}
	samples = make([]time.Duration, 0, iterations)
	for i := 0; i < iterations; i++ {
		d, _, _ := call()
		samples = append(samples, d)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return cold, samples, bodyBytes, rows
}

// TestLivePerfListScenariosStayUnderTheCapabilityBudget measures the route end
// to end (handler, Bolt, JSON) at scale and fails when any scenario's p95
// exceeds the budget. It is a measurement harness: the numbers it logs are the
// evidence recorded in docs/internal/evidence/8817-runtime-declaration-query.md.
func TestLivePerfListScenariosStayUnderTheCapabilityBudget(t *testing.T) {
	repos := perfEnvInt("ESHU_8817_PERF_REPOS", 5000)
	budget := time.Duration(perfEnvInt("ESHU_8817_P95_BUDGET_MS", 1000)) * time.Millisecond
	iterations := perfEnvInt("ESHU_8817_PERF_ITERATIONS", 100)

	store := openLiveStore(t)
	store.reset(t)
	t.Cleanup(func() { store.reset(t) })
	corpus := perfCorpus(repos)
	seedStart := time.Now()
	store.seed(t, corpus, 500)
	store.write(t, `MATCH (r:Repository) WHERE r.id STARTS WITH 'repository:live8817-' SET r.name = 'name-' + r.id`, nil)
	t.Logf("seeded %d declarations across %d repositories in %s", len(corpus), repos, time.Since(seedStart).Round(time.Millisecond))

	mux := http.NewServeMux()
	(&Handler{Neo4j: store.reader}).Mount(mux)

	// A cursor deep into the node-below-20 walk: 25 pages of 200 rows in.
	cursor := ""
	for page := 0; page < 25; page++ {
		body := `{"runtime":"node","major_below":20,"limit":200`
		if cursor != "" {
			body += `,"cursor":"` + cursor + `"`
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v0/runtime-declarations/query", strings.NewReader(body+"}"))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		got := decodeList(t, rec.Body.Bytes())
		if !got.Truncated {
			break
		}
		cursor = got.NextCursor
	}
	hundred := make([]string, 100)
	for i := range hundred {
		hundred[i] = `"` + liveRepoID(i*(repos/100)) + `"`
	}
	granted := make([]string, 100)
	for i := range granted {
		granted[i] = liveRepoID(i)
	}

	scenarios := []perfScenario{
		{name: "node major_below 20, first page of 50", body: `{"runtime":"node","major_below":20}`},
		{name: "node major_below 20, first page of 200", body: `{"runtime":"node","major_below":20,"limit":200}`},
		{name: "node major_below 20, page 26 (cursor)", body: `{"runtime":"node","major_below":20,"limit":200,"cursor":"` + cursor + `"}`},
		{name: "node version_below 20.10.1, first page of 50", body: `{"runtime":"node","version_below":"20.10.1"}`},
		{name: "node + source_kind nvmrc major_below 20", body: `{"runtime":"node","source_kind":"nvmrc","major_below":20}`},
		{name: "no filters, first page of 50", body: `{}`},
		{name: "source_kind only, first page of 50", body: `{"source_kind":"go_mod_go"}`},
		{name: "100 repo_ids, no runtime", body: `{"repo_ids":[` + strings.Join(hundred, ",") + `]}`},
		{name: "100 repo_ids, node major_below 20", body: `{"runtime":"node","major_below":20,"repo_ids":[` + strings.Join(hundred, ",") + `]}`},
		{name: "scoped caller (100 granted repos), node major_below 20", body: `{"runtime":"node","major_below":20}`, scoped: granted},
	}
	t.Logf("budget p95 %s; %d iterations after 1 cold + 5 warm-up calls", budget, iterations)
	t.Logf("%-58s %8s %8s %8s %8s %8s %6s %8s", "scenario", "cold", "p50", "p95", "p99", "max", "rows", "bytes")
	for _, s := range scenarios {
		cold, samples, size, rows := s.run(t, mux, iterations)
		p95 := percentile(samples, 0.95)
		t.Logf("%-58s %8s %8s %8s %8s %8s %6d %8d", s.name,
			cold.Round(time.Millisecond), percentile(samples, 0.5).Round(100*time.Microsecond), p95.Round(100*time.Microsecond),
			percentile(samples, 0.99).Round(100*time.Microsecond), samples[len(samples)-1].Round(100*time.Microsecond), rows, size)
		if p95 > budget {
			t.Errorf("%s: p95 %s exceeds the %s capability budget", s.name, p95, budget)
		}
	}

	// The repository context section, one repository per call.
	sectionSamples := make([]time.Duration, 0, iterations)
	for i := 0; i < iterations+5; i++ {
		start := time.Now()
		if _, err := LoadSection(context.Background(), store.reader, nil, liveRepoID((i*37)%repos)); err != nil {
			t.Fatalf("LoadSection: %v", err)
		}
		if i >= 5 {
			sectionSamples = append(sectionSamples, time.Since(start))
		}
	}
	sort.Slice(sectionSamples, func(i, j int) bool { return sectionSamples[i] < sectionSamples[j] })
	t.Logf("%-58s %8s %8s %8s %8s %8s", fmt.Sprintf("repository context section (%d distinct repos)", iterations), "-",
		percentile(sectionSamples, 0.5).Round(100*time.Microsecond), percentile(sectionSamples, 0.95).Round(100*time.Microsecond),
		percentile(sectionSamples, 0.99).Round(100*time.Microsecond), sectionSamples[len(sectionSamples)-1].Round(100*time.Microsecond))
	if p95 := percentile(sectionSamples, 0.95); p95 > budget {
		t.Errorf("context section p95 %s exceeds the %s budget", p95, budget)
	}
}
