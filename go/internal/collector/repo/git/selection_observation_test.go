// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// stubSelectionObserver is the test double for SelectionObserver: it records
// the evaluations it receives and answers with the installed outcome or
// error.
type stubSelectionObserver struct {
	outcome     scope.SelectionEvaluationOutcome
	err         error
	evaluations []scope.SelectionEvaluation
}

func (s *stubSelectionObserver) RecordSelectionEvaluation(
	_ context.Context,
	evaluation scope.SelectionEvaluation,
) (scope.SelectionEvaluationOutcome, error) {
	s.evaluations = append(s.evaluations, evaluation)
	return s.outcome, s.err
}

func selectionObserverTestInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("selection-observation-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return inst, reader
}

func selectionObserverTestLogger(logs *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// selectionCounterValue sums one counter for an exact attribute set.
func selectionCounterValue(
	t *testing.T,
	reader *sdkmetric.ManualReader,
	metricName string,
	wantAttrs map[string]string,
) int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	var total int64
	found := false
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Sum[int64]", metricName, metricRecord.Data)
			}
			for _, point := range sum.DataPoints {
				if collectorHasAttrs(point.Attributes.ToSlice(), wantAttrs) {
					found = true
					total += point.Value
				}
			}
		}
	}
	if !found {
		t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
	}
	return total
}

// selectionGaugeValue reads one gauge datapoint for an exact attribute set.
func selectionGaugeValue(
	t *testing.T,
	reader *sdkmetric.ManualReader,
	metricName string,
	wantAttrs map[string]string,
) int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			gauge, ok := metricRecord.Data.(metricdata.Gauge[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Gauge[int64]", metricName, metricRecord.Data)
			}
			for _, point := range gauge.DataPoints {
				if collectorHasAttrs(point.Attributes.ToSlice(), wantAttrs) {
					return point.Value
				}
			}
		}
	}
	t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
	return 0
}

func selectionLogMessages(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	entries := make([]map[string]any, 0)
	for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("decode log line: %v", err)
		}
		entries = append(entries, entry)
	}
	return entries
}

func selectionLogHasMessage(entries []map[string]any, message string) bool {
	for _, entry := range entries {
		if entry["msg"] == message {
			return true
		}
	}
	return false
}

func selectionObserverTestConfig() RepoSyncConfig {
	return RepoSyncConfig{
		ReposDir:                "/data/repos",
		SourceMode:              "githubOrg",
		GithubOrg:               "boatsgroup",
		GitAuthMethod:           "token",
		GitToken:                "test-token",
		RepoLimit:               4000,
		SelectionLivenessWindow: 48 * time.Hour,
	}
}

func TestSelectionSelectorIDStability(t *testing.T) {
	t.Parallel()

	config := selectionObserverTestConfig()
	config.RepositoryRules = []RepoSyncRepositoryRule{{Kind: "regex", Value: "boatsgroup/.*"}}
	first := selectionSelectorID(config, "token-one")
	second := selectionSelectorID(config, "token-one")
	if first != second {
		t.Fatalf("selectionSelectorID not stable: %q then %q", first, second)
	}
	if !strings.HasPrefix(first, "sel_") {
		t.Fatalf("selectionSelectorID = %q, want sel_ prefix", first)
	}
	if strings.Contains(first, "token-one") {
		t.Fatalf("selectionSelectorID %q leaks the token", first)
	}

	mutations := map[string]func(*RepoSyncConfig) string{
		"org":      func(config *RepoSyncConfig) string { config.GithubOrg = "other"; return "token-one" },
		"mode":     func(config *RepoSyncConfig) string { config.SourceMode = "explicit"; return "token-one" },
		"rules":    func(config *RepoSyncConfig) string { config.RepositoryRules = nil; return "token-one" },
		"archived": func(config *RepoSyncConfig) string { config.IncludeArchivedRepos = true; return "token-one" },
		"token":    func(config *RepoSyncConfig) string { return "token-two" },
	}
	for name, mutate := range mutations {
		mutated := config
		mutatedToken := mutate(&mutated)
		if got := selectionSelectorID(mutated, mutatedToken); got == first {
			t.Fatalf("selectionSelectorID unchanged after %s mutation: %q", name, got)
		}
	}
	// Rule order does not matter: the same rules in another order are the
	// same selector.
	reordered := config
	reordered.RepositoryRules = []RepoSyncRepositoryRule{
		{Kind: "exact", Value: "boatsgroup/pinned"},
		{Kind: "regex", Value: "boatsgroup/.*"},
	}
	shuffled := config
	shuffled.RepositoryRules = []RepoSyncRepositoryRule{
		{Kind: "regex", Value: "boatsgroup/.*"},
		{Kind: "exact", Value: "boatsgroup/pinned"},
	}
	if selectionSelectorID(reordered, "token-one") != selectionSelectorID(shuffled, "token-one") {
		t.Fatalf("selectionSelectorID depends on rule order")
	}
}

