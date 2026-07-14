// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

const repoDependencyConcurrencyOduName = "odu:repo-dependency-concurrency"

type oduEvidenceLoader struct {
	evidence []relationships.EvidenceFact
}

func (l oduEvidenceLoader) ListEvidenceFacts(
	_ context.Context,
	_ string,
) ([]relationships.EvidenceFact, error) {
	return append([]relationships.EvidenceFact(nil), l.evidence...), nil
}

type oduIntentWriter struct {
	rows []reducer.SharedProjectionIntentRow
}

func (w *oduIntentWriter) UpsertIntents(
	_ context.Context,
	rows []reducer.SharedProjectionIntentRow,
) error {
	w.rows = append(w.rows, rows...)
	return nil
}

func TestRepoDependencyConcurrencyOduProducesHostileProductionIntents(t *testing.T) {
	t.Parallel()

	odu, ok := ifa.CatalogByName()[repoDependencyConcurrencyOduName]
	if !ok {
		t.Fatalf("Ifa catalog is missing %q", repoDependencyConcurrencyOduName)
	}

	evidence := ifa.DiscoveredEvidence(odu)
	if len(evidence) == 0 {
		t.Fatalf("DiscoveredEvidence(%q) returned no production evidence", odu.Name)
	}

	writer := &oduIntentWriter{}
	evidenceBySource := make(map[string][]relationships.EvidenceFact)
	for _, item := range evidence {
		evidenceBySource[item.SourceRepoID] = append(evidenceBySource[item.SourceRepoID], item)
	}

	totalCount := 0
	for _, source := range sortedEvidenceSources(evidenceBySource) {
		scopeID, generationID := sourceCoordinates(t, odu, source)
		sourceEvidence := evidenceBySource[source]
		// Duplicate delivery is part of the hostile scenario. The production
		// resolver must dedupe it before constructing durable intent identities.
		sourceEvidence = append(sourceEvidence, sourceEvidence...)
		handler := reducer.CrossRepoRelationshipHandler{
			EvidenceLoader: oduEvidenceLoader{evidence: sourceEvidence},
			IntentWriter:   writer,
		}
		count, err := handler.Resolve(context.Background(), scopeID, generationID)
		if err != nil {
			t.Fatalf("CrossRepoRelationshipHandler.Resolve(%q) error = %v", source, err)
		}
		totalCount += count
	}
	if totalCount != len(writer.rows) {
		t.Fatalf("Resolve() count = %d, captured rows = %d", totalCount, len(writer.rows))
	}

	assertRepoDependencyOduIntentTruth(t, writer.rows)
}

