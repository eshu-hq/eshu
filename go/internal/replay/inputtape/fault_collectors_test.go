// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape_test

// Collector fault-injection coverage (C-14, #4367, epic #4172).
//
// The replay-coverage gate requires a `fault` depth scenario for every
// implemented collector boundary. These tests satisfy that requirement
// honestly: each case drives a REAL collector's poll with the inputtape timeout
// fault injected at its HTTP boundary, then asserts the collector surfaces the
// fault as a classified timeout rather than swallowing it, mis-classifying it,
// or hanging.
//
// The fault value injected is inputtape.ErrFaultTimeout — the exact error the
// Replayer serves for a FaultKindTimeout interaction (a *timeoutError that both
// wraps context.DeadlineExceeded and reports Timeout() bool == true). Injecting
// it through a RoundTripper reproduces, at the collector boundary, precisely
// what a recorded timeout-fault tape would deliver, without depending on a
// collector's request shape or volatile query params (which defeat tape
// request-key matching). The Replayer's own fault-injection mechanics are
// proven separately by fault_test.go and fault_timeout_test.go; these tests
// assert the COLLECTOR's reaction to a boundary fault, which is the C-14 grain
// (fault -> every implemented collector boundary).
//
// The cases are table-driven off collectorFaultCases so the surface set is a
// single source of truth. TestCollectorFaultManifestBinding then binds that set
// to the replay-coverage manifest, so a deleted or renamed case (or a manifest
// row added without a case) fails under the go-test-race gate — closing the gap
// where an os.Stat-only ref check would leave the dashboard green after a
// specific collector's proof was removed.
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor,
// concurrency-deadlock-rigor.

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/grafana"
	"github.com/eshu-hq/eshu/go/internal/collector/jira"
	"github.com/eshu-hq/eshu/go/internal/collector/loki"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry/packageruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/pagerduty"
	"github.com/eshu-hq/eshu/go/internal/collector/prometheusmimir"
	"github.com/eshu-hq/eshu/go/internal/collector/sbomruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/collector/tempo"
	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/collector/vulnerabilityintelligence"
	"github.com/eshu-hq/eshu/go/internal/replay/inputtape"
	"gopkg.in/yaml.v3"
)

// timeoutFaultClient returns an *http.Client whose transport injects the
// inputtape timeout fault on every request, driving the real collector wired to
// it straight into a boundary timeout.
func timeoutFaultClient() *http.Client {
	return &http.Client{Transport: faultTransport{err: inputtape.ErrFaultTimeout}}
}

// faultTransport is an http.RoundTripper that fails every request with a fixed
// fault error, reproducing an injected inputtape fault at a collector's HTTP
// boundary without a recorded tape.
type faultTransport struct{ err error }

// RoundTrip injects the configured fault instead of performing the request. The
// net/http stack wraps the returned error in *url.Error, whose Timeout() and
// Unwrap() delegate to the fault error — the shape a live transport fault takes.
func (f faultTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, f.err
}

// faultS3ObjectClient injects the inputtape timeout fault at the terraform-state
// collector's S3 read boundary — the narrow dependency interface the collector
// reads remote state through — reproducing a transport timeout without a live
// S3 backend.
type faultS3ObjectClient struct{ err error }

// GetObject fails every read with the injected fault.
func (f faultS3ObjectClient) GetObject(context.Context, terraformstate.S3GetObjectInput) (terraformstate.S3GetObjectOutput, error) {
	return terraformstate.S3GetObjectOutput{}, f.err
}

// collectorFaultCase drives one real collector's poll into an injected boundary
// timeout. surface is the replay-coverage surface id it proves (the single
// source of truth the manifest binding checks).
type collectorFaultCase struct {
	surface string
	collect func() error
}

