// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestJenkinsSiblingSeedB7FixtureAdmitsDeployableConfigWorkload guards the
// #8540 B-7 corpus seed: tests/fixtures/ecosystems/jenkins-ci-pipelines
// carries a deployable-config/Dockerfile whose unit key names the
// deployable-config deployable unit for the Jenkins repository. That workload
// pairs SAME_NAME with the deployable-config repository's own workload,
// giving the catalog SAME_NAME sibling read (CatalogWorkloadSiblingCypher)
// its corpus coverage so the catalogWorkloadEnrichments read exemption stays
// out of statement-builders.yaml. The pair name avoids deployable-source:
// that name carries legacy workload:deployable-source cloud anchors (see
// TestB7SeedAvoidsLegacyAnchoredWorkloadNames), which the #5385 handle
// ladder fails closed when ambiguous. This test is the fast, Docker-free
// proof that the fixture content parses, admits, and materializes exactly
// that workload plus its SAME_NAME edge write, mirroring the salt-formulas
// B-7 fixture test one package over.
func TestJenkinsSiblingSeedB7FixtureAdmitsDeployableConfigWorkload(t *testing.T) {
	t.Parallel()

	fixtureRoot := filepath.Join("..", "..", "..", "tests", "fixtures", "ecosystems",
		"jenkins-ci-pipelines")
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v", err)
	}

	parse := func(relativePath string) map[string]any {
		t.Helper()
		payload, err := engine.ParsePath(fixtureRoot, filepath.Join(fixtureRoot, relativePath), false, parser.Options{})
		if err != nil {
			t.Fatalf("ParsePath(%q) error = %v", relativePath, err)
		}
		serialized, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("json.Marshal(ParsePath(%q)) error = %v", relativePath, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(serialized, &decoded); err != nil {
			t.Fatalf("json.Unmarshal(ParsePath(%q)) error = %v", relativePath, err)
		}
		return decoded
	}

	// The seed Dockerfile must resolve through the production registry as a
	// Dockerfile: basename matching is what admits a nested Dockerfile path.
	definition, ok := parser.DefaultRegistry().LookupByPath(
		filepath.Join(fixtureRoot, "deployable-config", "Dockerfile"))
	if !ok {
		t.Fatal("no parser registered for the sibling-seed Dockerfile")
	}
	if definition.Language != "dockerfile" {
		t.Fatalf("sibling-seed Dockerfile language = %q, want %q", definition.Language, "dockerfile")
	}
	dockerPayload := parse(filepath.Join("deployable-config", "Dockerfile"))
	stages, _ := dockerPayload["dockerfile_stages"].([]any)
	if len(stages) == 0 {
		t.Fatalf("sibling-seed Dockerfile parsed zero stages: %v", dockerPayload)
	}

	now := time.Now().UTC()
	envelopes := []facts.Envelope{
		{
			FactID:   "sibling-seed-repo",
			FactKind: "repository",
			Payload: map[string]any{
				"graph_id": "repository:r_jenkinsseed",
				"name":     "jenkins-ci-pipelines",
			},
			ObservedAt: now,
		},
		goldenFixtureFileEnvelope(
			"sibling-seed-jenkinsfile",
			"repository:r_jenkinsseed",
			"Jenkinsfile",
			"groovy",
			parse("Jenkinsfile"),
			now,
		),
		goldenFixtureFileEnvelope(
			"sibling-seed-dockerfile",
			"repository:r_jenkinsseed",
			filepath.Join("deployable-config", "Dockerfile"),
			"dockerfile",
			dockerPayload,
			now,
		),
	}

	candidates, _ := ExtractWorkloadCandidates(envelopes)
	if len(candidates) != 1 {
		t.Fatalf("workload candidates = %d, want 1", len(candidates))
	}
	candidate := candidates[0]
	if candidate.Classification != "service" {
		t.Fatalf("candidate classification = %q, want %q", candidate.Classification, "service")
	}
	if candidate.Confidence != 0.88 {
		t.Fatalf("candidate confidence = %v, want 0.88", candidate.Confidence)
	}

	admitted, err := admittedCorrelatedWorkloadCandidates(deployableUnitIntent(), candidates)
	if err != nil {
		t.Fatalf("admittedCorrelatedWorkloadCandidates() error = %v", err)
	}
	if len(admitted) != 1 {
		t.Fatalf("admitted candidates = %d, want 1", len(admitted))
	}
	if admitted[0].WorkloadName != "deployable-config" {
		t.Fatalf("admitted workload name = %q, want %q", admitted[0].WorkloadName, "deployable-config")
	}

	projection := BuildProjectionRows(admitted, nil)
	if len(projection.WorkloadRows) != 1 {
		t.Fatalf("workload rows = %d, want 1", len(projection.WorkloadRows))
	}
	row := projection.WorkloadRows[0]
	if row.WorkloadID != "workload:repository:r_jenkinsseed:deployable-config" {
		t.Fatalf("workload id = %q, want %q", row.WorkloadID, "workload:repository:r_jenkinsseed:deployable-config")
	}
	if len(projection.InstanceRows) != 0 {
		t.Fatalf("instance rows = %d, want 0 (the seed carries no environment evidence)", len(projection.InstanceRows))
	}

	executor := &fakeNeo4jExecutor{}
	materializer := NewWorkloadMaterializer(executor)
	materializer.SiblingLookup = &fakeSiblingLookup{siblings: []WorkloadSibling{
		{
			ID:     "workload:repository:r_srcrepo:deployable-config",
			Name:   "deployable-config",
			RepoID: "repository:r_srcrepo",
		},
	}}
	result, err := materializer.Materialize(context.Background(), projection)
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if result.SameNameEdgesWritten != 1 {
		t.Fatalf("SameNameEdgesWritten = %d, want 1", result.SameNameEdgesWritten)
	}
	if !fakeCallContainsRow(executor.calls, batchSameNameEdgeUpsertCypher, map[string]any{
		"a":    "workload:repository:r_jenkinsseed:deployable-config",
		"b":    "workload:repository:r_srcrepo:deployable-config",
		"name": "deployable-config",
	}) {
		t.Fatal("SAME_NAME graph upsert missing the ordered jenkins/deployable-config pair")
	}
}

