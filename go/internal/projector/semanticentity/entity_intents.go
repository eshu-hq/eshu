// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticentity

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// semanticEntityFactKind mirrors root's FactKindParsedEntityObserved
// ("content_entity", declared in go/internal/projector/stage_facts.go)
// exactly. This package cannot import root -- root imports this package to
// dispatch, so the reverse direction cycles -- so the shared literal is
// duplicated here rather than referenced.
const semanticEntityFactKind = "content_entity"

// semanticEntityReducerTypes is the closed set of entity types that are
// semantic on their own, with no per-language metadata check. Every other
// candidate has to earn admission through one of the language predicates
// below.
var semanticEntityReducerTypes = map[string]struct{}{
	"Annotation":             {},
	"Typedef":                {},
	"TypeAlias":              {},
	"Component":              {},
	"Module":                 {},
	"ImplBlock":              {},
	"Protocol":               {},
	"ProtocolImplementation": {},
}

// BuildSemanticEntityReducerIntent returns one
// reducer.DomainSemanticEntityMaterialization intent for a single
// content_entity fact that carries semantic structure worth materializing,
// and reports false for every other fact. Unlike the scope-generation
// families, this builder is called once per input fact from root's
// buildProjection loop, so it reads a fact envelope rather than a
// projectorintent.FactLookup, and root's deterministic sort plus the
// reducer's per-entity-key claim collapse the repeated per-repo intents it
// emits. Admission is entity type first (semanticEntityReducerTypes), then
// the per-language predicates for callables and language-specific shapes; a
// fact with a blank repo_id is rejected because the entity key is the repo
// acceptance unit. The intent's source-system label is the raw
// SourceRef.SourceSystem, NOT the two-tier projectorintent.SourceSystem
// fallback the scope-generation families use -- that is the preserved
// pre-extraction behavior, not an oversight.
func BuildSemanticEntityReducerIntent(fact facts.Envelope) (projectorintent.ReducerIntent, bool) {
	if fact.FactKind != semanticEntityFactKind {
		return projectorintent.ReducerIntent{}, false
	}

	entityType, ok := payloadString(fact.Payload, "entity_type")
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	if _, ok := semanticEntityReducerTypes[entityType]; !ok {
		if !isJavaScriptCallableSemanticEntity(fact.Payload, entityType) &&
			!isGoCallableSemanticEntity(fact.Payload, entityType) &&
			!isPythonCallableSemanticEntity(fact.Payload, entityType) &&
			!isElixirCallableSemanticEntity(fact.Payload, entityType) &&
			!isTypeScriptJSXComponentTypeAssertionSemanticEntity(fact.Payload, entityType) &&
			!isTypeScriptJSXFragmentSemanticEntity(fact.Payload, entityType) &&
			!isTypeScriptModuleSemanticEntity(fact.Payload, entityType) &&
			!isElixirModuleAttributeSemanticEntity(fact.Payload, entityType) {
			return projectorintent.ReducerIntent{}, false
		}
	}

	repoID, _ := payloadString(fact.Payload, "repo_id")
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return projectorintent.ReducerIntent{}, false
	}

	return projectorintent.ReducerIntent{
		ScopeID:      fact.ScopeID,
		GenerationID: fact.GenerationID,
		Domain:       reducer.DomainSemanticEntityMaterialization,
		EntityKey:    semanticEntityAcceptanceUnitKey(repoID),
		Reason:       fmt.Sprintf("semantic entity follow-up for %s", entityType),
		FactID:       fact.FactID,
		SourceSystem: fact.SourceRef.SourceSystem,
	}, true
}

// semanticEntityAcceptanceUnitKey is the repository acceptance unit the
// reducer claims on. Every accepted entity in one repository shares this key,
// so the per-fact fan-out collapses to one claimable work item per repo.
func semanticEntityAcceptanceUnitKey(repoID string) string {
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return ""
	}
	return "repo:" + repoID
}

// isTypeScriptModuleSemanticEntity admits a TypeScript Module only when it is
// a namespace or participates in declaration merging. A plain ES module row
// carries no structure the reducer materializes.
func isTypeScriptModuleSemanticEntity(payload map[string]any, entityType string) bool {
	if entityType != "Module" {
		return false
	}
	if payloadMetadataString(payload, "language") != "typescript" {
		return false
	}
	if payloadMetadataString(payload, "module_kind") == "namespace" {
		return true
	}
	if payloadMetadataString(payload, "declaration_merge_group") != "" {
		return true
	}
	return len(payloadMetadataStringSlice(payload, "declaration_merge_kinds")) > 0
}

