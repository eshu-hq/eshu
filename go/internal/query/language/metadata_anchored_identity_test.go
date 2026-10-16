// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

// The id-anchored enrichment is only exact if, for every label in
// contentIDAnchoredLabels, the graph node's id is the content_entities primary
// key for the same entity. Neither side is copied here: the graph id comes from
// the projector's own production entity extraction (canonical.ExtractEntityRows,
// the path BuildMaterialization uses), and the content id from the function
// every content mint site calls (content.CanonicalEntityIDWithMetadata, used by
// content/shape.Materialize and the projector runtime's fallback).
//
// The fact carries dependency-shaped metadata so a label for which the
// dependency-identity branch could fire would produce a different content id,
// and it is run with three incoming ids -- the minted one, a stale one, and
// none -- so a label the projector stops deriving (falling back to the
// producer-supplied id) also fails.
const (
	identityPinRepo = "repository:r_identity_pin"
	identityPinPath = "src/pkg/thing.php"
	identityPinName = "thing"
	identityPinLine = 42
)

func identityPinDependencyMetadata() map[string]any {
	return map[string]any{
		"config_kind":     "dependency",
		"package_manager": "npm",
		"section":         "dependencies",
	}
}

func identityPinEnvelope(entityType, incomingID string) facts.Envelope {
	payload := map[string]any{
		"entity_type":     entityType,
		"entity_name":     identityPinName,
		"relative_path":   identityPinPath,
		"start_line":      identityPinLine,
		"end_line":        identityPinLine + 3,
		"language":        "php",
		"repo_id":         identityPinRepo,
		"entity_metadata": identityPinDependencyMetadata(),
	}
	if incomingID != "" {
		payload["entity_id"] = incomingID
	}
	return facts.Envelope{
		FactID:   "identity-pin-" + entityType + "-" + incomingID,
		ScopeID:  "scope-identity-pin",
		FactKind: "content_entity",
		Payload:  payload,
	}
}

// TestContentIDAnchoredLabelsShareTheProjectorGraphID pins the label set
// against the projector: every anchored label's graph id must equal its
// content id, whatever id the producer supplied.
func TestContentIDAnchoredLabelsShareTheProjectorGraphID(t *testing.T) {
	t.Parallel()

	labels := make([]string, 0, len(contentIDAnchoredLabels))
	for label := range contentIDAnchoredLabels {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	if len(labels) == 0 {
		t.Fatal("contentIDAnchoredLabels is empty")
	}

	for _, label := range labels {
		entityType := graphLabelToContentEntityType(label)
		if entityType == "" {
			t.Errorf("%s: anchored label has no content entity type; the enrichment never reaches it", label)
			continue
		}
		contentID := content.CanonicalEntityIDWithMetadata(
			identityPinRepo, identityPinPath, entityType, identityPinName, identityPinLine,
			identityPinDependencyMetadata(),
		)
		for _, incoming := range []string{contentID, "producer-supplied-stale-id", ""} {
			rows := canonical.ExtractEntityRows(
				[]facts.Envelope{identityPinEnvelope(entityType, incoming)},
				identityPinRepo, "/repos/identity-pin",
			)
			if len(rows) != 1 {
				t.Errorf("%s (incoming id %q): projector wrote %d graph rows, want 1", label, incoming, len(rows))
				continue
			}
			if rows[0].Label != label {
				t.Errorf("%s (incoming id %q): projector label = %q", label, incoming, rows[0].Label)
			}
			if rows[0].EntityID != contentID {
				t.Errorf("%s (incoming id %q): graph id %q != content id %q; an id-keyed merge would miss or mis-attach this label",
					label, incoming, rows[0].EntityID, contentID)
			}
		}
	}
}

// TestContentIDAnchoredLabelsExcludeVariable pins the #7247 correction:
// Variable stays on the page read. A dependency-identity Variable's content id
// is CanonicalDependencyEntityID, not the line-keyed id a graph Variable node
// carries, so the two keys name different rows.
func TestContentIDAnchoredLabelsExcludeVariable(t *testing.T) {
	t.Parallel()

	if _, ok := contentIDAnchoredLabels["Variable"]; ok {
		t.Fatal("Variable is in contentIDAnchoredLabels; dependency-identity Variables have a different content id")
	}
	lineKeyed := content.CanonicalEntityID(identityPinRepo, identityPinPath, "Variable", identityPinName, identityPinLine)
	dependencyKeyed := content.CanonicalEntityIDWithMetadata(
		identityPinRepo, identityPinPath, "Variable", identityPinName, identityPinLine,
		identityPinDependencyMetadata(),
	)
	if lineKeyed == dependencyKeyed {
		t.Fatal("a dependency Variable's content id equals the line-keyed id; the Variable exclusion rests on a premise that no longer holds -- re-derive it")
	}
}

// TestContentIDAnchoredLabelsAreRouteReachable keeps the set honest about
// what it changes: every member is a label one of the route's graph-reading
// dispatch maps can send to the enrichment.
func TestContentIDAnchoredLabelsAreRouteReachable(t *testing.T) {
	t.Parallel()

	reachable := map[string]struct{}{}
	for _, label := range graphBackedEntityTypes {
		reachable[label] = struct{}{}
	}
	for _, label := range graphFirstContentBackedEntityTypes {
		reachable[label] = struct{}{}
	}
	for label := range contentIDAnchoredLabels {
		if _, ok := reachable[label]; !ok {
			t.Errorf("%s is anchored but no graph-reading dispatch branch of the route reaches it", label)
		}
	}
}