// collectorFaultCases is the authoritative list of collector boundaries proven
// against a timeout fault. Adding a collector here without a matching manifest
// row (or vice versa) fails TestCollectorFaultManifestBinding.
func collectorFaultCases() []collectorFaultCase {
	return []collectorFaultCase{
		{
			surface: "collector:grafana",
			collect: func() error {
				client, err := grafana.NewHTTPClient(grafana.HTTPClientConfig{
					BaseURL: "https://grafana.invalid",
					Token:   "grafana-token",
					Client:  timeoutFaultClient(),
				})
				if err != nil {
					return err
				}
				_, err = client.CollectObservedMetadata(context.Background(), grafana.TargetConfig{
					Provider:      grafana.ProviderGrafana,
					ScopeID:       "grafana:instance:prod",
					InstanceID:    "grafana-prod",
					BaseURL:       "https://grafana.invalid",
					Token:         "grafana-token",
					ResourceLimit: 50,
				})
				return err
			},
		},
		{
			surface: "collector:loki",
			collect: func() error {
				client, err := loki.NewHTTPClient(loki.HTTPClientConfig{
					BaseURL: "https://loki.invalid",
					Client:  timeoutFaultClient(),
				})
				if err != nil {
					return err
				}
				_, err = client.CollectObservedMetadata(context.Background(), loki.TargetConfig{
					ScopeID:       "loki:tenant:prod",
					InstanceID:    "loki-prod",
					BaseURL:       "https://loki.invalid",
					Token:         "loki-token",
					TenantID:      "tenant-prod",
					ResourceLimit: 50,
				})
				return err
			},
		},
		{
			surface: "collector:prometheus_mimir",
			collect: func() error {
				client, err := prometheusmimir.NewHTTPClient(prometheusmimir.HTTPClientConfig{
					BaseURL: "https://prometheus.invalid",
					Client:  timeoutFaultClient(),
				})
				if err != nil {
					return err
				}
				_, err = client.CollectObservedMetadata(context.Background(), prometheusmimir.TargetConfig{
					ScopeID:       "prom:cluster:prod",
					InstanceID:    "prom-prod",
					Provider:      prometheusmimir.ProviderPrometheus,
					BaseURL:       "https://prometheus.invalid",
					Token:         "prom-token",
					ResourceLimit: 50,
				})
				return err
			},
		},
		{
			surface: "collector:tempo",
			collect: func() error {
				client, err := tempo.NewHTTPClient(tempo.HTTPClientConfig{
					BaseURL: "https://tempo.invalid",
					Client:  timeoutFaultClient(),
				})
				if err != nil {
					return err
				}
				_, err = client.CollectObservedMetadata(context.Background(), tempo.TargetConfig{
					ScopeID:       "tempo:cluster:prod",
					InstanceID:    "tempo-prod",
					BaseURL:       "https://tempo.invalid",
					ResourceLimit: 50,
				})
				return err
			},
		},
		{
			surface: "collector:jira",
			collect: func() error {
				client, err := jira.NewHTTPClient(jira.HTTPClientConfig{
					BaseURL: "https://jira.invalid",
					Email:   "collector@example.com",
					Token:   "jira-token",
					Client:  timeoutFaultClient(),
				})
				if err != nil {
					return err
				}
				_, err = client.CollectWorkItemEvidence(context.Background(), jira.TargetConfig{
					Provider:   "jira",
					ScopeID:    "jira:site:prod",
					SiteID:     "site-prod",
					BaseURL:    "https://jira.invalid",
					Email:      "collector@example.com",
					Token:      "jira-token",
					JQL:        "project = OPS",
					IssueLimit: 25,
				}, jira.CollectionWindow{Until: time.Now().UTC()})
				return err
			},
		},
		{
			surface: "collector:pagerduty",
			collect: func() error {
				client, err := pagerduty.NewHTTPClient(pagerduty.HTTPClientConfig{
					BaseURL: "https://pagerduty.invalid",
					Token:   "pagerduty-token",
					Client:  timeoutFaultClient(),
				})
				if err != nil {
					return err
				}
				_, err = client.CollectIncidentEvidence(context.Background(), pagerduty.TargetConfig{
					Provider:      "pagerduty",
					ScopeID:       "pagerduty:account:prod",
					AccountID:     "account-prod",
					Token:         "pagerduty-token",
					APIBaseURL:    "https://pagerduty.invalid",
					IncidentLimit: 25,
				}, pagerduty.CollectionWindow{Until: time.Now().UTC()})
				return err
			},
		},
		{
			surface: "collector:security_alert",
			collect: func() error {
				client := securityalerts.NewGitHubDependabotClient(securityalerts.GitHubDependabotClientConfig{
					BaseURL:              "https://api.github.invalid",
					Token:                "github-token",
					AllowedRepositories:  []string{"octo-org/checkout"},
					RepositoryAlertLimit: 25,
					HTTPClient:           timeoutFaultClient(),
				})
				_, err := client.ListRepositoryAlerts(context.Background(), "octo-org/checkout")
				return err
			},
		},
		{
			surface: "collector:vulnerability_intelligence",
			collect: func() error {
				client := vulnerabilityintelligence.NewOSVClient("https://osv.invalid", timeoutFaultClient())
				_, err := client.GetVulnerability(context.Background(), "GHSA-0000-0000-0000")
				return err
			},
		},
		{
			surface: "collector:oci_registry",
			collect: func() error {
				client, err := distribution.NewClient(distribution.ClientConfig{
					BaseURL:     "https://registry.invalid",
					BearerToken: "registry-token",
					Client:      timeoutFaultClient(),
				})
				if err != nil {
					return err
				}
				return client.Ping(context.Background())
			},
		},
		{
			surface: "collector:package_registry",
			collect: func() error {
				provider := packageruntime.HTTPMetadataProvider{Client: timeoutFaultClient()}
				_, err := provider.FetchMetadata(context.Background(), packageruntime.TargetConfig{
					MetadataURL: "https://packages.invalid/metadata.json",
				})
				return err
			},
		},
		{
			surface: "collector:sbom_attestation",
			collect: func() error {
				provider := sbomruntime.HTTPProvider{HTTPClient: timeoutFaultClient()}
				_, err := provider.FetchDocument(context.Background(), sbomruntime.TargetConfig{
					ScopeID:     "sbom:instance:prod",
					SourceType:  sbomruntime.SourceTypeConfigured,
					DocumentURL: "https://attestations.invalid/sbom.json",
				})
				return err
			},
		},
		{
			surface: "collector:terraform_state",
			collect: func() error {
				source, err := terraformstate.NewS3StateSource(terraformstate.S3SourceConfig{
					Bucket: "tfstate-prod",
					Key:    "env/prod/terraform.tfstate",
					Region: "us-east-1",
					Client: faultS3ObjectClient{err: inputtape.ErrFaultTimeout},
				})
				if err != nil {
					return err
				}
				_, _, err = source.Open(context.Background())
				return err
			},
		},
	}
}

