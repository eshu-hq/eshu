// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	metricnoop "go.opentelemetry.io/otel/metric/noop"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func testRedactionKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("gcp-runtime-fixture-key"))
	if err != nil {
		t.Fatalf("redact.NewKey: %v", err)
	}
	return key
}

func testMetrics(t *testing.T) *gcpcloud.Metrics {
	t.Helper()
	metrics, err := gcpcloud.NewMetrics(metricnoop.NewMeterProvider().Meter("test"))
	if err != nil {
		t.Fatalf("gcpcloud.NewMetrics: %v", err)
	}
	return metrics
}

func readFixturePage(t *testing.T, name string) gcpcloud.AssetsListPage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	page, err := gcpcloud.ParseAssetsListPage(raw)
	if err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return page
}

func testScope() ScopeConfig {
	return ScopeConfig{
		ParentScopeKind: gcpcloud.ParentScopeProject,
		ParentScopeID:   "my-project",
		AssetTypeFamily: "mixed",
		ContentFamily:   "resource",
		LocationBucket:  "global",
		FencingToken:    7,
		CredentialRef:   "gcp-readonly-sa",
	}
}

func testConfig(scopes ...ScopeConfig) Config {
	return Config{
		CollectorInstanceID: "gcp-instance-1",
		PollInterval:        time.Minute,
		Scopes:              scopes,
	}
}

func fixedClock() func() time.Time {
	at := time.Date(2026, 6, 9, 12, 0, 5, 0, time.UTC)
	return func() time.Time { return at }
}

func drainFacts(t *testing.T, collected collector.CollectedGeneration) []facts.Envelope {
	t.Helper()
	out := make([]facts.Envelope, 0, collected.FactCount)
	for env := range collected.Facts {
		out = append(out, env)
	}
	if collected.FactStreamErr != nil {
		if err := collected.FactStreamErr(); err != nil {
			t.Fatalf("fact stream error: %v", err)
		}
	}
	return out
}

func countKind(envs []facts.Envelope, kind string) int {
	count := 0
	for _, e := range envs {
		if e.FactKind == kind {
			count++
		}
	}
	return count
}

func firstEnvelopeKind(t *testing.T, envs []facts.Envelope, kind string) facts.Envelope {
	t.Helper()
	for _, env := range envs {
		if env.FactKind == kind {
			return env
		}
	}
	t.Fatalf("missing fact kind %s", kind)
	return facts.Envelope{}
}

func newSource(t *testing.T, cfg Config, provider PageProvider, tracker *gcpcloud.GenerationTracker) *Source {
	t.Helper()
	return &Source{
		Config:       cfg,
		Provider:     provider,
		RedactionKey: testRedactionKey(t),
		Tracker:      tracker,
		Metrics:      testMetrics(t),
		Clock:        fixedClock(),
	}
}

// TestSourceYieldsGenerationFromFixturePages proves a single scope drains both
// fixture pages into one CollectedGeneration with the expected fact counts and
// scope identity.
func TestSourceYieldsGenerationFromFixturePages(t *testing.T) {
	scopeCfg := testScope().withDefaults()
	provider := NewFixturePageProvider(map[string][]gcpcloud.AssetsListPage{
		scopeCfg.ScopeID: {
			readFixturePage(t, "assets_list_page1.json"),
			readFixturePage(t, "assets_list_page2.json"),
		},
	})
	src := newSource(t, testConfig(testScope()), provider, nil)

	collected, ok, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if !ok {
		t.Fatal("Next returned ok=false, want a generation")
	}
	if collected.Scope.ScopeID != scopeCfg.ScopeID {
		t.Fatalf("scope id = %q, want %q", collected.Scope.ScopeID, scopeCfg.ScopeID)
	}
	if collected.Scope.CollectorKind != scope.CollectorGCP {
		t.Fatalf("collector kind = %q, want %q", collected.Scope.CollectorKind, scope.CollectorGCP)
	}
	if err := collected.Generation.ValidateForScope(collected.Scope); err != nil {
		t.Fatalf("generation/scope identity invalid: %v", err)
	}
	envs := drainFacts(t, collected)
	if got := countKind(envs, facts.GCPCloudResourceFactKind); got != 3 {
		t.Fatalf("resource fact count = %d, want 3", got)
	}
	if got := countKind(envs, facts.GCPTagObservationFactKind); got != 2 {
		t.Fatalf("tag fact count = %d, want 2", got)
	}
	if got := countKind(envs, facts.GCPCollectionWarningFactKind); got != 0 {
		t.Fatalf("warning fact count = %d, want 0", got)
	}
	tag := firstEnvelopeKind(t, envs, facts.GCPTagObservationFactKind)
	readTime, ok := tag.Payload["read_time"].(time.Time)
	if !ok {
		t.Fatalf("tag read_time = %#v, want time.Time", tag.Payload["read_time"])
	}
	if want := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC); !readTime.Equal(want) {
		t.Fatalf("tag read_time = %s, want %s", readTime, want)
	}
}

