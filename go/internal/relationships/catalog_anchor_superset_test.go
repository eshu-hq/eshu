// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// payloadMatchesAnchorsSim mirrors the SQL predicate
// lower(payload::text) LIKE ANY('%anchor%') the scoped fact load uses, by
// marshaling the envelope payload to JSON (the same shape Postgres stores in the
// payload jsonb column), lowercasing it, and testing each anchor as a substring.
// It lets the pure-Go superset and equality tests reproduce exactly which facts
// the database would return for a given anchor set without a live Postgres.
func payloadMatchesAnchorsSim(t *testing.T, envelope facts.Envelope, anchors []string) bool {
	t.Helper()
	raw, err := json.Marshal(envelope.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	text := strings.ToLower(string(raw))
	for _, anchor := range anchors {
		anchor = strings.ToLower(strings.TrimSpace(anchor))
		if anchor == "" {
			continue
		}
		if strings.Contains(text, anchor) {
			return true
		}
	}
	return false
}

// argoCDOverSelectMarkersSim mirrors argoCDOverSelectAnchors in the postgres
// package: the lowercase payload markers that force ArgoCD-shaped facts into the
// scoped load unconditionally. Kept in sync by the superset test, which asserts
// the ArgoCD fact is selected via one of these markers rather than an alias.
var argoCDOverSelectMarkersSim = []string{
	"kind: application",
	"kind: applicationset",
	"argocd_applications",
	"argocd_applicationsets",
	`"artifact_type":"argocd"`,
	`"artifact_type": "argocd"`,
}

// supersetFixture is one extractor's matching content/file/gcp fact plus the
// catalog entry it should match, used to prove the anchor predicate selects it.
type supersetFixture struct {
	name        string
	envelope    facts.Envelope
	catalog     CatalogEntry
	argoCDOnly  bool // selected by ArgoCD marker, not by an alias anchor
	wantsModule bool // private terraform registry: anchor is the provider suffix
}

// allExtractorSupersetFixtures returns one matching fact per supported extractor
// family. Alias names are generic to keep the public repo free of proprietary
// identifiers.
func allExtractorSupersetFixtures() []supersetFixture {
	return []supersetFixture{
		{
			name: "terraform_content",
			envelope: facts.Envelope{
				ScopeID: "repo:infra",
				Payload: map[string]any{
					"artifact_type": "terraform",
					"relative_path": "main.tf",
					"content":       `app_repo = "payments-service"`,
				},
			},
			catalog: CatalogEntry{RepoID: "repo:payments", Aliases: []string{"payments-service"}},
		},
		{
			name: "terraform_private_registry_module",
			envelope: facts.Envelope{
				ScopeID: "repo:infra",
				Payload: map[string]any{
					"artifact_type": "terraform",
					"relative_path": "modules/net.tf",
					"content":       `module "net" { source = "registry.example.com/org/network/aws" }`,
				},
			},
			catalog:     CatalogEntry{RepoID: "repo:tf-aws", Aliases: []string{"terraform-modules-aws"}},
			wantsModule: true,
		},
		{
			name: "terragrunt",
			envelope: facts.Envelope{
				ScopeID: "repo:infra",
				Payload: map[string]any{
					"relative_path": "terragrunt.hcl",
					"parsed_file_data": map[string]any{
						"terragrunt_config_paths": "../../modules/orders-service",
					},
				},
			},
			catalog: CatalogEntry{RepoID: "repo:orders", Aliases: []string{"orders-service"}},
		},
		{
			name: "helm_values",
			envelope: facts.Envelope{
				ScopeID: "repo:charts",
				Payload: map[string]any{
					"artifact_type": "helm",
					"relative_path": "values.yaml",
					"content":       "image:\n  repository: billing-service\n",
				},
			},
			catalog: CatalogEntry{RepoID: "repo:billing", Aliases: []string{"billing-service"}},
		},
		{
			name: "kustomize",
			envelope: facts.Envelope{
				ScopeID: "repo:overlays",
				Payload: map[string]any{
					"relative_path": "overlays/prod/kustomization.yaml",
					"content":       "resources:\n  - shipping-service\n",
				},
			},
			catalog: CatalogEntry{RepoID: "repo:shipping", Aliases: []string{"shipping-service"}},
		},
		{
			name: "jenkins",
			envelope: facts.Envelope{
				ScopeID: "repo:ci",
				Payload: map[string]any{
					"relative_path": "Jenkinsfile",
					"content":       `library identifier: 'pipeline-library@main'`,
				},
			},
			catalog: CatalogEntry{RepoID: "repo:pipeline-lib", Aliases: []string{"pipeline-library"}},
		},
		{
			name: "dockerfile",
			envelope: facts.Envelope{
				ScopeID: "repo:app",
				Payload: map[string]any{
					"artifact_type": "dockerfile",
					"relative_path": "Dockerfile",
					"parsed_file_data": map[string]any{
						"dockerfile_labels": []any{
							map[string]any{"name": "org.source", "value": "base-image-service"},
						},
					},
				},
			},
			catalog: CatalogEntry{RepoID: "repo:base-image", Aliases: []string{"base-image-service"}},
		},
		{
			name: "docker_compose",
			envelope: facts.Envelope{
				ScopeID: "repo:app",
				Payload: map[string]any{
					"artifact_type": "docker_compose",
					"relative_path": "docker-compose.yaml",
					"content":       "services:\n  web:\n    image: frontend-service\n",
				},
			},
			catalog: CatalogEntry{RepoID: "repo:frontend", Aliases: []string{"frontend-service"}},
		},
		{
			name: "github_actions",
			envelope: facts.Envelope{
				ScopeID: "repo:app",
				Payload: map[string]any{
					"artifact_type": "github_actions_workflow",
					"relative_path": ".github/workflows/ci.yaml",
					"content":       "jobs:\n  build:\n    uses: example/deploy-toolkit/.github/workflows/deploy.yaml@main\n",
				},
			},
			catalog: CatalogEntry{RepoID: "repo:deploy-toolkit", Aliases: []string{"deploy-toolkit"}},
		},
		{
			name: "ansible",
			envelope: facts.Envelope{
				ScopeID: "repo:infra",
				Payload: map[string]any{
					"artifact_type": "ansible",
					"relative_path": "playbook.yaml",
					"content":       "- hosts: all\n  roles:\n    - config-role\n",
				},
			},
			catalog: CatalogEntry{RepoID: "repo:config-role", Aliases: []string{"config-role"}},
		},
		{
			name: "gcp_cloud_relationship",
			envelope: facts.Envelope{
				ScopeID:  "gcp:project:demo",
				FactKind: facts.GCPCloudRelationshipFactKind,
				Payload: map[string]any{
					"source_full_resource_name": "//run.googleapis.com/projects/demo/services/order-gateway",
					"target_full_resource_name": "//secretmanager.googleapis.com/projects/demo/secrets/payments-service",
					"relationship_type":         "run_service_uses_secret",
					"support_state":             "supported",
				},
			},
			catalog: CatalogEntry{RepoID: "repo:payments", Aliases: []string{"payments-service"}},
		},
		{
			name: "argocd_applicationset",
			envelope: facts.Envelope{
				ScopeID: "repo:control-plane",
				Payload: map[string]any{
					"artifact_type": "argocd",
					"relative_path": "appset.yaml",
					"content": "kind: ApplicationSet\n" +
						"spec:\n" +
						"  generators:\n" +
						"    - git:\n" +
						"        repoURL: config-repo\n" +
						"        files:\n" +
						"          - path: apps/*/config.yaml\n" +
						"  template:\n" +
						"    spec:\n" +
						"      source:\n" +
						"        repoURL: '{{.path.basenameNormalized}}'\n",
				},
			},
			catalog:    CatalogEntry{RepoID: "repo:config", Aliases: []string{"config-repo"}},
			argoCDOnly: true,
		},
	}
}

// TestCatalogPayloadAnchorsSelectsEveryExtractorFamily proves the anchor
// predicate is a superset: for every extractor family, the matching fact is
// selected by the anchors derived from the scoped catalog (alias-derived for most
// families, the Terraform provider suffix for private-registry modules, and the
// ArgoCD marker for ApplicationSet template synthesis).
func TestCatalogPayloadAnchorsSelectsEveryExtractorFamily(t *testing.T) {
	t.Parallel()

	for _, fixture := range allExtractorSupersetFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()

			anchors := CatalogPayloadAnchors([]CatalogEntry{fixture.catalog})
			combined := append(append([]string(nil), anchors...), argoCDOverSelectMarkersSim...)

			if !payloadMatchesAnchorsSim(t, fixture.envelope, combined) {
				t.Fatalf("fixture %q not selected by anchors %v", fixture.name, combined)
			}

			// Confirm the selection mechanism matches the documented contract.
			aliasSelected := payloadMatchesAnchorsSim(t, fixture.envelope, anchors)
			markerSelected := payloadMatchesAnchorsSim(t, fixture.envelope, argoCDOverSelectMarkersSim)
			switch {
			case fixture.argoCDOnly:
				if !markerSelected {
					t.Fatalf("argoCD fixture %q must be selected by an ArgoCD marker", fixture.name)
				}
			default:
				if !aliasSelected {
					t.Fatalf("fixture %q must be selected by an alias-derived anchor", fixture.name)
				}
			}
		})
	}
}