// isJavaScriptCallableSemanticEntity admits a JavaScript Function that
// carries a docstring or a method kind. The check is narrower than the Go one
// on purpose: JavaScript rows do not carry the wider callable metadata set.
func isJavaScriptCallableSemanticEntity(payload map[string]any, entityType string) bool {
	if entityType != "Function" {
		return false
	}
	if payloadMetadataString(payload, "language") != "javascript" {
		return false
	}
	return payloadMetadataString(payload, "docstring") != "" || payloadMetadataString(payload, "method_kind") != ""
}

// isGoCallableSemanticEntity admits a Go Function that carries any of the
// shared callable metadata. A plain package-level func with no metadata is
// rejected.
func isGoCallableSemanticEntity(payload map[string]any, entityType string) bool {
	if entityType != "Function" {
		return false
	}
	if payloadMetadataString(payload, "language") != "go" {
		return false
	}
	return hasCallableSemanticMetadata(payload)
}

// isPythonCallableSemanticEntity admits a Python Function that is a lambda,
// is async, or carries decorators.
func isPythonCallableSemanticEntity(payload map[string]any, entityType string) bool {
	if entityType != "Function" {
		return false
	}
	if payloadMetadataString(payload, "language") != "python" {
		return false
	}
	if payloadMetadataString(payload, "semantic_kind") == "lambda" {
		return true
	}
	if payloadMetadataBool(payload, "async") {
		return true
	}
	return len(payloadMetadataStringSlice(payload, "decorators")) > 0
}

// isElixirCallableSemanticEntity admits an Elixir guard definition
// (defguard/defguardp), which the parser emits as a Function with
// semantic_kind=guard.
func isElixirCallableSemanticEntity(payload map[string]any, entityType string) bool {
	if entityType != "Function" {
		return false
	}
	if payloadMetadataString(payload, "language") != "elixir" {
		return false
	}
	return payloadMetadataString(payload, "semantic_kind") == "guard"
}

// isElixirModuleAttributeSemanticEntity admits an Elixir module attribute,
// which the parser emits as a Variable with
// attribute_kind=module_attribute. Its reducer-side twin of the same name
// lives in go/internal/reducer/semanticentity/materialization_helpers.go and
// gates the same rows further down the pipeline.
func isElixirModuleAttributeSemanticEntity(payload map[string]any, entityType string) bool {
	if entityType != "Variable" {
		return false
	}
	if payloadMetadataString(payload, "language") != "elixir" {
		return false
	}
	return payloadMetadataString(payload, "attribute_kind") == "module_attribute"
}

// isTypeScriptJSXFragmentSemanticEntity admits a TSX Function that uses JSX
// fragment shorthand.
func isTypeScriptJSXFragmentSemanticEntity(payload map[string]any, entityType string) bool {
	if entityType != "Function" {
		return false
	}
	if payloadMetadataString(payload, "language") != "tsx" {
		return false
	}
	return payloadMetadataBool(payload, "jsx_fragment_shorthand")
}

// isTypeScriptJSXComponentTypeAssertionSemanticEntity admits a TSX Variable
// annotated as a component type. Its reducer-side twin of the same name lives
// in go/internal/reducer/semanticentity/materialization_helpers.go and gates
// the same rows further down the pipeline.
func isTypeScriptJSXComponentTypeAssertionSemanticEntity(payload map[string]any, entityType string) bool {
	if entityType != "Variable" {
		return false
	}
	if payloadMetadataString(payload, "language") != "tsx" {
		return false
	}
	return payloadMetadataString(payload, "component_type_assertion") != ""
}

// hasCallableSemanticMetadata reports whether a callable row carries any
// metadata that makes it worth materializing beyond its name and position.
func hasCallableSemanticMetadata(payload map[string]any) bool {
	for _, key := range []string{
		"docstring",
		"class_context",
		"method_kind",
		"constructor_kind",
		"annotation_kind",
		"context",
		"impl_context",
	} {
		if payloadMetadataString(payload, key) != "" {
			return true
		}
	}
	if len(payloadMetadataStringSlice(payload, "decorators")) > 0 {
		return true
	}
	if len(payloadMetadataStringSlice(payload, "type_parameters")) > 0 {
		return true
	}
	return payloadMetadataBool(payload, "async") ||
		payloadMetadataBool(payload, "jsx_fragment_shorthand")
}