// TestSourcePaginationResumesFromContinuationToken proves the source reads the
// second page only by resuming from the first page's continuation token, never
// by index, so a provider that enforces token matching still drains fully.
func TestSourcePaginationResumesFromContinuationToken(t *testing.T) {
	scopeCfg := testScope().withDefaults()
	provider := &recordingProvider{
		inner: NewFixturePageProvider(map[string][]gcpcloud.AssetsListPage{
			scopeCfg.ScopeID: {
				readFixturePage(t, "assets_list_page1.json"),
				readFixturePage(t, "assets_list_page2.json"),
			},
		}),
	}
	src := newSource(t, testConfig(testScope()), provider, nil)

	collected, ok, err := src.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("Next: ok=%v err=%v", ok, err)
	}
	envs := drainFacts(t, collected)
	if got := countKind(envs, facts.GCPCloudResourceFactKind); got != 3 {
		t.Fatalf("resource fact count = %d, want 3", got)
	}
	if len(provider.tokens) != 2 {
		t.Fatalf("page fetches = %d, want 2", len(provider.tokens))
	}
	if provider.tokens[0] != "" {
		t.Fatalf("first fetch token = %q, want empty", provider.tokens[0])
	}
	if provider.tokens[1] != "PAGE2TOKEN" {
		t.Fatalf("second fetch token = %q, want PAGE2TOKEN", provider.tokens[1])
	}
}

// TestSourceIdempotentReEmission proves re-collecting the same scope with the
// same generation id and fencing token yields the same stable fact keys.
func TestSourceIdempotentReEmission(t *testing.T) {
	scopeCfg := testScope()
	scopeCfg.GenerationID = "gen-fixed"
	resolved := scopeCfg.withDefaults()
	pages := map[string][]gcpcloud.AssetsListPage{
		resolved.ScopeID: {
			readFixturePage(t, "assets_list_page1.json"),
			readFixturePage(t, "assets_list_page2.json"),
		},
	}
	tracker := gcpcloud.NewGenerationTracker()

	build := func() []string {
		src := newSource(t, testConfig(scopeCfg), NewFixturePageProvider(pages), tracker)
		collected, ok, err := src.Next(context.Background())
		if err != nil || !ok {
			t.Fatalf("Next: ok=%v err=%v", ok, err)
		}
		envs := drainFacts(t, collected)
		keys := make([]string, 0, len(envs))
		for _, e := range envs {
			keys = append(keys, e.FactID)
		}
		sort.Strings(keys)
		return keys
	}

	first := build()
	second := build()
	if len(first) != 5 {
		t.Fatalf("expected 5 facts, got %d", len(first))
	}
	if !equalStrings(first, second) {
		t.Fatalf("re-emission not idempotent:\n%v\n%v", first, second)
	}
}

