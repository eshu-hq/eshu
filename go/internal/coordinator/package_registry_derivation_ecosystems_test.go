// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestPackageRegistryWorkPlannerDerivesPackageTargetsAcrossSupportedEcosystems(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "collector-package-registry",
		CollectorKind:  scope.CollectorPackageRegistry,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"derive_from_owned_packages":{"enabled":true,"ecosystems":["npm","pypi","go","maven","nuget","composer","rubygems","cargo"],"planning_mode":"single_pass","target_limit":20,"package_limit":1,"version_limit":3}}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := PackageRegistryWorkPlanner{}.PlanPackageRegistryWork(context.Background(), PackageRegistryPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-single-pass",
		OwnedPackageTargets: []workflow.OwnedPackageDependencyTarget{
			{Ecosystem: "npm", PackageName: "@Example/Web-App", Version: "1.2.3", RepositoryID: "repo-fixture"},
			{Ecosystem: "pypi", PackageName: "Friendly_Bard", Version: "2.0.0", RepositoryID: "repo-fixture"},
			{Ecosystem: "go", PackageName: "example.com/acme/lib/v2", Version: "v2.1.0", RepositoryID: "repo-fixture"},
			{Ecosystem: "maven", PackageName: "org.example:demo-core", Version: "3.0.0", RepositoryID: "repo-fixture"},
			{Ecosystem: "nuget", PackageName: "Newtonsoft.Json", Version: "13.0.3", RepositoryID: "repo-fixture"},
			{Ecosystem: "composer", PackageName: "Symfony/Console", Version: "7.0.0", RepositoryID: "repo-fixture"},
			{Ecosystem: "rubygems", PackageName: "Rails", Version: "7.1.0", RepositoryID: "repo-fixture"},
			{Ecosystem: "cargo", PackageName: "Serde_JSON", Version: "1.0.116", RepositoryID: "repo-fixture"},
		},
	})
	if err != nil {
		t.Fatalf("PlanPackageRegistryWork() error = %v", err)
	}

	gotScopes := make(map[string]bool, len(items))
	for _, item := range items {
		gotScopes[item.ScopeID] = true
	}
	for _, want := range []string{
		"npm://registry.npmjs.org/@example/web-app",
		"pypi://pypi.org/pypi/friendly-bard",
		"gomod://proxy.golang.org/example.com/acme/lib/v2",
		"maven://repo.maven.apache.org/maven2/org.example:demo-core",
		"nuget://api.nuget.org/v3/index.json/newtonsoft.json",
		"composer://repo.packagist.org/symfony/console",
		"rubygems://rubygems.org/rails",
		"cargo://crates.io/serde_json",
	} {
		if !gotScopes[want] {
			t.Fatalf("derived scopes = %#v, missing %q", gotScopes, want)
		}
	}

	var requested struct {
		Targets []struct {
			ScopeID string `json:"scope_id"`
			Derived bool   `json:"derived"`
			Class   string `json:"target_class"`
		} `json:"targets"`
	}
	if err := json.Unmarshal([]byte(run.RequestedScopeSet), &requested); err != nil {
		t.Fatalf("RequestedScopeSet JSON = %q: %v", run.RequestedScopeSet, err)
	}
	if got, want := len(requested.Targets), 8; got != want {
		t.Fatalf("len(RequestedScopeSet.targets) = %d, want %d", got, want)
	}
	for _, target := range requested.Targets {
		if !target.Derived || target.Class != packageRegistryTargetClassOwnedPackage {
			t.Fatalf("requested target = %#v, want derived owned-package target", target)
		}
	}
}
