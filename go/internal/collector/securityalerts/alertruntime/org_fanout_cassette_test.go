// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// providerCassette is a recorded GitHub Dependabot request/response exchange.
// Each scenario replays its interactions strictly in order against the real
// GitHubDependabotClient, so the test proves the exact provider request count
// and sequence a claim sends, not a fake client's view of it.
type providerCassette struct {
	Description string                     `json:"description"`
	Scenarios   []providerCassetteScenario `json:"scenarios"`
}

type providerCassetteScenario struct {
	Name                string                        `json:"name"`
	MaxPages            int                           `json:"max_pages"`
	AllowedRepositories []string                      `json:"allowed_repositories"`
	Interactions        []providerCassetteInteraction `json:"interactions"`
	Expect              providerCassetteExpect        `json:"expect"`
}

type providerCassetteInteraction struct {
	Request struct {
		Method string            `json:"method"`
		Path   string            `json:"path"`
		Query  map[string]string `json:"query"`
	} `json:"request"`
	Response struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
		Body    json.RawMessage   `json:"body"`
	} `json:"response"`
}

type providerCassetteExpect struct {
	Requests int `json:"requests"`
	Facts    []struct {
		Repository string   `json:"repository"`
		Number     int64    `json:"number"`
		Coverage   string   `json:"coverage"`
		Reasons    []string `json:"reasons"`
	} `json:"facts"`
	Metrics []struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
		Value  int64             `json:"value"`
	} `json:"metrics"`
}

// TestClaimedSourceOrgFanoutReplaysProviderCassette is the #8805 cassette
// proof: a truncated org read fans out with the recorded request count, and a
// rate-limited per-repository read stops the fan-out after exactly one failed
// request instead of retrying or reading the remaining repositories.
func TestClaimedSourceOrgFanoutReplaysProviderCassette(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("testdata/org_fanout_cassette.json")
	if err != nil {
		t.Fatalf("read cassette: %v", err)
	}
	var cassette providerCassette
	if err := json.Unmarshal(raw, &cassette); err != nil {
		t.Fatalf("decode cassette: %v", err)
	}
	if len(cassette.Scenarios) == 0 {
		t.Fatal("cassette has no scenarios")
	}
	for _, scenario := range cassette.Scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			t.Parallel()
			replayProviderCassetteScenario(t, scenario)
		})
	}
}

func replayProviderCassetteScenario(t *testing.T, scenario providerCassetteScenario) {
	t.Helper()
	var mu sync.Mutex
	next := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		index := next
		next++
		mu.Unlock()
		if index >= len(scenario.Interactions) {
			t.Errorf("unexpected request %d %s %s beyond the %d recorded", index+1, r.Method, r.URL.String(), len(scenario.Interactions))
			http.Error(w, "{}", http.StatusInternalServerError)
			return
		}
		recorded := scenario.Interactions[index]
		if got := r.Header.Get("Authorization"); got != "Bearer github-token" {
			t.Errorf("request %d Authorization = %q, want the configured bearer token", index+1, got)
		}
		if r.Method != recorded.Request.Method || r.URL.Path != recorded.Request.Path || !queryMatches(r, recorded.Request.Query) {
			t.Errorf("request %d = %s %s, want %s %s %v", index+1, r.Method, r.URL.String(),
				recorded.Request.Method, recorded.Request.Path, recorded.Request.Query)
		}
		for key, value := range recorded.Response.Headers {
			w.Header().Set(key, strings.ReplaceAll(value, "{{base_url}}", server.URL))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(recorded.Response.Status)
		_, _ = w.Write(recorded.Response.Body)
	}))
	t.Cleanup(server.Close)

	instruments, reader := newTestInstruments(t)
	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider:            ProviderGitHubDependabot,
			Scope:               TargetScopeOrganization,
			ScopeID:             fanoutTestOrgScopeID,
			Organization:        "example-org",
			Token:               "github-token",
			APIBaseURL:          server.URL,
			MaxPages:            scenario.MaxPages,
			AllowedRepositories: scenario.AllowedRepositories,
		}},
		Now:         func() time.Time { return now },
		Instruments: instruments,
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	envs, _ := collectOrgFacts(t, source)

	mu.Lock()
	requests := next
	mu.Unlock()
	if requests != scenario.Expect.Requests {
		t.Fatalf("provider requests = %d, want the recorded %d", requests, scenario.Expect.Requests)
	}
	if got, want := len(envs), len(scenario.Expect.Facts); got != want {
		t.Fatalf("len(facts) = %d, want %d", got, want)
	}
	for i, want := range scenario.Expect.Facts {
		assertCassetteFact(t, envs[i], want.Repository, want.Number, want.Coverage, want.Reasons)
	}
	for _, metric := range scenario.Expect.Metrics {
		if got := counterValue(t, reader, metric.Name, metric.Labels); got != metric.Value {
			t.Fatalf("%s%v = %d, want %d", metric.Name, metric.Labels, got, metric.Value)
		}
	}
}

func assertCassetteFact(t *testing.T, env facts.Envelope, repository string, number int64, coverage string, reasons []string) {
	t.Helper()
	if got, want := payloadString(env.Payload, "repository_id"), "security-alert:github:"+repository; got != want {
		t.Fatalf("repository_id = %q, want %q", got, want)
	}
	if got := payloadInt(env.Payload, "provider_alert_number"); got != number {
		t.Fatalf("%s provider_alert_number = %d, want %d", repository, got, number)
	}
	if got := payloadString(env.Payload, "collection_coverage_state"); got != coverage {
		t.Fatalf("%s#%d coverage = %q, want %q", repository, number, got, coverage)
	}
	if got := payloadStrings(env.Payload, "collection_incomplete_reasons"); !slices.Equal(got, reasons) && (len(got) != 0 || len(reasons) != 0) {
		t.Fatalf("%s#%d collection_incomplete_reasons = %v, want %v", repository, number, got, reasons)
	}
	if strings.Contains(payloadString(env.Payload, "source_url"), "github-token") {
		t.Fatalf("%s#%d source_url leaks the token", repository, number)
	}
}

func queryMatches(r *http.Request, want map[string]string) bool {
	got := r.URL.Query()
	if len(got) != len(want) {
		return false
	}
	for key, value := range want {
		if got.Get(key) != value {
			return false
		}
	}
	return true
}