func assertRepoDependencyOduIntentTruth(
	t *testing.T,
	rows []reducer.SharedProjectionIntentRow,
) {
	t.Helper()

	intentIDs := make(map[string]struct{}, len(rows))
	sources := make(map[string]struct{})
	typedEdges := make(map[string]struct{})
	provisionTargets := make(map[string]map[string]struct{})
	coordinates := make(map[string]string)
	writeRows := 0
	evidenceArtifacts := 0

	for _, row := range rows {
		if _, duplicate := intentIDs[row.IntentID]; duplicate {
			t.Fatalf("duplicate durable intent id %q after duplicate evidence replay", row.IntentID)
		}
		intentIDs[row.IntentID] = struct{}{}

		action := oduPayloadString(row.Payload, "action")
		if action == "retract" || action == "delete" {
			continue
		}
		writeRows++

		source := oduPayloadString(row.Payload, "repo_id")
		target := oduPayloadString(row.Payload, "target_repo_id")
		relationshipType := oduPayloadString(row.Payload, "relationship_type")
		if source == "" || target == "" || relationshipType == "" {
			t.Fatalf("write row is missing source, target, or relationship type: %#v", row)
		}
		if row.AcceptanceUnitID != source {
			t.Fatalf(
				"intent %q acceptance unit = %q, want whole source repository %q",
				row.IntentID,
				row.AcceptanceUnitID,
				source,
			)
		}
		if source == target {
			t.Fatalf("self relationship escaped Odù exclusion: %s -> %s (%s)", source, target, relationshipType)
		}

		sources[source] = struct{}{}
		typedEdges[edgeKey(source, relationshipType, target)] = struct{}{}
		coordinates[source] = row.ScopeID + "\x00" + row.GenerationID
		evidenceArtifacts += assertOduEvidenceArtifacts(t, row)
		if relationshipType == string(relationships.RelProvisionsDependencyFor) {
			if provisionTargets[target] == nil {
				provisionTargets[target] = make(map[string]struct{})
			}
			provisionTargets[target][source] = struct{}{}
		}
	}

	if got, want := len(sources), 8; got != want {
		t.Fatalf("source acceptance units = %d, want %d; sources=%v", got, want, sortedKeys(sources))
	}
	if got, want := writeRows, 8; got != want {
		t.Fatalf("write intents = %d, want %d", got, want)
	}
	if !hasSharedProvisionTarget(provisionTargets, 4) {
		t.Fatalf("no PROVISIONS_DEPENDENCY_FOR target receives the required 4-source fan-in: %#v", provisionTargets)
	}
	if got, want := len(coordinates), 8; got != want {
		t.Fatalf("source scope/generation coordinates = %d, want %d", got, want)
	}
	if got, want := evidenceArtifacts, 8; got != want {
		t.Fatalf("evidence artifacts = %d, want %d", got, want)
	}
	reciprocalFound := false
	for edge := range typedEdges {
		source, relationshipType, target := splitEdgeKey(t, edge)
		if _, sourceIsFixtureUnit := sources[target]; !sourceIsFixtureUnit {
			continue
		}
		if _, reciprocal := typedEdges[edgeKey(target, relationshipType, source)]; !reciprocal {
			t.Fatalf("missing reciprocal %s edge for %s -> %s", relationshipType, source, target)
		}
		reciprocalFound = true
	}
	if !reciprocalFound {
		t.Fatal("Odù produced no reciprocal source-repository relationship pair")
	}
}

func assertOduEvidenceArtifacts(t *testing.T, row reducer.SharedProjectionIntentRow) int {
	t.Helper()
	artifacts, ok := row.Payload["evidence_artifacts"].([]map[string]any)
	if !ok {
		t.Fatalf("intent %q evidence_artifacts type = %T, want []map[string]any", row.IntentID, row.Payload["evidence_artifacts"])
	}
	if len(artifacts) != 1 {
		t.Fatalf("intent %q evidence artifacts = %d, want 1", row.IntentID, len(artifacts))
	}
	if got, want := oduPayloadString(artifacts[0], "path"), "env/ifa-prod-proof/main.tf"; got != want {
		t.Fatalf("intent %q evidence path = %q, want %q", row.IntentID, got, want)
	}
	if got, want := oduPayloadString(artifacts[0], "environment"), "ifa-prod-proof"; got != want {
		t.Fatalf("intent %q evidence environment = %q, want %q", row.IntentID, got, want)
	}
	return len(artifacts)
}

func oduPayloadString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

func edgeKey(source, relationshipType, target string) string {
	return source + "\x00" + relationshipType + "\x00" + target
}

func splitEdgeKey(t *testing.T, key string) (string, string, string) {
	t.Helper()
	parts := strings.Split(key, "\x00")
	if len(parts) != 3 {
		t.Fatalf("invalid edge key %q", key)
	}
	return parts[0], parts[1], parts[2]
}

func hasSharedProvisionTarget(targets map[string]map[string]struct{}, sourceCount int) bool {
	for _, sources := range targets {
		if len(sources) == sourceCount {
			return true
		}
	}
	return false
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedEvidenceSources(values map[string][]relationships.EvidenceFact) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sourceCoordinates(t *testing.T, odu ifa.Odu, source string) (string, string) {
	t.Helper()
	for _, fact := range odu.Facts {
		if fact.FactKind != "repository" || oduPayloadString(fact.Payload, "repo_id") != source {
			continue
		}
		if strings.TrimSpace(fact.ScopeID) == "" || strings.TrimSpace(fact.GenerationID) == "" {
			t.Fatalf("source repository %q is missing its Odù scope or generation", source)
		}
		return fact.ScopeID, fact.GenerationID
	}
	t.Fatalf("source repository %q has no matching repository fact", source)
	return "", ""
}
