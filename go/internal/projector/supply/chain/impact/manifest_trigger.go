// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// The manifest-dependency trigger (#9874). A git generation that changes a
// manifest or lockfile carries content_entity dependency rows but none of the
// twelve candidate kinds, so impact matching never re-ran for it. The marker
// intent asks the reducer to reopen the scopes that hold advisory evidence for
// the changed packages; it does not ask for a findings pass of its own.
const (
	// contentEntityFactKind is the git collector's parsed-entity fact kind.
	contentEntityFactKind = "content_entity"
	// payloadKeyTrigger and triggerManifestDependency are the marker the
	// reducer branches on. They must equal core.PayloadKeyTrigger and
	// core.TriggerManifestDependency; manifest_trigger_test.go asserts it.
	payloadKeyTrigger         = "trigger"
	triggerManifestDependency = "manifest_dependency"
	// manifestDependencyReason is the intent's human-readable reason.
	manifestDependencyReason = "manifest dependency evidence observed"
)

// buildManifestDependencyIntent returns the marker intent for a git
// generation, anchored on its earliest dependency row, or on its repository
// fact when it holds none. The entity key is the same
// supply_chain_impact:<scope> key, so a push that changes 500 pins is still
// one intent.
func buildManifestDependencyIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKindMatching(contentEntityFactKind, manifestDependencyFact)
	if !ok {
		// #9874 F2: every git generation raises the marker, not only one
		// holding a dependency row. A deleted lockfile, a reverted path, or a
		// zero-row re-index changes findings with no dependency row to show
		// for it; the reducer diffs the generation's full view against the
		// chain and reopens nothing when nothing changed.
		envelope, ok = lookup.FirstAcrossKinds(gitRepositoryFact, repositoryFactKinds...)
	}
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainSupplyChainImpact,
		EntityKey:    "supply_chain_impact:" + scopeID,
		Reason:       manifestDependencyReason,
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
		Payload:      map[string]any{payloadKeyTrigger: triggerManifestDependency},
	}, true
}

// repositoryFactKinds are the git repository fact kinds, legacy suffix
// included.
var repositoryFactKinds = []string{"repository", "repositoryFact"}

// gitRepositoryFact reports whether envelope is a git generation's repository
// fact.
func gitRepositoryFact(envelope facts.Envelope) bool {
	return strings.TrimSpace(envelope.SourceRef.SourceSystem) == "git"
}

// manifestDependencyFact reports whether envelope is a git manifest or
// lockfile dependency row with the shape correlation.
// ExtractPackageManifestDependencies accepts: a content_entity Variable whose
// config_kind is dependency, from the git source, naming a repository, a
// package manager, and a dependency, and not a central version declaration.
// A tombstoned row qualifies too: a removed dependency changes findings.
//
// The checks run most selective first, because the predicate visits every
// content_entity of a generation that holds no dependency row: config_kind
// refuses almost every entity in two map reads, before the entity type,
// source, and identity reads that only dependency rows reach. Payloads are
// already decoded maps, so no check decodes JSON.
func manifestDependencyFact(envelope facts.Envelope) bool {
	if envelope.FactKind != contentEntityFactKind || !dependencyConfigKind(envelope.Payload) {
		return false
	}
	if payloadString(envelope.Payload, "entity_type") != "Variable" {
		return false
	}
	if strings.TrimSpace(envelope.SourceRef.SourceSystem) != "git" {
		return false
	}
	if strings.EqualFold(metadataString(envelope.Payload, "central_version_declaration"), "true") {
		return false
	}
	return payloadString(envelope.Payload, "repo_id") != "" &&
		payloadString(envelope.Payload, "entity_name") != "" &&
		metadataString(envelope.Payload, "package_manager") != ""
}

// dependencyConfigKind reports whether the row's config_kind is dependency,
// with the extractor's precedence: a non-blank top-level config_kind wins over
// entity_metadata. It reads entity_metadata first because parsers write the
// key there, so a refused row costs the metadata read plus one top-level miss.
func dependencyConfigKind(payload map[string]any) bool {
	metadata, _ := payload["entity_metadata"].(map[string]any)
	if anyText(metadata["config_kind"]) == "dependency" {
		top := anyText(payload["config_kind"])
		return top == "" || top == "dependency"
	}
	return anyText(payload["config_kind"]) == "dependency"
}

// payloadString returns the trimmed string at key, or "" when absent or not a
// string.
func payloadString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

// metadataString reads key from the top level, then from entity_metadata,
// the same fallback the reducer's manifest extractor uses. A non-string value
// (a bool central-version flag) is read by its text form.
func metadataString(payload map[string]any, key string) string {
	if value := anyText(payload[key]); value != "" {
		return value
	}
	metadata, _ := payload["entity_metadata"].(map[string]any)
	return anyText(metadata[key])
}

func anyText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}