// TestSourceStaleGenerationEmitsWarning proves a generation rejected by a newer
// fencing token does not replace current facts and instead emits a single stale
// collection-warning fact.
func TestSourceStaleGenerationEmitsWarning(t *testing.T) {
	scopeCfg := testScope()
	resolved := scopeCfg.withDefaults()
	tracker := gcpcloud.NewGenerationTracker()
	// A newer generation already owns the scope at a higher fencing token.
	if err := tracker.Accept(resolved.ScopeID, "gen-newer", 9); err != nil {
		t.Fatalf("seed tracker: %v", err)
	}
	provider := NewFixturePageProvider(map[string][]gcpcloud.AssetsListPage{
		resolved.ScopeID: {readFixturePage(t, "assets_list_page1.json")},
	})
	src := newSource(t, testConfig(scopeCfg), provider, tracker)

	collected, ok, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if !ok {
		t.Fatal("stale scan should still yield a warning generation")
	}
	envs := drainFacts(t, collected)
	if got := countKind(envs, facts.GCPCloudResourceFactKind); got != 0 {
		t.Fatalf("stale scan emitted %d resource facts, want 0", got)
	}
	if got := countKind(envs, facts.GCPCollectionWarningFactKind); got != 1 {
		t.Fatalf("stale warning fact count = %d, want 1", got)
	}
	if envs[0].Payload["warning_kind"] != gcpcloud.WarningKindStale {
		t.Fatalf("warning_kind = %v, want %q", envs[0].Payload["warning_kind"], gcpcloud.WarningKindStale)
	}
}

// TestSourcePageTokenExpiredWarning proves a continuation token the provider
// cannot resume converts to a page_token_expired partial warning rather than a
// hard failure or silent truncation.
func TestSourcePageTokenExpiredWarning(t *testing.T) {
	scopeCfg := testScope()
	resolved := scopeCfg.withDefaults()
	// Only page 1 is available, but it advertises a continuation token the
	// provider cannot satisfy.
	provider := NewFixturePageProvider(map[string][]gcpcloud.AssetsListPage{
		resolved.ScopeID: {{
			ReadTime:      time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
			Resources:     readFixturePage(t, "assets_list_page1.json").Resources,
			NextPageToken: "DANGLING",
		}},
	})
	src := newSource(t, testConfig(scopeCfg), provider, nil)

	collected, ok, err := src.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("Next: ok=%v err=%v", ok, err)
	}
	envs := drainFacts(t, collected)
	if got := countKind(envs, facts.GCPCloudResourceFactKind); got != 2 {
		t.Fatalf("resource fact count = %d, want 2", got)
	}
	if got := countKind(envs, facts.GCPCollectionWarningFactKind); got != 1 {
		t.Fatalf("page-token-expired warning count = %d, want 1", got)
	}
}

// TestSourceProviderWarningEmitsCollectionWarning proves live provider warning
// classifications become durable coverage facts rather than hard failures or
// silent success.
func TestSourceProviderWarningEmitsCollectionWarning(t *testing.T) {
	src := newSource(t, testConfig(testScope()), warningProvider{
		warning: ProviderWarning{
			WarningKind: gcpcloud.WarningKindQuota,
			Outcome:     gcpcloud.OutcomeUnavailable,
			Reason:      "cloud asset inventory throttle exhausted",
			Retryable:   true,
		},
	}, nil)

	collected, ok, err := src.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("Next: ok=%v err=%v", ok, err)
	}
	envs := drainFacts(t, collected)
	if got := countKind(envs, facts.GCPCloudResourceFactKind); got != 0 {
		t.Fatalf("resource facts = %d, want 0", got)
	}
	warning := firstEnvelopeKind(t, envs, facts.GCPCollectionWarningFactKind)
	if warning.Payload["warning_kind"] != gcpcloud.WarningKindQuota {
		t.Fatalf("warning_kind = %v, want quota", warning.Payload["warning_kind"])
	}
	if warning.Payload["outcome"] != gcpcloud.OutcomeUnavailable {
		t.Fatalf("outcome = %v, want unavailable", warning.Payload["outcome"])
	}
	if warning.Payload["retryable"] != true {
		t.Fatalf("retryable = %v, want true", warning.Payload["retryable"])
	}
}