// TestScopedFactLoadEqualsFullLoadForScopedCatalog is the central correctness
// gate: discovering evidence over the anchor-scoped fact load against the scoped
// catalog must produce exactly the same evidence as discovering over the full
// fact corpus against the same scoped catalog. A mixed corpus of matching and
// non-matching facts is used so the scoped load genuinely drops facts, yet no
// evidence is lost.
func TestScopedFactLoadEqualsFullLoadForScopedCatalog(t *testing.T) {
	t.Parallel()

	fixtures := allExtractorSupersetFixtures()

	var fullCorpus []facts.Envelope
	var scopedCatalog []CatalogEntry
	for _, fixture := range fixtures {
		fullCorpus = append(fullCorpus, fixture.envelope)
		scopedCatalog = append(scopedCatalog, fixture.catalog)
	}

	// Add noise facts that reference repositories NOT in the scoped catalog, so
	// the anchor predicate has something real to exclude.
	noise := []facts.Envelope{
		{
			ScopeID: "repo:noise-a",
			Payload: map[string]any{
				"artifact_type": "terraform",
				"relative_path": "main.tf",
				"content":       `app_repo = "unrelated-service-one"`,
			},
		},
		{
			ScopeID: "repo:noise-b",
			Payload: map[string]any{
				"artifact_type": "helm",
				"relative_path": "values.yaml",
				"content":       "image:\n  repository: unrelated-service-two\n",
			},
		},
	}
	fullCorpus = append(fullCorpus, noise...)

	anchors := backfillRelationshipAnchorTermsSim(scopedCatalog)

	var scopedLoad []facts.Envelope
	for _, envelope := range fullCorpus {
		if payloadMatchesAnchorsSim(t, envelope, anchors) {
			scopedLoad = append(scopedLoad, envelope)
		}
	}

	// The scoped load must genuinely be smaller than the full corpus, or the test
	// is not exercising exclusion.
	if len(scopedLoad) >= len(fullCorpus) {
		t.Fatalf("scoped load (%d) did not exclude any of the full corpus (%d)", len(scopedLoad), len(fullCorpus))
	}

	fullEvidence := DedupeEvidenceFacts(DiscoverEvidence(fullCorpus, scopedCatalog))
	scopedEvidence := DedupeEvidenceFacts(DiscoverEvidence(scopedLoad, scopedCatalog))

	if !reflect.DeepEqual(canonicalEvidence(fullEvidence), canonicalEvidence(scopedEvidence)) {
		t.Fatalf("scoped evidence != full evidence\nfull:   %v\nscoped: %v",
			canonicalEvidence(fullEvidence), canonicalEvidence(scopedEvidence))
	}
	if len(fullEvidence) == 0 {
		t.Fatal("expected non-empty evidence from the mixed corpus")
	}
}

