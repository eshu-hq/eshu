// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

func TestDeployableConfigGoldenFixtureProducesExactRepoLocalCorrelation(t *testing.T) {
	t.Parallel()

	const catalogPath = "catalog-info.yaml"
	fixtureRoot := filepath.Join("..", "..", "..", "tests", "fixtures", "ecosystems", "deployable-config")
	if _, err := os.Stat(filepath.Join(fixtureRoot, catalogPath)); err != nil {
		t.Fatalf("committed service-catalog fixture: %v", err)
	}
	repo, err := repositoryidentity.MetadataFor(
		"deployable-config",
		fixtureRoot,
		"https://github.com/acme/deployable-config",
	)
	if err != nil {
		t.Fatalf("repositoryidentity.MetadataFor() error = %v, want nil", err)
	}
	if got, want := repo.ID, "repository:r_217415d9"; got != want {
		t.Fatalf("fixture repository ID = %q, want %q", got, want)
	}

	observedAt := time.Date(2026, time.August, 8, 12, 30, 0, 0, time.UTC)
	collected := buildStreamingGeneration(fixtureRoot, repo, "golden-service-catalog-run", observedAt, RepositorySnapshot{
		FileCount: 1,
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: catalogPath,
			Digest:       "sha256:1668dcaa4b46f28eb0d488770b386e54e5341ca5b499c0e5dec782e66464b190",
			Language:     "yaml",
		}},
	}, false, "")
	envelopes := drainFactChannel(collected.Facts)

	entityFacts := goldenFactsByKind(envelopes, facts.ServiceCatalogEntityFactKind)
	ownershipFacts := goldenFactsByKind(envelopes, facts.ServiceCatalogOwnershipFactKind)
	repositoryLinkFacts := goldenFactsByKind(envelopes, facts.ServiceCatalogRepositoryLinkFactKind)
	repositoryFacts := goldenFactsByKind(envelopes, "repository")
	if got, want := len(entityFacts), 1; got != want {
		t.Fatalf("service catalog entity fact count = %d, want %d", got, want)
	}
	if got, want := len(ownershipFacts), 1; got != want {
		t.Fatalf("service catalog ownership fact count = %d, want %d", got, want)
	}
	if got := len(repositoryLinkFacts); got != 0 {
		t.Fatalf("service catalog repository-link fact count = %d, want 0 for repo-local admission", got)
	}
	if got, want := len(repositoryFacts), 1; got != want {
		t.Fatalf("repository fact count = %d, want %d", got, want)
	}
	if got := len(goldenFactsByKind(envelopes, facts.ServiceCatalogWarningFactKind)); got != 0 {
		t.Fatalf("service catalog warning fact count = %d, want 0", got)
	}

	entity := entityFacts[0]
	goldenAssertServiceCatalogEnvelope(t, entity, facts.ServiceCatalogEntityFactKind, catalogPath)
	for field, want := range map[string]string{
		"collector_instance_id": "git-service-catalog",
		"provider":              "backstage",
		"entity_ref":            "component:default/deployable-config",
		"entity_type":           "service",
		"display_name":          "Deployable Config",
		"lifecycle":             "production",
	} {
		if got := goldenPayloadString(entity.Payload, field); got != want {
			t.Errorf("entity payload[%q] = %q, want %q", field, got, want)
		}
	}
	for _, forbidden := range []string{"repository_id", "service_id", "workload_id"} {
		if _, ok := entity.Payload[forbidden]; ok {
			t.Errorf("entity payload unexpectedly contains %q: %#v", forbidden, entity.Payload)
		}
	}

	ownership := ownershipFacts[0]
	goldenAssertServiceCatalogEnvelope(t, ownership, facts.ServiceCatalogOwnershipFactKind, catalogPath)
	for field, want := range map[string]string{
		"collector_instance_id": "git-service-catalog",
		"provider":              "backstage",
		"entity_ref":            "component:default/deployable-config",
		"owner_ref":             "group:default/platform",
	} {
		if got := goldenPayloadString(ownership.Payload, field); got != want {
			t.Errorf("ownership payload[%q] = %q, want %q", field, got, want)
		}
	}

	decisions := reducer.BuildServiceCatalogCorrelationDecisions(envelopes)
	if got, want := len(decisions), 1; got != want {
		t.Fatalf("correlation decision count = %d, want %d", got, want)
	}
	decision := decisions[0]
	for field, values := range map[string][2]string{
		"provider":      {decision.Provider, "backstage"},
		"entity ref":    {decision.EntityRef, "component:default/deployable-config"},
		"entity type":   {decision.EntityType, "service"},
		"display name":  {decision.DisplayName, "Deployable Config"},
		"repository id": {decision.RepositoryID, "repository:r_217415d9"},
		"service id":    {decision.ServiceID, "component:default/deployable-config"},
		"owner ref":     {decision.OwnerRef, "group:default/platform"},
		"lifecycle":     {decision.Lifecycle, "production"},
		"drift kind":    {decision.DriftKind, "repository"},
		"drift status":  {decision.DriftStatus, "matches"},
		"reason":        {decision.Reason, "repo-local catalog descriptor scope matches canonical repository identity"},
	} {
		if got, want := values[0], values[1]; got != want {
			t.Errorf("decision %s = %q, want %q", field, got, want)
		}
	}
	if decision.Outcome != reducer.ServiceCatalogCorrelationExact {
		t.Errorf("decision Outcome = %q, want %q", decision.Outcome, reducer.ServiceCatalogCorrelationExact)
	}
	if decision.ProvenanceOnly {
		t.Error("decision ProvenanceOnly = true, want false after exact repository admission")
	}
	if decision.WorkloadID != "" {
		t.Errorf("decision WorkloadID = %q, want empty without workload proof", decision.WorkloadID)
	}
	if decision.Tier != "" {
		t.Errorf("decision Tier = %q, want empty because the fixture declares no tier", decision.Tier)
	}
	if got, want := len(decision.EvidenceFactIDs), 3; got != want {
		t.Errorf("decision EvidenceFactIDs count = %d, want %d", got, want)
	}
	goldenRequireEvidenceFactIDs(t, decision.EvidenceFactIDs, entity.FactID, ownership.FactID, repositoryFacts[0].FactID)

	withoutRepository := reducer.BuildServiceCatalogCorrelationDecisions(goldenFactsWithoutKind(envelopes, "repository"))
	if got, want := len(withoutRepository), 1; got != want {
		t.Fatalf("correlation decision count without repository evidence = %d, want %d", got, want)
	}
	unresolved := withoutRepository[0]
	if unresolved.Outcome != reducer.ServiceCatalogCorrelationUnresolved {
		t.Errorf("decision without repository evidence Outcome = %q, want %q", unresolved.Outcome, reducer.ServiceCatalogCorrelationUnresolved)
	}
	if !unresolved.ProvenanceOnly {
		t.Error("decision without repository evidence ProvenanceOnly = false, want true")
	}
	if unresolved.RepositoryID != "" || unresolved.ServiceID != "" || unresolved.WorkloadID != "" {
		t.Errorf(
			"decision without repository evidence identities = repository:%q service:%q workload:%q, want all empty",
			unresolved.RepositoryID,
			unresolved.ServiceID,
			unresolved.WorkloadID,
		)
	}
	if got, want := unresolved.Reason, "repo-local catalog descriptor scope did not match any active repository"; got != want {
		t.Errorf("decision without repository evidence Reason = %q, want %q", got, want)
	}
	if got, want := unresolved.DriftStatus, "missing"; got != want {
		t.Errorf("decision without repository evidence DriftStatus = %q, want %q", got, want)
	}
	if !goldenContainsString(unresolved.RequiredAnchorKeys, "git-repository-scope:<repo_id>") {
		t.Errorf("decision without repository evidence RequiredAnchorKeys = %#v, want repo-local scope anchor", unresolved.RequiredAnchorKeys)
	}
}