func TestSelectionCredentialIdentity(t *testing.T) {
	t.Parallel()

	app := selectionObserverTestConfig()
	app.GitHubAppID = "123"
	app.GitHubAppInstallation = "456"
	if got, want := selectionCredentialIdentity(app, "ignored"), "app:123:456"; got != want {
		t.Fatalf("app identity = %q, want %q", got, want)
	}

	token := selectionObserverTestConfig()
	first := selectionCredentialIdentity(token, "secret-token")
	second := selectionCredentialIdentity(token, "secret-token")
	if first != second {
		t.Fatalf("token identity not stable: %q then %q", first, second)
	}
	if !strings.HasPrefix(first, "token:") || len(first) != len("token:")+16 {
		t.Fatalf("token identity = %q, want truncated token: prefix", first)
	}
	if strings.Contains(first, "secret-token") {
		t.Fatalf("token identity %q leaks the token", first)
	}
	if selectionCredentialIdentity(token, "other-token") == first {
		t.Fatalf("token rotation did not change the identity")
	}

	if got := selectionCredentialIdentity(selectionObserverTestConfig(), ""); got != "none" {
		t.Fatalf("empty credential identity = %q, want none", got)
	}
}

func TestSelectionScopeIDForRepoMatchesManagedDerivation(t *testing.T) {
	t.Parallel()

	config := selectionObserverTestConfig()
	config.ReposDir = t.TempDir()
	// The observer derives the scope from the selection repo ID; the
	// baseline resolver derives it from the managed checkout path. Both
	// must agree or observations attach to scopes that never sync.
	for _, repoID := range []string{"boatsgroup/api", "boatsgroup/worker"} {
		checkoutName, err := repoCheckoutName(repoID)
		if err != nil {
			t.Fatalf("repoCheckoutName(%q) error = %v", repoID, err)
		}
		managed := config.ReposDir + "/" + checkoutName
		if got, want := selectionScopeIDForRepo(config, repoID), gitScopeIDForManagedRepo(config, managed); got == "" || got != want {
			t.Fatalf("selectionScopeIDForRepo(%q) = %q, managed derivation = %q", repoID, got, want)
		}
	}
	if got := selectionScopeIDForRepo(config, ""); got != "" {
		t.Fatalf("selectionScopeIDForRepo(empty) = %q, want empty", got)
	}
}