// TestCorpusWideAnchorScopedLoadEqualsFullLoad is the issue #3569 accuracy gate:
// the corpus-wide deferred backfill scopes its source-fact DB load to the
// content-anchor predicate derived from the FULL catalog (every repository is an
// eligible target), then discovers evidence over the loaded facts against that
// same full catalog. The result must equal discovering over the entire fact
// corpus against the full catalog. The fixture corpus mixes every extractor
// family's matching fact with noise facts that reference no catalog repo, so the
// anchor predicate genuinely drops facts while preserving all evidence.
func TestCorpusWideAnchorScopedLoadEqualsFullLoad(t *testing.T) {
	t.Parallel()

	fixtures := allExtractorSupersetFixtures()

	var fullCorpus []facts.Envelope
	var fullCatalog []CatalogEntry
	for _, fixture := range fixtures {
		fullCorpus = append(fullCorpus, fixture.envelope)
		fullCatalog = append(fullCatalog, fixture.catalog)
	}

	// Noise facts reference repositories absent from the catalog, so the anchor
	// predicate has facts to legitimately exclude.
	noise := []facts.Envelope{
		{
			ScopeID: "repo:noise-a",
			Payload: map[string]any{
				"artifact_type": "terraform",
				"relative_path": "main.tf",
				"content":       `app_repo = "absent-service-alpha"`,
			},
		},
		{
			ScopeID: "repo:noise-b",
			Payload: map[string]any{
				"artifact_type": "helm",
				"relative_path": "values.yaml",
				"content":       "image:\n  repository: absent-service-beta\n",
			},
		},
	}
	fullCorpus = append(fullCorpus, noise...)

	// Anchors are derived from the WHOLE catalog, mirroring the deferred backfill
	// (no new-repo scoping: every repository is an eligible target).
	anchors := backfillRelationshipAnchorTermsSim(fullCatalog)

	var scopedLoad []facts.Envelope
	for _, envelope := range fullCorpus {
		if payloadMatchesAnchorsSim(t, envelope, anchors) {
			scopedLoad = append(scopedLoad, envelope)
		}
	}

	if len(scopedLoad) >= len(fullCorpus) {
		t.Fatalf("corpus-wide scoped load (%d) did not exclude any noise from the full corpus (%d)", len(scopedLoad), len(fullCorpus))
	}

	fullEvidence := DedupeEvidenceFacts(DiscoverEvidence(fullCorpus, fullCatalog))
	scopedEvidence := DedupeEvidenceFacts(DiscoverEvidence(scopedLoad, fullCatalog))

	if !reflect.DeepEqual(canonicalEvidence(fullEvidence), canonicalEvidence(scopedEvidence)) {
		t.Fatalf("corpus-wide scoped evidence != full evidence\nfull:   %v\nscoped: %v",
			canonicalEvidence(fullEvidence), canonicalEvidence(scopedEvidence))
	}
	if len(fullEvidence) == 0 {
		t.Fatal("expected non-empty evidence from the mixed corpus")
	}
}

// backfillRelationshipAnchorTermsSim mirrors the postgres-package
// backfillRelationshipAnchorTerms: alias-derived anchors plus the ArgoCD markers.
func backfillRelationshipAnchorTermsSim(catalog []CatalogEntry) []string {
	anchors := CatalogPayloadAnchors(catalog)
	if len(anchors) == 0 {
		return nil
	}
	return append(append([]string(nil), anchors...), argoCDOverSelectMarkersSim...)
}

// canonicalEvidence returns a stable, comparable projection of evidence facts
// (kind, source, target, entity) so order differences do not fail the equality
// assertion.
func canonicalEvidence(evidence []EvidenceFact) []string {
	keys := make([]string, 0, len(evidence))
	for _, fact := range evidence {
		keys = append(keys, strings.Join([]string{
			string(fact.EvidenceKind),
			fact.SourceRepoID,
			fact.TargetRepoID,
			fact.TargetEntityID,
		}, "|"))
	}
	sort.Strings(keys)
	return keys
}