// TestSourceDrainsScopesThenReportsIdle proves the source yields one generation
// per scope, then returns ok=false to signal the batch is drained, then
// restarts the batch on the next poll.
func TestSourceDrainsScopesThenReportsIdle(t *testing.T) {
	scopeA := testScope()
	scopeA.ParentScopeID = "project-a"
	scopeB := testScope()
	scopeB.ParentScopeID = "project-b"
	resolvedA := scopeA.withDefaults()
	resolvedB := scopeB.withDefaults()
	provider := NewFixturePageProvider(map[string][]gcpcloud.AssetsListPage{
		resolvedA.ScopeID: {readFixturePage(t, "assets_list_page1.json")},
		resolvedB.ScopeID: {readFixturePage(t, "assets_list_page2.json")},
	})
	src := newSource(t, testConfig(scopeA, scopeB), provider, nil)

	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		collected, ok, err := src.Next(context.Background())
		if err != nil || !ok {
			t.Fatalf("Next %d: ok=%v err=%v", i, ok, err)
		}
		seen[collected.Scope.ScopeID] = true
		drainFacts(t, collected)
	}
	if !seen[resolvedA.ScopeID] || !seen[resolvedB.ScopeID] {
		t.Fatalf("did not see both scopes: %v", seen)
	}
	// Batch is drained.
	if _, ok, err := src.Next(context.Background()); ok || err != nil {
		t.Fatalf("expected idle drain, got ok=%v err=%v", ok, err)
	}
	// Next poll restarts the batch.
	if _, ok, err := src.Next(context.Background()); !ok || err != nil {
		t.Fatalf("expected batch restart, got ok=%v err=%v", ok, err)
	}
}

// TestSourceRejectsInvalidConfig proves missing required identity fails fast and
// never reaches the provider.
func TestSourceRejectsInvalidConfig(t *testing.T) {
	cases := map[string]Config{
		"no instance": {Scopes: []ScopeConfig{testScope()}},
		"no scopes":   {CollectorInstanceID: "gcp-instance-1"},
		"no credential": testConfig(ScopeConfig{
			ParentScopeKind: gcpcloud.ParentScopeProject,
			ParentScopeID:   "p",
			FencingToken:    1,
		}),
		"bad fencing token": testConfig(ScopeConfig{
			ParentScopeKind: gcpcloud.ParentScopeProject,
			ParentScopeID:   "p",
			CredentialRef:   "ref",
		}),
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			src := &Source{Config: cfg, Provider: failProvider{}, RedactionKey: testRedactionKey(t)}
			if _, _, err := src.Next(context.Background()); err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

// TestSourceRequiresRedactionKey proves the source refuses to emit facts with an
// unkeyed redaction marker.
func TestSourceRequiresRedactionKey(t *testing.T) {
	src := &Source{Config: testConfig(testScope()), Provider: failProvider{}}
	if _, _, err := src.Next(context.Background()); err == nil {
		t.Fatal("expected redaction-key error, got nil")
	}
}

// TestSourceContextCancellationStops proves a cancelled context aborts a scan
// before any provider call rather than draining silently.
func TestSourceContextCancellationStops(t *testing.T) {
	src := newSource(t, testConfig(testScope()), failProvider{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := src.Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

type recordingProvider struct {
	inner  PageProvider
	tokens []string
}

func (p *recordingProvider) FetchPage(ctx context.Context, req PageRequest) (gcpcloud.AssetsListPage, error) {
	p.tokens = append(p.tokens, req.PageToken)
	return p.inner.FetchPage(ctx, req)
}

type failProvider struct{}

func (failProvider) FetchPage(context.Context, PageRequest) (gcpcloud.AssetsListPage, error) {
	return gcpcloud.AssetsListPage{}, errors.New("provider must not be called")
}

type warningProvider struct {
	warning ProviderWarning
}

func (p warningProvider) FetchPage(context.Context, PageRequest) (gcpcloud.AssetsListPage, error) {
	return gcpcloud.AssetsListPage{}, p.warning
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