func TestObserveRepositorySelectionEvaluatesGitHubOrg(t *testing.T) {
	t.Parallel()

	config := selectionObserverTestConfig()
	observer := &stubSelectionObserver{outcome: scope.SelectionEvaluationOutcome{
		Outcome:          scope.SelectionEvaluationEvaluated,
		Selected:         2,
		ArchivedExcluded: 1,
		NotListed:        1,
	}}
	inst, reader := selectionObserverTestInstruments(t)
	var logs bytes.Buffer
	selection := RepositorySelection{
		RepositoryIDs:         []string{"boatsgroup/api", "boatsgroup/console"},
		ArchivedRepositoryIDs: []string{"boatsgroup/old"},
		GitHubIDsByRepoID:     map[string]int64{"boatsgroup/api": 101, "boatsgroup/console": 102, "boatsgroup/old": 103},
	}
	observedAt := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

	observeRepositorySelection(context.Background(), config, "token", selection, observedAt, observer, selectionObserverTestLogger(&logs), inst)

	if len(observer.evaluations) != 1 {
		t.Fatalf("evaluations recorded = %d, want 1", len(observer.evaluations))
	}
	evaluation := observer.evaluations[0]
	if evaluation.SelectorKind != scope.SelectionSelectorKindGitHubOrg {
		t.Fatalf("SelectorKind = %q, want githubOrg", evaluation.SelectorKind)
	}
	if evaluation.Org != "boatsgroup" {
		t.Fatalf("Org = %q, want boatsgroup", evaluation.Org)
	}
	if evaluation.LivenessWindowSeconds != 48*3600 {
		t.Fatalf("LivenessWindowSeconds = %d, want %d", evaluation.LivenessWindowSeconds, 48*3600)
	}
	if len(evaluation.Listed) != 2 || len(evaluation.Archived) != 1 || len(evaluation.RuleExcluded) != 0 {
		t.Fatalf("listed/archived/rule-excluded = %d/%d/%d, want 2/1/0",
			len(evaluation.Listed), len(evaluation.Archived), len(evaluation.RuleExcluded))
	}
	if evaluation.Listed[0].GitHubID != 101 || evaluation.Archived[0].GitHubID != 103 {
		t.Fatalf("github ids = %+v/%+v, want 101/103", evaluation.Listed, evaluation.Archived)
	}
	for _, evaluated := range append(append([]scope.EvaluatedRepository{}, evaluation.Listed...), evaluation.Archived...) {
		if !strings.HasPrefix(evaluated.ScopeID, "git-repository-scope:") {
			t.Fatalf("ScopeID = %q, want git-repository-scope: prefix", evaluated.ScopeID)
		}
	}

	counterAttrs := map[string]string{"outcome": "evaluated", "selector_kind": "githubOrg"}
	if got := selectionCounterValue(t, reader, "eshu_dp_collector_repository_selection_evaluations_total", counterAttrs); got != 1 {
		t.Fatalf("evaluations counter = %d, want 1", got)
	}
	if got := selectionGaugeValue(t, reader, "eshu_dp_collector_repository_selection_scopes", map[string]string{"state": "selected"}); got != 2 {
		t.Fatalf("selected gauge = %d, want 2", got)
	}
	if got := selectionGaugeValue(t, reader, "eshu_dp_collector_repository_selection_scopes", map[string]string{"state": "not_listed"}); got != 1 {
		t.Fatalf("not_listed gauge = %d, want 1", got)
	}
	if entries := selectionLogMessages(t, &logs); !selectionLogHasMessage(entries, "git_repository_selection_evaluated") {
		t.Fatalf("no git_repository_selection_evaluated INFO in %v", entries)
	}
}

func TestObserveRepositorySelectionSkipsTruncatedListing(t *testing.T) {
	t.Parallel()

	config := selectionObserverTestConfig()
	observer := &stubSelectionObserver{outcome: scope.SelectionEvaluationOutcome{Outcome: scope.SelectionEvaluationEvaluated}}
	inst, reader := selectionObserverTestInstruments(t)
	var logs bytes.Buffer
	selection := RepositorySelection{
		RepositoryIDs:    []string{"boatsgroup/api"},
		ListingTruncated: true,
	}

	observeRepositorySelection(context.Background(), config, "token", selection, time.Now().UTC(), observer, selectionObserverTestLogger(&logs), inst)

	if len(observer.evaluations) != 0 {
		t.Fatalf("evaluations recorded = %d, want 0 (truncated listing skips the store)", len(observer.evaluations))
	}
	counterAttrs := map[string]string{"outcome": "listing_truncated", "selector_kind": "githubOrg"}
	if got := selectionCounterValue(t, reader, "eshu_dp_collector_repository_selection_evaluations_total", counterAttrs); got != 1 {
		t.Fatalf("truncated counter = %d, want 1", got)
	}
	entries := selectionLogMessages(t, &logs)
	if !selectionLogHasMessage(entries, "git_repository_selection_listing_truncated") {
		t.Fatalf("no git_repository_selection_listing_truncated WARN in %v", entries)
	}
	if selectionLogHasMessage(entries, "git_repository_selection_evaluated") {
		t.Fatalf("unexpected git_repository_selection_evaluated INFO for a skipped listing")
	}
}

func TestObserveRepositorySelectionStoreErrorWarnsWithoutFailing(t *testing.T) {
	t.Parallel()

	config := selectionObserverTestConfig()
	config.GitToken = "ghp-distinctive-secret-7625"
	selector := NativeRepositorySelector{
		Config: config,
		DiscoverSelection: func(context.Context, RepoSyncConfig, string) (RepositorySelection, error) {
			return RepositorySelection{RepositoryIDs: []string{"boatsgroup/api"}}, nil
		},
		SyncGit: func(_ context.Context, _ RepoSyncConfig, repositoryIDs []string) (GitSyncSelection, error) {
			return GitSyncSelection{}, nil
		},
		SelectionObserver: &stubSelectionObserver{err: errors.New("connection refused")},
	}
	inst, reader := selectionObserverTestInstruments(t)
	selector.Instruments = inst
	var logs bytes.Buffer
	selector.Logger = selectionObserverTestLogger(&logs)

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil (store error never fails ingestion)", err)
	}
	_ = batch
	counterAttrs := map[string]string{"outcome": "store_error", "selector_kind": "githubOrg"}
	if got := selectionCounterValue(t, reader, "eshu_dp_collector_repository_selection_evaluations_total", counterAttrs); got != 1 {
		t.Fatalf("store_error counter = %d, want 1", got)
	}
	// The failure must not leak the token into the log.
	if strings.Contains(logs.String(), "ghp-distinctive-secret-7625") {
		t.Fatalf("store error log leaks the credential: %s", logs.String())
	}
	if entries := selectionLogMessages(t, &logs); !selectionLogHasMessage(entries, "git_repository_selection_store_error") {
		t.Fatalf("no git_repository_selection_store_error WARN in %v", entries)
	}
}

