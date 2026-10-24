// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Extraction PREP mirror (#8854, in-tree path): the command-surface
// contracts the extracted collector must preserve — claim-capable instance
// selection, credential resolution from the environment only, the
// heartbeat-under-lease guard, the ci-cd-run claim-ID namespace, the
// dual-provider (github_actions + gitlab_ci) target split with scope-ID
// routing, and the exact emitted-kind census at schema 1.0.0 — proved
// against the current in-tree config path, including hostile inputs.
//
// No-Observability-Change: existing eshu_dp_ci_cd_run_* signals cover this
// binary; this test adds no metric, span, or pipeline stage.

func extractionPrepGetenv(extra map[string]string) func(string) string {
	base := map[string]string{
		envCollectorInstances: `[{
			"instance_id": "cicd-run-primary",
			"collector_kind": "ci_cd_run",
			"mode": "continuous",
			"enabled": true,
			"claims_enabled": true,
			"configuration": {
				"targets": [{
					"provider": "github_actions",
					"scope_id": "ci-cd:github-actions:example-org/example-repo",
					"repository": "example-org/example-repo",
					"token_env": "GITHUB_TOKEN",
					"allowed_repositories": ["example-org/example-repo"],
					"max_runs": 1,
					"max_jobs": 25,
					"max_artifacts": 25
				}, {
					"provider": "gitlab_ci",
					"scope_id": "gitlab-ci://gitlab.com/eshu-hq/demo",
					"repository": "eshu-hq/demo",
					"token_env": "GITLAB_TOKEN",
					"allowed_repositories": ["eshu-hq/demo"],
					"max_runs": 1,
					"max_jobs": 25
				}]
			}
		}]`,
		envCollectorInstanceID: "cicd-run-primary",
		"GITHUB_TOKEN":         "github-token-value",
		"GITLAB_TOKEN":         "gitlab-token-value",
	}
	for key, value := range extra {
		base[key] = value
	}
	return func(key string) string { return base[key] }
}

func TestExtractionPrepClaimBoundaryRequiresClaimCapableInstance(t *testing.T) {
	t.Parallel()

	config, err := loadClaimedRuntimeConfig(extractionPrepGetenv(nil))
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if config.Instance.CollectorKind != scope.CollectorCICDRun {
		t.Errorf("CollectorKind = %q, want %q", config.Instance.CollectorKind, scope.CollectorCICDRun)
	}
	if len(config.GitHubSource.Targets) != 1 || config.GitHubSource.Targets[0].Token != "github-token-value" {
		t.Fatalf("GitHubSource.Targets = %+v, want one target with env-resolved token", config.GitHubSource.Targets)
	}
	if len(config.GitLabSource.Targets) != 1 || config.GitLabSource.Targets[0].Token != "gitlab-token-value" {
		t.Fatalf("GitLabSource.Targets = %+v, want one target with env-resolved token", config.GitLabSource.Targets)
	}

	// A second enabled ci_cd_run instance without an explicit selector fails
	// closed instead of collecting under an ambiguous identity.
	single := extractionPrepGetenv(nil)(envCollectorInstances)
	twoInstances := `[` + single[1:len(single)-1] + `,{
		"instance_id": "cicd-run-secondary",
		"collector_kind": "ci_cd_run",
		"mode": "continuous",
		"enabled": true,
		"claims_enabled": true,
		"configuration": {"targets": []}
	}]`
	if _, err := loadClaimedRuntimeConfig(extractionPrepGetenv(map[string]string{
		envCollectorInstances:  twoInstances,
		envCollectorInstanceID: "",
	})); err == nil {
		t.Error("loadClaimedRuntimeConfig(two instances, no selector) = nil, want ambiguity rejection")
	}
}

func TestExtractionPrepCredentialNeverInline(t *testing.T) {
	t.Parallel()

	// A target naming a token_env the environment does not define fails
	// closed; the error names the env reference, never a credential value.
	_, err := loadClaimedRuntimeConfig(func(key string) string {
		if key == envCollectorInstances {
			return extractionPrepGetenv(nil)(envCollectorInstances)
		}
		return ""
	})
	if err == nil {
		t.Fatal("loadClaimedRuntimeConfig(missing tokens) = nil, want credential error")
	}
	if strings.Contains(err.Error(), "github-token-value") || strings.Contains(err.Error(), "gitlab-token-value") {
		t.Fatalf("loadClaimedRuntimeConfig() error = %q, leaks a credential value", err.Error())
	}
}

