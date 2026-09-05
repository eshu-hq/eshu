// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// This file is part of the #6060 lane-A P0 shim split out of
// family_code_shim.go to keep every file under the repo's 500-line
// cap. The deletion protocol in family_code_shim.go's header applies
// to every entry here: delete each entry with its named handler move.

import (
	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
)

// DeadCodePolicyStats aliases the leaf-owned scan counters so the staying
// dead-code orchestrator, scan, investigation, and cross-repo readers keep
// their signatures and literals unchanged. Delete with code_dead_code.go's
// handler move.
type DeadCodePolicyStats = codemodel.DeadCodePolicyStats

// deadCodeGoPolicyContext aliases the leaf-owned per-candidate Go policy
// state so the staying orchestrator keeps threading it opaquely. Delete
// with code_dead_code.go's handler move.
type deadCodeGoPolicyContext = codemodel.DeadCodeGoPolicyContext

// deadCodeClassificationAmbiguous aliases the leaf-owned classification so
// the staying scan, investigation, and cross-repo readers keep comparing
// through it. Delete with code_dead_code.go's handler move.
const deadCodeClassificationAmbiguous = codemodel.DeadCodeClassificationAmbiguous

// deadCodeClassificationExcluded aliases the leaf-owned classification so
// the staying scan and investigation keep comparing through it. Delete with
// code_dead_code.go's handler move.
const deadCodeClassificationExcluded = codemodel.DeadCodeClassificationExcluded

// deadCodeClassificationUnused aliases the leaf-owned classification so
// the staying investigation keeps comparing through it. Delete with
// code_dead_code.go's handler move.
const deadCodeClassificationUnused = codemodel.DeadCodeClassificationUnused

// deadCodeHiddenConsumerResultKey aliases the leaf-owned hidden-consumer
// marker so the staying scan keeps stamping through it. Delete with
// code_dead_code.go's handler move.
const deadCodeHiddenConsumerResultKey = codemodel.DeadCodeHiddenConsumerResultKey

// deadCodeHiddenConsumerReason aliases the leaf-owned hidden-consumer
// reason so the staying investigation keeps reporting through it. Delete
// with code_dead_code.go's handler move.
const deadCodeHiddenConsumerReason = codemodel.DeadCodeHiddenConsumerReason

// deadCodeLanguageMaturity aliases the leaf-owned language maturity table
// so the staying investigation and contract tests keep reading through it
// (same underlying map). Delete with code_dead_code.go's handler move.
var deadCodeLanguageMaturity = codemodel.DeadCodeLanguageMaturity

// elixirDeadCodeMetadataRootKinds aliases the leaf-owned Elixir metadata
// root kinds so the staying roots test keeps iterating them (same slice).
// Delete with code_dead_code.go's handler move.
var elixirDeadCodeMetadataRootKinds = codemodel.ElixirDeadCodeMetadataRootKinds

// phpDeadCodeMetadataRootKinds aliases the leaf-owned PHP metadata root
// kinds so the staying roots test keeps iterating them (same slice).
// Delete with code_dead_code.go's handler move.
var phpDeadCodeMetadataRootKinds = codemodel.PHPDeadCodeMetadataRootKinds

// buildDeadCodeAnalysis forwards to the leaf-owned summarizer so the
// staying dead-code tests keep their call sites unchanged. Delete with
// code_dead_code.go's handler move.
func buildDeadCodeAnalysis(results []map[string]any, excluded []string, stats DeadCodePolicyStats) map[string]any {
	return codemodel.BuildDeadCodeAnalysis(results, excluded, stats)
}

// buildDeadCodeAnalysisForLanguage forwards to the leaf-owned summarizer
// so the staying orchestrator, scan, investigation, cross-repo readers,
// and tests keep their call sites unchanged. Delete with
// code_dead_code.go's handler move.
func buildDeadCodeAnalysisForLanguage(
	results []map[string]any,
	excluded []string,
	stats DeadCodePolicyStats,
	language string,
) map[string]any {
	return codemodel.BuildDeadCodeAnalysisForLanguage(results, excluded, stats, language)
}

// classifyDeadCodeResults forwards to the leaf-owned classifier so the
// staying scan keeps its call site unchanged. Delete with
// code_dead_code_scan.go's reader move.
func classifyDeadCodeResults(results []map[string]any, contentByID map[string]*EntityContent) {
	codemodel.ClassifyDeadCodeResults(results, contentByID)
}