func TestObserveRepositorySelectionGuardTrippedWarns(t *testing.T) {
	t.Parallel()

	config := selectionObserverTestConfig()
	observer := &stubSelectionObserver{outcome: scope.SelectionEvaluationOutcome{
		Outcome:      scope.SelectionEvaluationGuardTripped,
		KnownScopes:  802,
		NewlyMissing: 100,
	}}
	inst, reader := selectionObserverTestInstruments(t)
	var logs bytes.Buffer
	selection := RepositorySelection{RepositoryIDs: []string{"boatsgroup/api"}}

	observeRepositorySelection(context.Background(), config, "token", selection, time.Now().UTC(), observer, selectionObserverTestLogger(&logs), inst)

	counterAttrs := map[string]string{"outcome": "guard_tripped", "selector_kind": "githubOrg"}
	if got := selectionCounterValue(t, reader, "eshu_dp_collector_repository_selection_evaluations_total", counterAttrs); got != 1 {
		t.Fatalf("guard_tripped counter = %d, want 1", got)
	}
	entries := selectionLogMessages(t, &logs)
	if !selectionLogHasMessage(entries, "git_repository_selection_guard_tripped") {
		t.Fatalf("no git_repository_selection_guard_tripped WARN in %v", entries)
	}
}

func TestObserveRepositorySelectionLivenessLapsed(t *testing.T) {
	t.Parallel()

	config := selectionObserverTestConfig()
	observedAt := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	observer := &stubSelectionObserver{outcome: scope.SelectionEvaluationOutcome{
		Outcome:          scope.SelectionEvaluationEvaluated,
		Selected:         1,
		PriorEvaluatedAt: observedAt.Add(-72 * time.Hour),
	}}
	inst, _ := selectionObserverTestInstruments(t)
	var logs bytes.Buffer
	selection := RepositorySelection{RepositoryIDs: []string{"boatsgroup/api"}}

	observeRepositorySelection(context.Background(), config, "token", selection, observedAt, observer, selectionObserverTestLogger(&logs), inst)

	entries := selectionLogMessages(t, &logs)
	if !selectionLogHasMessage(entries, "git_repository_selection_liveness_lapsed") {
		t.Fatalf("no git_repository_selection_liveness_lapsed WARN after a 72h gap in %v", entries)
	}
	if !selectionLogHasMessage(entries, "git_repository_selection_evaluated") {
		t.Fatalf("no git_repository_selection_evaluated INFO alongside the lapsed WARN in %v", entries)
	}

	// A gap inside the window stays quiet.
	observer.outcome.PriorEvaluatedAt = observedAt.Add(-time.Hour)
	logs.Reset()
	observeRepositorySelection(context.Background(), config, "token", selection, observedAt, observer, selectionObserverTestLogger(&logs), inst)
	if entries := selectionLogMessages(t, &logs); selectionLogHasMessage(entries, "git_repository_selection_liveness_lapsed") {
		t.Fatalf("unexpected liveness-lapsed WARN for a 1h gap in %v", entries)
	}
}