func TestExtractionPrepHeartbeatGuard(t *testing.T) {
	t.Parallel()

	if _, err := loadClaimedRuntimeConfig(extractionPrepGetenv(map[string]string{
		envClaimLeaseTTL:     "30s",
		envHeartbeatInterval: "30s",
	})); err == nil {
		t.Error("loadClaimedRuntimeConfig(heartbeat == lease) = nil, want heartbeat-under-lease rejection")
	}
	if _, err := loadClaimedRuntimeConfig(extractionPrepGetenv(map[string]string{
		envClaimLeaseTTL:     "30s",
		envHeartbeatInterval: "10s",
	})); err != nil {
		t.Errorf("loadClaimedRuntimeConfig(heartbeat < lease) error = %v, want nil", err)
	}
}

func TestExtractionPrepUnsupportedProviderRejected(t *testing.T) {
	t.Parallel()

	_, err := loadClaimedRuntimeConfig(func(key string) string {
		if key != envCollectorInstances {
			return ""
		}
		return `[{
			"instance_id": "cicd-run-primary",
			"collector_kind": "ci_cd_run",
			"mode": "continuous",
			"enabled": true,
			"claims_enabled": true,
			"configuration": {
				"targets": [{
					"provider": "bitbucket_pipelines",
					"scope_id": "ci-cd:bitbucket:example/repo",
					"repository": "example/repo",
					"token_env": "TOKEN",
					"max_runs": 1
				}]
			}
		}]`
	})
	if err == nil {
		t.Fatal("loadClaimedRuntimeConfig(bitbucket) = nil, want unsupported provider rejection")
	}
	if !strings.Contains(err.Error(), "unsupported ci/cd run provider") {
		t.Fatalf("loadClaimedRuntimeConfig() error = %q, want unsupported ci/cd run provider", err.Error())
	}
}

func TestExtractionPrepProviderRoutingRejectsUnknownScope(t *testing.T) {
	t.Parallel()

	if _, err := newProviderRoutedSource(nil); err == nil {
		t.Fatal("newProviderRoutedSource(nil) = nil, want non-nil")
	}
	routed, err := newProviderRoutedSource(map[string]collector.ClaimedSource{
		"ci-cd:github-actions:example/repo": nilSource{},
	})
	if err != nil {
		t.Fatalf("newProviderRoutedSource() error = %v, want nil", err)
	}
	if _, _, err := routed.NextClaimed(context.Background(), workflow.WorkItem{
		ScopeID: "gitlab-ci://gitlab.com/eshu-hq/unknown",
	}); err == nil {
		t.Error("NextClaimed(unregistered scope) = nil, want no-registered-source error")
	} else if !strings.Contains(err.Error(), "has no registered provider source") {
		t.Fatalf("NextClaimed() error = %q, want no-registered-source", err.Error())
	}
}

func TestExtractionPrepClaimIDNamespace(t *testing.T) {
	t.Parallel()

	first, second := newClaimID(), newClaimID()
	for _, id := range []string{first, second} {
		if !strings.HasPrefix(id, "ci-cd-run-claim-") {
			t.Errorf("claim ID %q missing ci-cd-run-claim- namespace prefix", id)
		}
	}
	if first == second {
		t.Error("consecutive claim IDs identical; claim identity must be unique per acquisition")
	}
}

// parityFixture is the checked-in census agreement for #8854: the exact
// configuration the in-tree path must accept plus the exact kind census it
// must emit, so the #4047 gate can compare in-tree and external outputs on
// identical inputs.
type parityFixture struct {
	Instances []json.RawMessage `json:"instances"`
	Expected  struct {
		CollectorKind string   `json:"collector_kind"`
		ClaimPrefix   string   `json:"claim_id_prefix"`
		GitHubScopes  []string `json:"github_scopes"`
		GitLabScopes  []string `json:"gitlab_scopes"`
		EmittedKinds  []string `json:"emitted_kinds"`
		ExcludedKinds []string `json:"excluded_kinds"`
		Schema        string   `json:"schema_version"`
	} `json:"expected"`
	Tokens map[string]string `json:"tokens"`
}