// deadCodeResultClassification forwards to the leaf-owned classifier so
// the staying investigation keeps its call site unchanged. Delete with
// code_dead_code.go's handler move.
func deadCodeResultClassification(result map[string]any, entity *EntityContent) string {
	return codemodel.DeadCodeResultClassification(result, entity)
}

// deadCodeResultHasHiddenConsumer forwards to the leaf-owned predicate so
// the staying investigation keeps its call site unchanged. Delete with
// code_dead_code.go's handler move.
func deadCodeResultHasHiddenConsumer(result map[string]any) bool {
	return codemodel.DeadCodeResultHasHiddenConsumer(result)
}

// deadCodeWeakIncomingAmbiguityReason forwards to the leaf-owned reason
// builder so the staying investigation keeps its call site unchanged.
// Delete with code_dead_code.go's handler move.
func deadCodeWeakIncomingAmbiguityReason(result map[string]any) (string, bool) {
	return codemodel.DeadCodeWeakIncomingAmbiguityReason(result)
}

// deadCodeLanguageSupported forwards to the leaf-owned language gate so
// the staying orchestrator keeps its call site unchanged. Delete with
// code_dead_code.go's handler move.
func deadCodeLanguageSupported(language string) bool {
	return codemodel.DeadCodeLanguageSupported(language)
}

// deadCodeLanguageMaturityReport forwards to the leaf-owned maturity table
// so the staying investigation keeps its call site unchanged. Delete with
// code_dead_code.go's handler move.
func deadCodeLanguageMaturityReport() map[string]string {
	return codemodel.DeadCodeLanguageMaturityReport()
}

// deadCodeLanguageExactnessBlockerReport forwards to the leaf-owned blocker
// table so the staying investigation keeps its call site unchanged. Delete
// with code_dead_code.go's handler move.
func deadCodeLanguageExactnessBlockerReport() map[string][]string {
	return codemodel.DeadCodeLanguageExactnessBlockerReport()
}

// deadCodeIsCRoot forwards to the leaf-owned C entrypoint predicate so the
// staying orchestrator keeps its call site unchanged. Delete with
// code_dead_code.go's handler move.
func deadCodeIsCRoot(result map[string]any, entity *EntityContent, stats *DeadCodePolicyStats) bool {
	return codemodel.DeadCodeIsCRoot(result, entity, stats)
}

// deadCodeIsCPPRoot forwards to the leaf-owned C++ entrypoint predicate so
// the staying orchestrator keeps its call site unchanged. Delete with
// code_dead_code.go's handler move.
func deadCodeIsCPPRoot(result map[string]any, entity *EntityContent, stats *DeadCodePolicyStats) bool {
	return codemodel.DeadCodeIsCPPRoot(result, entity, stats)
}

// deadCodeIsCSharpRoot forwards to the leaf-owned C# entrypoint predicate
// so the staying orchestrator keeps its call site unchanged. Delete with
// code_dead_code.go's handler move.
func deadCodeIsCSharpRoot(result map[string]any, entity *EntityContent, stats *DeadCodePolicyStats) bool {
	return codemodel.DeadCodeIsCSharpRoot(result, entity, stats)
}

// deadCodeIsDartRoot forwards to the leaf-owned Dart entrypoint predicate
// so the staying orchestrator keeps its call site unchanged. Delete with
// code_dead_code.go's handler move.
func deadCodeIsDartRoot(result map[string]any, entity *EntityContent, stats *DeadCodePolicyStats) bool {
	return codemodel.DeadCodeIsDartRoot(result, entity, stats)
}

// deadCodeIsElixirRoot forwards to the leaf-owned Elixir entrypoint
// predicate so the staying orchestrator keeps its call site unchanged.
// Delete with code_dead_code.go's handler move.
func deadCodeIsElixirRoot(result map[string]any, entity *EntityContent, stats *DeadCodePolicyStats) bool {
	return codemodel.DeadCodeIsElixirRoot(result, entity, stats)
}

// deadCodeIsGoFrameworkRoot forwards to the leaf-owned Go framework-root
// predicate so the staying orchestrator keeps its call site unchanged.
// Delete with code_dead_code.go's handler move.
func deadCodeIsGoFrameworkRoot(
	result map[string]any,
	policy deadCodeGoPolicyContext,
	stats *DeadCodePolicyStats,
) bool {
	return codemodel.DeadCodeIsGoFrameworkRoot(result, policy, stats)
}