func TestObserveRepositorySelectionExplicitWritesPositiveRows(t *testing.T) {
	t.Parallel()

	config := selectionObserverTestConfig()
	config.SourceMode = "explicit"
	observer := &stubSelectionObserver{outcome: scope.SelectionEvaluationOutcome{Outcome: scope.SelectionEvaluationEvaluated, Selected: 1}}
	inst, reader := selectionObserverTestInstruments(t)
	var logs bytes.Buffer
	selection := RepositorySelection{RepositoryIDs: []string{"boatsgroup/api"}}

	observeRepositorySelection(context.Background(), config, "", selection, time.Now().UTC(), observer, selectionObserverTestLogger(&logs), inst)

	if len(observer.evaluations) != 1 {
		t.Fatalf("evaluations recorded = %d, want 1", len(observer.evaluations))
	}
	evaluation := observer.evaluations[0]
	if evaluation.SelectorKind != scope.SelectionSelectorKindExplicit {
		t.Fatalf("SelectorKind = %q, want explicit", evaluation.SelectorKind)
	}
	if evaluation.Org != "" {
		t.Fatalf("Org = %q, want empty (explicit mode does not enumerate)", evaluation.Org)
	}
	if len(evaluation.Listed) != 1 || len(evaluation.Archived) != 0 || len(evaluation.RuleExcluded) != 0 {
		t.Fatalf("listed/archived/rule-excluded = %d/%d/%d, want 1/0/0",
			len(evaluation.Listed), len(evaluation.Archived), len(evaluation.RuleExcluded))
	}
	counterAttrs := map[string]string{"outcome": "evaluated", "selector_kind": "explicit"}
	if got := selectionCounterValue(t, reader, "eshu_dp_collector_repository_selection_evaluations_total", counterAttrs); got != 1 {
		t.Fatalf("explicit evaluated counter = %d, want 1", got)
	}
}

func TestNativeRepositorySelectorEvaluatesOnlyOnShardZero(t *testing.T) {
	t.Parallel()

	run := func(shardIndex int) int {
		config := selectionObserverTestConfig()
		config.ReposDir = t.TempDir()
		config.RepoShardCount = 2
		config.RepoShardIndex = shardIndex
		observer := &stubSelectionObserver{outcome: scope.SelectionEvaluationOutcome{Outcome: scope.SelectionEvaluationEvaluated}}
		selector := NativeRepositorySelector{
			Config: config,
			DiscoverSelection: func(context.Context, RepoSyncConfig, string) (RepositorySelection, error) {
				return RepositorySelection{RepositoryIDs: []string{"boatsgroup/api", "boatsgroup/console"}}, nil
			},
			SyncGit: func(_ context.Context, _ RepoSyncConfig, _ []string) (GitSyncSelection, error) {
				return GitSyncSelection{}, nil
			},
			SelectionObserver: observer,
		}
		if _, err := selector.SelectRepositories(context.Background()); err != nil {
			t.Fatalf("SelectRepositories() error = %v", err)
		}
		return len(observer.evaluations)
	}
	if got := run(0); got != 1 {
		t.Fatalf("shard 0 evaluations = %d, want 1", got)
	}
	if got := run(1); got != 0 {
		t.Fatalf("shard 1 evaluations = %d, want 0", got)
	}
}

func TestNativeRepositorySelectorSkipsEvaluationInFilesystemMode(t *testing.T) {
	t.Parallel()

	observer := &stubSelectionObserver{outcome: scope.SelectionEvaluationOutcome{Outcome: scope.SelectionEvaluationEvaluated}}
	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:       t.TempDir(),
			SourceMode:     "filesystem",
			FilesystemRoot: t.TempDir(),
			RepoLimit:      4000,
			GitAuthMethod:  "none",
		},
		DiscoverSelection: func(context.Context, RepoSyncConfig, string) (RepositorySelection, error) {
			return RepositorySelection{RepositoryIDs: []string{"service-a"}}, nil
		},
		SyncFilesystem: func(_ context.Context, _ RepoSyncConfig, _ []string) (FilesystemSyncSelection, error) {
			return FilesystemSyncSelection{}, nil
		},
		SelectionObserver: observer,
	}
	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v", err)
	}
	if len(observer.evaluations) != 0 {
		t.Fatalf("filesystem evaluations = %d, want 0", len(observer.evaluations))
	}
}

func TestSelectionLivenessWindowFromEnv(t *testing.T) {
	t.Parallel()

	getenv := func(value string) func(string) string {
		return func(key string) string {
			if key == "ESHU_REPO_SELECTION_LIVENESS_WINDOW" {
				return value
			}
			return ""
		}
	}
	cases := map[string]struct {
		raw  string
		want time.Duration
	}{
		"unset uses default":   {raw: "", want: 48 * time.Hour},
		"invalid uses default": {raw: "two days", want: 48 * time.Hour},
		"zero uses default":    {raw: "0", want: 48 * time.Hour},
		"below minimum clamps": {raw: "30m", want: time.Hour},
		"minimum stays":        {raw: "1h", want: time.Hour},
		"larger parses":        {raw: "72h", want: 72 * time.Hour},
	}
	for name, tc := range cases {
		if got := selectionLivenessWindowFromEnv(getenv(tc.raw)); got != tc.want {
			t.Fatalf("%s: window = %v, want %v", name, got, tc.want)
		}
	}
}