// TestCollectorsSurfaceInjectedTimeout runs every collector fault case: the real
// collector is driven into an injected boundary timeout and must surface a
// classified timeout on both paths real SDKs gate retries on — the
// context.DeadlineExceeded sentinel and the net.Error Timeout() interface. A
// collector that returns nil (swallows the fault) or an unclassified error fails.
func TestCollectorsSurfaceInjectedTimeout(t *testing.T) {
	t.Parallel()

	for _, tc := range collectorFaultCases() {
		t.Run(tc.surface, func(t *testing.T) {
			t.Parallel()

			err := tc.collect()
			if err == nil {
				t.Fatalf("collector swallowed the injected timeout; want a surfaced error")
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("collector error not classified as context.DeadlineExceeded: %v", err)
			}
			var timeout interface{ Timeout() bool }
			if !errors.As(err, &timeout) || !timeout.Timeout() {
				t.Fatalf("collector error not reachable as a net timeout: %v", err)
			}
		})
	}
}

// TestCollectorFaultManifestBinding binds the collectorFaultCases surface set to
// the replay-coverage manifest's collector fault rows that point at this file.
// It fails if a case is deleted or renamed without dropping its manifest row (or
// a manifest row is added without a case), closing the os.Stat-only ref gap
// where the fault dashboard could stay 17/17 after a specific collector's proof
// was silently removed.
func TestCollectorFaultManifestBinding(t *testing.T) {
	t.Parallel()

	const manifestRel = "../../../../specs/replay-coverage-manifest.v1.yaml"
	const thisFileRef = "go/internal/replay/inputtape/fault_collectors_test.go"

	raw, err := os.ReadFile(filepath.Clean(manifestRel))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest struct {
		Coverage []struct {
			Surface      string `yaml:"surface"`
			ScenarioType string `yaml:"scenario_type"`
			Ref          string `yaml:"ref"`
		} `yaml:"coverage"`
	}
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	manifestSurfaces := map[string]struct{}{}
	for _, entry := range manifest.Coverage {
		if entry.ScenarioType != "fault" || !strings.HasPrefix(entry.Surface, "collector:") {
			continue
		}
		if entry.Ref != thisFileRef {
			continue
		}
		manifestSurfaces[entry.Surface] = struct{}{}
	}

	caseSurfaces := map[string]struct{}{}
	for _, tc := range collectorFaultCases() {
		if _, dup := caseSurfaces[tc.surface]; dup {
			t.Fatalf("duplicate collector fault case surface %q", tc.surface)
		}
		caseSurfaces[tc.surface] = struct{}{}
	}

	for surface := range manifestSurfaces {
		if _, ok := caseSurfaces[surface]; !ok {
			t.Errorf("manifest row %q (fault -> %s) has no collectorFaultCases entry; the dashboard would count a collector with no running test", surface, thisFileRef)
		}
	}
	for surface := range caseSurfaces {
		if _, ok := manifestSurfaces[surface]; !ok {
			t.Errorf("collectorFaultCases entry %q has no fault manifest row referencing %s", surface, thisFileRef)
		}
	}
}