func goldenFactsByKind(envelopes []facts.Envelope, kind string) []facts.Envelope {
	out := make([]facts.Envelope, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			out = append(out, envelope)
		}
	}
	return out
}

func goldenFactsWithoutKind(envelopes []facts.Envelope, kind string) []facts.Envelope {
	out := make([]facts.Envelope, 0, len(envelopes))
	for _, envelope := range envelopes {
		if envelope.FactKind != kind {
			out = append(out, envelope)
		}
	}
	return out
}

func goldenPayloadString(payload map[string]any, field string) string {
	value, _ := payload[field].(string)
	return value
}

func goldenAssertServiceCatalogEnvelope(t *testing.T, envelope facts.Envelope, factKind, sourceURI string) {
	t.Helper()

	for field, values := range map[string][2]string{
		"fact kind":      {envelope.FactKind, factKind},
		"schema version": {envelope.SchemaVersion, facts.ServiceCatalogSchemaVersionV1},
		"collector kind": {envelope.CollectorKind, "service_catalog"},
		"scope id":       {envelope.ScopeID, "git-repository-scope:repository:r_217415d9"},
		"source system":  {envelope.SourceRef.SourceSystem, "service_catalog"},
		"source uri":     {envelope.SourceRef.SourceURI, sourceURI},
	} {
		if got, want := values[0], values[1]; got != want {
			t.Errorf("envelope %s = %q, want %q", field, got, want)
		}
	}
	if envelope.FactID == "" || envelope.StableFactKey == "" {
		t.Errorf("envelope identifiers = fact:%q stable:%q, want both non-empty", envelope.FactID, envelope.StableFactKey)
	}
}

func goldenContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func goldenRequireEvidenceFactIDs(t *testing.T, got []string, want ...string) {
	t.Helper()

	seen := make(map[string]bool, len(got))
	for _, factID := range got {
		seen[factID] = true
	}
	for _, factID := range want {
		if !seen[factID] {
			t.Errorf("EvidenceFactIDs = %#v, want fact id %q", got, factID)
		}
	}
}