// deadCodeIsGoSemanticRoot forwards to the leaf-owned Go semantic-root
// predicate so the staying orchestrator keeps its call site unchanged.
// Delete with code_dead_code.go's handler move.
func deadCodeIsGoSemanticRoot(
	result map[string]any,
	policy deadCodeGoPolicyContext,
	stats *DeadCodePolicyStats,
) bool {
	return codemodel.DeadCodeIsGoSemanticRoot(result, policy, stats)
}

// deadCodeIsGroovyRoot forwards to the leaf-owned Groovy entrypoint
// predicate so the staying orchestrator keeps its call site unchanged.
// Delete with code_dead_code.go's handler move.
func deadCodeIsGroovyRoot(result map[string]any, entity *EntityContent, stats *DeadCodePolicyStats) bool {
	return codemodel.DeadCodeIsGroovyRoot(result, entity, stats)
}

// deadCodeIsHaskellRoot forwards to the leaf-owned Haskell entrypoint
// predicate so the staying orchestrator keeps its call site unchanged.
// Delete with code_dead_code.go's handler move.
func deadCodeIsHaskellRoot(result map[string]any, entity *EntityContent, stats *DeadCodePolicyStats) bool {
	return codemodel.DeadCodeIsHaskellRoot(result, entity, stats)
}

// deadCodeIsJavaRoot forwards to the leaf-owned Java entrypoint predicate
// so the staying orchestrator keeps its call site unchanged. Delete with
// code_dead_code.go's handler move.
func deadCodeIsJavaRoot(result map[string]any, entity *EntityContent, stats *DeadCodePolicyStats) bool {
	return codemodel.DeadCodeIsJavaRoot(result, entity, stats)
}

// deadCodeIsJavaScriptFrameworkRoot forwards to the leaf-owned JavaScript
// framework-root predicate so the staying orchestrator keeps its call site
// unchanged. Delete with code_dead_code.go's handler move.
func deadCodeIsJavaScriptFrameworkRoot(
	result map[string]any,
	entity *EntityContent,
	stats *DeadCodePolicyStats,
) bool {
	return codemodel.DeadCodeIsJavaScriptFrameworkRoot(result, entity, stats)
}

// deadCodeIsKotlinRoot forwards to the leaf-owned Kotlin entrypoint
// predicate so the staying orchestrator keeps its call site unchanged.
// Delete with code_dead_code.go's handler move.
func deadCodeIsKotlinRoot(result map[string]any, entity *EntityContent, stats *DeadCodePolicyStats) bool {
	return codemodel.DeadCodeIsKotlinRoot(result, entity, stats)
}

// deadCodeIsPerlRoot forwards to the leaf-owned Perl entrypoint predicate
// so the staying orchestrator keeps its call site unchanged. Delete with
// code_dead_code.go's handler move.
func deadCodeIsPerlRoot(result map[string]any, entity *EntityContent, stats *DeadCodePolicyStats) bool {
	return codemodel.DeadCodeIsPerlRoot(result, entity, stats)
}

// deadCodeIsPHPRoot forwards to the leaf-owned PHP entrypoint predicate so
// the staying orchestrator keeps its call site unchanged. Delete with
// code_dead_code.go's handler move.
func deadCodeIsPHPRoot(result map[string]any, entity *EntityContent, stats *DeadCodePolicyStats) bool {
	return codemodel.DeadCodeIsPHPRoot(result, entity, stats)
}

// deadCodeIsPythonFrameworkRoot forwards to the leaf-owned Python
// framework-root predicate so the staying orchestrator keeps its call site
// unchanged. Delete with code_dead_code.go's handler move.
func deadCodeIsPythonFrameworkRoot(
	result map[string]any,
	entity *EntityContent,
	stats *DeadCodePolicyStats,
) bool {
	return codemodel.DeadCodeIsPythonFrameworkRoot(result, entity, stats)
}

// deadCodeIsPythonAnonymousLambda forwards to the leaf-owned anonymous
// lambda predicate so the staying orchestrator keeps its call site
// unchanged. Delete with code_dead_code.go's handler move.
func deadCodeIsPythonAnonymousLambda(result map[string]any, entity *EntityContent) bool {
	return codemodel.DeadCodeIsPythonAnonymousLambda(result, entity)
}