func loadParityFixture(t *testing.T) parityFixture {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "8854-parity-cicd-run.json"))
	if err != nil {
		t.Fatalf("ReadFile(parity fixture) error = %v, want checked-in census fixture", err)
	}
	var fixture parityFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("Unmarshal(parity fixture) error = %v, want valid JSON", err)
	}
	return fixture
}

func TestExtractionPrepParityFixtureAgreement(t *testing.T) {
	t.Parallel()

	fixture := loadParityFixture(t)
	if len(fixture.Instances) != 1 {
		t.Fatalf("len(instances) = %d, want 1 dual-provider instance", len(fixture.Instances))
	}
	instancesJSON, err := json.Marshal(fixture.Instances)
	if err != nil {
		t.Fatalf("Marshal(instances) error = %v", err)
	}
	getenv := func(key string) string {
		switch key {
		case envCollectorInstances:
			return string(instancesJSON)
		case envCollectorInstanceID:
			return "cicd-run-primary"
		default:
			return fixture.Tokens[key]
		}
	}
	config, err := loadClaimedRuntimeConfig(getenv)
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig(fixture) error = %v, want nil", err)
	}
	if string(config.Instance.CollectorKind) != fixture.Expected.CollectorKind {
		t.Errorf("CollectorKind = %q, want %q", config.Instance.CollectorKind, fixture.Expected.CollectorKind)
	}
	githubScopes := map[string]bool{}
	for _, target := range config.GitHubSource.Targets {
		githubScopes[target.ScopeID] = true
	}
	for _, want := range fixture.Expected.GitHubScopes {
		if !githubScopes[want] {
			t.Errorf("GitHub scopes missing %q (have %+v)", want, config.GitHubSource.Targets)
		}
	}
	gitlabScopes := map[string]bool{}
	for _, target := range config.GitLabSource.Targets {
		gitlabScopes[target.ScopeID] = true
	}
	for _, want := range fixture.Expected.GitLabScopes {
		if !gitlabScopes[want] {
			t.Errorf("GitLab scopes missing %q (have %+v)", want, config.GitLabSource.Targets)
		}
	}

	// The emitted-kind census must match the fixture exactly at schema
	// 1.0.0. Excluded kinds stay family-declared (so silently dropping
	// them from the family breaks this test) but are carved out of this
	// collector's emission contract: ci.workflow_image_evidence is
	// emitted by go/internal/collector/repo/git/workflow/image/facts.go,
	// never by the cicd-run path. The count check below locks the
	// partition: no kind may slip in or out without a fixture update.
	family := map[string]bool{}
	for _, kind := range facts.CICDRunFactKinds() {
		family[kind] = true
		version, ok := facts.CICDRunSchemaVersion(kind)
		if !ok {
			t.Errorf("CICDRunSchemaVersion(%q) missing, want versioned kind", kind)
			continue
		}
		if version != fixture.Expected.Schema {
			t.Errorf("CICDRunSchemaVersion(%q) = %q, want %q", kind, version, fixture.Expected.Schema)
		}
	}
	for _, want := range fixture.Expected.EmittedKinds {
		if !family[want] {
			t.Errorf("emitted kind %q missing from ci_cd_run family declaration", want)
		}
	}
	for _, excluded := range fixture.Expected.ExcludedKinds {
		if !family[excluded] {
			t.Errorf("excluded kind %q missing from ci_cd_run family declaration; update the fixture deliberately, do not drop silently", excluded)
		}
	}
	if got, want := len(family), len(fixture.Expected.EmittedKinds)+len(fixture.Expected.ExcludedKinds); got != want {
		t.Errorf("family kind count = %d, want %d (emitted %d + excluded %d); update the fixture deliberately",
			got, want, len(fixture.Expected.EmittedKinds), len(fixture.Expected.ExcludedKinds))
	}
}

// nilSource is a non-nil ClaimedSource stand-in so the routing test proves
// scope dispatch, not provider fetch behavior.
type nilSource struct{}

func (nilSource) NextClaimed(context.Context, workflow.WorkItem) (collector.CollectedGeneration, bool, error) {
	return collector.CollectedGeneration{}, false, nil
}
