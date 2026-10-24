// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const fanoutTestOrgScopeID = "security-alert:github-org:example-org"

// fanoutClient is a RepositoryAlertClient fake that returns one org result
// and per-repository results keyed by normalized full name, recording every
// per-repository call in order.
type fanoutClient struct {
	mu        sync.Mutex
	org       securityalerts.GitHubDependabotAlertResult
	orgErr    error
	repos     map[string]securityalerts.GitHubDependabotAlertResult
	repoErrs  map[string]error
	repoCalls []string
	repoPages []int
	orgCalls  int
	// onRepoCall, when set, runs at the start of every per-repository call
	// with the client lock released, so a test can observe state mid-fan-out.
	onRepoCall func()
}

func (c *fanoutClient) ListRepositoryAlertsPages(
	_ context.Context,
	repository string,
	maxPages int,
) (securityalerts.GitHubDependabotAlertResult, error) {
	if c.onRepoCall != nil {
		c.onRepoCall()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.repoCalls = append(c.repoCalls, repository)
	c.repoPages = append(c.repoPages, maxPages)
	if err := c.repoErrs[repository]; err != nil {
		return securityalerts.GitHubDependabotAlertResult{}, err
	}
	return c.repos[repository], nil
}

func (c *fanoutClient) ListOrganizationAlertsPages(
	context.Context,
	string,
	int,
) (securityalerts.GitHubDependabotAlertResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.orgCalls++
	return c.org, c.orgErr
}

// repoAlert builds a per-repository endpoint alert: the provider omits the
// repository object on that endpoint, so the fake leaves it empty too.
func repoAlert(number int) securityalerts.GitHubDependabotAlert {
	alert := testDependabotAlert()
	alert.Number = number
	return alert
}

func newFanoutSource(
	t *testing.T,
	client RepositoryAlertClient,
	allowed []string,
	maxPages int,
	instruments *telemetry.Instruments,
) *ClaimedSource {
	t.Helper()
	return newTracedFanoutSource(t, client, allowed, maxPages, instruments, nil)
}

// newTracedFanoutSource is newFanoutSource with an optional tracer so a test
// can read the spans a fan-out claim emits.
func newTracedFanoutSource(
	t *testing.T,
	client RepositoryAlertClient,
	allowed []string,
	maxPages int,
	instruments *telemetry.Instruments,
	tracer trace.Tracer,
) *ClaimedSource {
	t.Helper()
	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider:            ProviderGitHubDependabot,
			Scope:               TargetScopeOrganization,
			ScopeID:             fanoutTestOrgScopeID,
			Organization:        "example-org",
			Token:               "github-token",
			MaxPages:            maxPages,
			AllowedRepositories: allowed,
		}},
		ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) { return client, nil },
		Now:           func() time.Time { return now },
		Instruments:   instruments,
		Tracer:        tracer,
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	return source
}

func collectOrgFacts(t *testing.T, source *ClaimedSource) ([]facts.Envelope, string) {
	t.Helper()
	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	item := testSecurityAlertWorkItem(now)
	item.ScopeID = fanoutTestOrgScopeID
	item.AcceptanceUnitID = fanoutTestOrgScopeID
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got, want := collected.Generation.GenerationID, item.GenerationID; got != want {
		t.Fatalf("GenerationID = %q, want %q", got, want)
	}
	if got, want := collected.Generation.ScopeID, fanoutTestOrgScopeID; got != want {
		t.Fatalf("generation ScopeID = %q, want %q", got, want)
	}
	envs := drainFacts(collected.Facts)
	sort.Slice(envs, func(i, j int) bool {
		left, right := payloadString(envs[i].Payload, "repository_id"), payloadString(envs[j].Payload, "repository_id")
		if left != right {
			return left < right
		}
		return payloadInt(envs[i].Payload, "provider_alert_number") < payloadInt(envs[j].Payload, "provider_alert_number")
	})
	return envs, collected.Generation.FreshnessHint
}

func payloadInt(payload map[string]any, key string) int64 {
	switch value := payload[key].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	default:
		return 0
	}
}

func payloadStrings(payload map[string]any, key string) []string {
	values, _ := payload[key].([]string)
	return values
}

func newTestInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return instruments, reader
}

// counterValue sums the int64 counter named metricName across data points
// whose attributes contain every key/value in want.
func counterValue(t *testing.T, reader *sdkmetric.ManualReader, metricName string, want map[string]string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != metricName {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want Sum[int64]", metricName, m.Data)
			}
			for _, point := range sum.DataPoints {
				if attributesMatch(point.Attributes, want) {
					total += point.Value
				}
			}
		}
	}
	return total
}

func attributesMatch(set attribute.Set, want map[string]string) bool {
	for key, value := range want {
		got, ok := set.Value(attribute.Key(key))
		if !ok || got.AsString() != value {
			return false
		}
	}
	return true
}