// deadCodeIsRubyRoot forwards to the leaf-owned Ruby entrypoint predicate
// so the staying orchestrator and verdict tests keep their call sites
// unchanged. Delete with code_dead_code.go's handler move.
func deadCodeIsRubyRoot(
	result map[string]any,
	entity *EntityContent,
	stats *DeadCodePolicyStats,
	downgraded deadCodeDowngradedRoots,
) bool {
	return codemodel.DeadCodeIsRubyRoot(result, entity, stats, downgraded)
}

// deadCodeIsRustRoot forwards to the leaf-owned Rust entrypoint predicate
// so the staying orchestrator keeps its call site unchanged. Delete with
// code_dead_code.go's handler move.
func deadCodeIsRustRoot(result map[string]any, entity *EntityContent, stats *DeadCodePolicyStats) bool {
	return codemodel.DeadCodeIsRustRoot(result, entity, stats)
}

// deadCodeIsRustCargoAuxiliaryTarget forwards to the leaf-owned cargo
// auxiliary-target predicate so the staying orchestrator keeps its call
// site unchanged. Delete with code_dead_code.go's handler move.
func deadCodeIsRustCargoAuxiliaryTarget(result map[string]any, entity *EntityContent) bool {
	return codemodel.DeadCodeIsRustCargoAuxiliaryTarget(result, entity)
}

// deadCodeIsScalaRoot forwards to the leaf-owned Scala entrypoint
// predicate so the staying orchestrator keeps its call site unchanged.
// Delete with code_dead_code.go's handler move.
func deadCodeIsScalaRoot(result map[string]any, entity *EntityContent, stats *DeadCodePolicyStats) bool {
	return codemodel.DeadCodeIsScalaRoot(result, entity, stats)
}

// deadCodeIsSwiftRoot forwards to the leaf-owned Swift entrypoint
// predicate so the staying orchestrator keeps its call site unchanged.
// Delete with code_dead_code.go's handler move.
func deadCodeIsSwiftRoot(result map[string]any, entity *EntityContent, stats *DeadCodePolicyStats) bool {
	return codemodel.DeadCodeIsSwiftRoot(result, entity, stats)
}

// deadCodeIsGeneratedCode forwards to the leaf-owned generated-code
// predicate so the staying orchestrator keeps its call site unchanged.
// Delete with code_dead_code.go's handler move.
func deadCodeIsGeneratedCode(result map[string]any, entity *EntityContent) bool {
	return codemodel.DeadCodeIsGeneratedCode(result, entity)
}

// deadCodeIsTestFile forwards to the leaf-owned test-file predicate so the
// staying orchestrator and exclusion tests keep their call sites unchanged.
// Delete with code_dead_code.go's handler move.
func deadCodeIsTestFile(result map[string]any, entity *EntityContent) bool {
	return codemodel.DeadCodeIsTestFile(result, entity)
}

// newDeadCodeGoPolicyContext forwards to the leaf-owned policy constructor
// so the staying orchestrator keeps threading the context opaquely. Delete
// with code_dead_code.go's handler move.
func newDeadCodeGoPolicyContext(
	result map[string]any,
	entity *EntityContent,
) deadCodeGoPolicyContext {
	return codemodel.NewDeadCodeGoPolicyContext(result, entity)
}

// filterResultsByDecoratorExclusions forwards to the leaf-owned decorator
// filter so the staying scan keeps its call site unchanged. Delete with
// code_dead_code_scan.go's reader move.
func filterResultsByDecoratorExclusions(
	results []map[string]any,
	excluded []string,
) []map[string]any {
	return codemodel.FilterResultsByDecoratorExclusions(results, excluded)
}

// resultMatchesDecoratorExclusion forwards to the leaf-owned matcher so
// the staying investigation keeps its call site unchanged. Delete with
// code_dead_code.go's handler move.
func resultMatchesDecoratorExclusion(metadata map[string]any, excluded []string) bool {
	return codemodel.ResultMatchesDecoratorExclusion(metadata, excluded)
}

// normalizeDecoratorName forwards to the leaf-owned normalizer so the
// staying investigation keeps its call site unchanged. Delete with
// code_dead_code.go's handler move.
func normalizeDecoratorName(value string) string {
	return codemodel.NormalizeDecoratorName(value)
}

// deadCodePolicyStats aliases the leaf-owned scan counters under their old
// name so the staying orchestrator keeps its signatures unchanged. It is
// the same alias as DeadCodePolicyStats above. Delete with
// code_dead_code.go's handler move.
type deadCodePolicyStats = codemodel.DeadCodePolicyStats
