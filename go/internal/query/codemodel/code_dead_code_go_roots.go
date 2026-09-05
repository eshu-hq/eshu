// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// DeadCodePolicyStats carries dead-code scan counters the staying orchestrator threads through.
type DeadCodePolicyStats struct {
	RootsSkippedMissingSource    int
	ParserMetadataFrameworkRoots int
	SourceFallbackFrameworkRoots int
	GoSemanticRootsFromMetadata  int
}

// DeadCodeGoPolicyContext carries the per-candidate Go root evaluation state.
// DeadCodeGoPolicyContext carries the per-candidate Go root evaluation
// state: the resolved language, the normalized source, and the metadata
// root kinds. The fields are exported because the staying orchestrator
// reads them across the boundary via the root alias.
type DeadCodeGoPolicyContext struct {
	Language         string
	NormalizedSource string
	RootKinds        []string
}

// NewDeadCodeGoPolicyContext builds the per-candidate Go root policy context.
func NewDeadCodeGoPolicyContext(result map[string]any, entity *querycontract.EntityContent) DeadCodeGoPolicyContext {
	return DeadCodeGoPolicyContext{
		Language:         strings.ToLower(deadCodeEntityLanguage(result, entity)),
		NormalizedSource: deadCodeNormalizedSource(entity),
		RootKinds:        deadCodeRootKinds(result, entity),
	}
}

func deadCodeIsGoHTTPHandlerRoot(result map[string]any, policy DeadCodeGoPolicyContext) bool {
	if policy.Language != "go" {
		return false
	}
	if primaryEntityLabel(result) != "Function" {
		return false
	}
	if slices.Contains(policy.RootKinds, "go.net_http_handler_signature") ||
		slices.Contains(policy.RootKinds, "go.net_http_handler_registration") {
		return true
	}

	return strings.Contains(policy.NormalizedSource, "http.responsewriter") &&
		strings.Contains(policy.NormalizedSource, "*http.request")
}

func deadCodeIsGoCLICommandRoot(result map[string]any, policy DeadCodeGoPolicyContext) bool {
	if policy.Language != "go" {
		return false
	}
	if primaryEntityLabel(result) != "Function" {
		return false
	}
	if slices.Contains(policy.RootKinds, "go.cobra_run_signature") ||
		slices.Contains(policy.RootKinds, "go.cobra_run_registration") {
		return true
	}

	return strings.Contains(policy.NormalizedSource, "*cobra.command") &&
		strings.Contains(policy.NormalizedSource, "[]string")
}

func deadCodeIsGoFrameworkCallbackRoot(result map[string]any, policy DeadCodeGoPolicyContext) bool {
	if policy.Language != "go" {
		return false
	}
	if primaryEntityLabel(result) != "Function" {
		return false
	}
	if slices.Contains(policy.RootKinds, "go.controller_runtime_reconcile_signature") {
		return true
	}
	if strings.TrimSpace(querycontract.StringVal(result, "name")) != "Reconcile" {
		return false
	}

	if !strings.Contains(policy.NormalizedSource, "context.context") {
		return false
	}
	if !strings.Contains(policy.NormalizedSource, "request") {
		return false
	}

	return (strings.Contains(policy.NormalizedSource, "ctrl.request") || strings.Contains(policy.NormalizedSource, "reconcile.request")) &&
		(strings.Contains(policy.NormalizedSource, "ctrl.result") || strings.Contains(policy.NormalizedSource, "reconcile.result"))
}

func deadCodeNormalizedSource(entity *querycontract.EntityContent) string {
	if entity == nil {
		return ""
	}
	normalized := strings.ToLower(stripGoComments(entity.SourceCache))
	if normalized == "" {
		return ""
	}
	return strings.Join(strings.Fields(normalized), " ")
}

func deadCodeRootKinds(result map[string]any, entity *querycontract.EntityContent) []string {
	if metadata, ok := result["metadata"].(map[string]any); ok {
		if kinds := querycontract.DeadCodeRootKindsFromMetadata(metadata); len(kinds) > 0 {
			return kinds
		}
	}
	if entity == nil {
		return nil
	}
	return querycontract.DeadCodeRootKindsFromMetadata(entity.Metadata)
}

var deadCodeGoSemanticRootKinds = map[string]struct{}{
	"go.dependency_injection_callback":   {},
	"go.direct_method_call":              {},
	"go.fmt_stringer_method":             {},
	"go.function_literal_reachable_call": {},
	"go.function_value_reference":        {},
	"go.generic_constraint_method":       {},
	"go.imported_direct_method_call":     {},
	"go.imported_fmt_stringer_method":    {},
	"go.interface_implementation_type":   {},
	"go.interface_method_implementation": {},
	"go.interface_type_reference":        {},
	"go.method_value_reference":          {},
	"go.type_reference":                  {},
}

// deadCodeIsGoSemanticRoot honors parser or reducer evidence that a Go symbol
// participates in language-level dispatch that direct CALLS edges may not show.
func DeadCodeIsGoSemanticRoot(result map[string]any, policy DeadCodeGoPolicyContext, stats *DeadCodePolicyStats) bool {
	if policy.Language != "go" {
		return false
	}
	for _, rootKind := range policy.RootKinds {
		if _, ok := deadCodeGoSemanticRootKinds[rootKind]; ok {
			stats.GoSemanticRootsFromMetadata++
			return true
		}
	}
	return false
}

func stripGoComments(source string) string {
	if source == "" {
		return ""
	}

	var out strings.Builder
	out.Grow(len(source))
	for i := 0; i < len(source); {
		switch {
		case i+1 < len(source) && source[i] == '/' && source[i+1] == '/':
			i += 2
			for i < len(source) && source[i] != '\n' {
				i++
			}
		case i+1 < len(source) && source[i] == '/' && source[i+1] == '*':
			i += 2
			for i+1 < len(source) && (source[i] != '*' || source[i+1] != '/') {
				i++
			}
			if i+1 < len(source) {
				i += 2
			} else {
				i = len(source)
			}
		default:
			out.WriteByte(source[i])
			i++
		}
	}
	return out.String()
}