// TestB7SeedAvoidsLegacyAnchoredWorkloadNames guards the #8574 regression:
// the B-7 awscloud cassette anchors cloud resources to the legacy handle
// workload:deployable-source (testdata/cassettes/awscloud/supply-chain-demo.json),
// which the #5385 handle ladder resolves by name uniqueness and fails closed
// when ambiguous. Seeding a second deployable-source workload in another B-7
// repo zeroed every USES edge (rc-175, edge_count_USES), the value-flow cloud
// sink self-loop (sl-value-flow-cloud-sink), and 409'd trace-deployment-chain
// on both backends. No staged B-7 fixture outside the deployable-source repo
// itself may therefore derive the deployable-source deployable-unit key.
func TestB7SeedAvoidsLegacyAnchoredWorkloadNames(t *testing.T) {
	t.Parallel()

	staged := []string{"go_comprehensive", "php_comprehensive", "python_comprehensive", "sql_comprehensive", "terraform_comprehensive", "terraform_local_backend_demo", "terragrunt_comprehensive", "cloudformation_comprehensive", "kubernetes_comprehensive", "helm_argocd_platform", "lib-common", "orders-api", "format-kit", "storefront-web", "ledger-kit", "invoice-portal", "deployable-source", "deployable-config", "kustomize-deployable-overlay", "ansible-platform-playbooks", "ansible-shared-roles", "jenkins-ci-pipelines", "puppet-platform-modules", "chef-cookbooks", "salt-formulas", "helm-umbrella-chart", "helm-template-chart", "api-svc", "container-base-lineage", "container-ci-lineage", "ruby_rails_app", "dart_comprehensive", "swift_vapor_app", "github_actions_workflows", "supply-chain-demo-db"}
	for _, repo := range staged {
		root := filepath.Join("..", "..", "..", "tests", "fixtures", "ecosystems", repo)
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			base := filepath.Base(path)
			lowerBase := strings.ToLower(base)
			isDockerfile := strings.EqualFold(base, "Dockerfile") ||
				strings.HasSuffix(lowerBase, ".dockerfile") ||
				strings.HasPrefix(lowerBase, "dockerfile.")
			if !isDockerfile {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			if key := deployableUnitKeyFromPath(repo, filepath.ToSlash(rel)); key == "deployable-source" && repo != "deployable-source" {
				t.Errorf("fixture %s derives legacy-anchored unit key deployable-source; rename the seed (see #8574)", filepath.Join(repo, filepath.ToSlash(rel)))
			}
			return nil
		})
	}
}
