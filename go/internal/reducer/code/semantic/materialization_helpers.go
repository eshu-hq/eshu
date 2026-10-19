// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantic

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projfailure "github.com/eshu-hq/eshu/go/internal/projector/failure"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// truncateSemanticUIDsForReport bounds an unmatched-uid list for logs and
// error messages. Shared by the WARN site and the failure constructor so the
// two signals always name the same prefix with the same overflow count.
func truncateSemanticUIDsForReport(uids []string) (shown []string, truncated int) {
	shown = uids
	if len(uids) > maxSemanticUnmatchedUIDsLogged {
		shown = uids[:maxSemanticUnmatchedUIDsLogged]
		truncated = len(uids) - maxSemanticUnmatchedUIDsLogged
	}
	return shown, truncated
}

// semanticEntityIdentityDivergentError is the non-retryable work-item failure
// for admitted rows that matched no canonical node identity (#8587). It does
// not implement Retryable(): reducercontract.IsRetryable defaults to false
// for errors that do not opt in, so the queue dead-letters the item once
// instead of retry-looping. That is the poison-pill guard: an
// identity-divergent uid can never heal on retry — the projector provably
// never derives it — so any retry is pure waste. FailureClass reports the
// projection_bug triage class so the telemetry failure_class attribute agrees
// with the durable dead-letter row (ClassifyFailure defaults plain errors to
// the same bucket) and the drain keeps refusing the row as manual-review
// poison. The message names the bounded uid list and the matched count so an
// operator can see the matched rows already landed and only the named uids
// need a derivation fix.
type semanticEntityIdentityDivergentError struct {
	message string
}

func (e *semanticEntityIdentityDivergentError) Error() string { return e.message }

func (e *semanticEntityIdentityDivergentError) FailureClass() string {
	return string(projfailure.TriageClassProjectionBug)
}

// Retryable reports that an identity-divergent uid is terminal: the
// projector provably never derives it, so replaying the item unchanged can
// never succeed and it must dead-letter, not re-enter the queue. Explicit
// rather than relying on the IsRetryable default, matching FactDecodeError,
// so the poison-pill reasoning stays visible on the type if retry semantics
// ever change.
func (e *semanticEntityIdentityDivergentError) Retryable() bool {
	return false
}

func newSemanticEntityIdentityDivergentError(writeResult EntityWriteResult) error {
	uids := writeResult.UnmatchedEntityIDs
	shown, truncated := truncateSemanticUIDsForReport(uids)
	return &semanticEntityIdentityDivergentError{
		message: fmt.Sprintf(
			"semantic entity rows matched no canonical node identity: %d unmatched (uids=%v truncated=%d, %d matched already landed)",
			len(uids), shown, truncated, countSemanticMatchedRows(writeResult.RowsMatchedByLabel),
		),
	}
}

func collectSemanticMetadata(payload map[string]any) map[string]any {
	metadata := cloneAnyMap(payloadcore.PayloadMap(payload, "entity_metadata"))
	if metadata == nil {
		metadata = make(map[string]any)
	}
	for _, key := range []string{"kind", "target_kind", "type", "trait", "target", "impl_context"} {
		if value := semanticPayloadMetadataString(payload, key); value != "" {
			metadata[key] = value
		}
	}
	for _, key := range []string{"docstring", "method_kind", "semantic_kind", "module_kind", "declaration_merge_group", "annotation_kind", "context", "enclosing_function"} {
		if value := semanticPayloadMetadataString(payload, key); value != "" {
			metadata[key] = value
		}
	}
	if classContext := semanticPayloadMetadataString(payload, "class_context"); classContext != "" {
		metadata["class_context"] = classContext
	}
	if jsxFragment := semanticPayloadMetadataBool(payload, "jsx_fragment_shorthand"); jsxFragment {
		metadata["jsx_fragment_shorthand"] = true
	}
	if componentAssertion := semanticPayloadMetadataString(payload, "component_type_assertion"); componentAssertion != "" {
		metadata["component_type_assertion"] = componentAssertion
	}
	if componentWrapper := semanticPayloadMetadataString(payload, "component_wrapper_kind"); componentWrapper != "" {
		metadata["component_wrapper_kind"] = componentWrapper
	}
	for _, key := range []string{"attribute_kind", "value"} {
		if value := semanticPayloadMetadataString(payload, key); value != "" {
			metadata[key] = value
		}
	}
	if count := semanticPayloadMetadataInt(payload, "declaration_merge_count"); count > 0 {
		metadata["declaration_merge_count"] = count
	}
	if count := semanticPayloadParameterCount(payload); count > 0 {
		metadata["parameter_count"] = count
	}
	if kinds := semanticPayloadMetadataStringSlice(payload, "declaration_merge_kinds"); len(kinds) > 0 {
		metadata["declaration_merge_kinds"] = kinds
	}
	if decorators := semanticPayloadMetadataStringSlice(payload, "decorators"); len(decorators) > 0 {
		metadata["decorators"] = decorators
	}
	if typeParameters := semanticPayloadMetadataStringSlice(payload, "type_parameters"); len(typeParameters) > 0 {
		metadata["type_parameters"] = typeParameters
	}
	if async := semanticPayloadMetadataBool(payload, "async"); async {
		metadata["async"] = true
	}
	if count, kinds := semanticPayloadTypeAnnotationSummary(payload); count > 0 {
		metadata["type_annotation_count"] = count
		if len(kinds) > 0 {
			metadata["type_annotation_kinds"] = kinds
		}
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

// semanticPayloadMetadataString forwards to
// [payloadcore.SemanticPayloadMetadataString].
func semanticPayloadMetadataString(payload map[string]any, key string) string {
	return payloadcore.SemanticPayloadMetadataString(payload, key)
}

// semanticPayloadMetadataStringSlice forwards to
// [payloadcore.SemanticPayloadMetadataStringSlice].
func semanticPayloadMetadataStringSlice(payload map[string]any, key string) []string {
	return payloadcore.SemanticPayloadMetadataStringSlice(payload, key)
}

func semanticPayloadMetadataBool(payload map[string]any, key string) bool {
	if value, ok := payload[key]; ok {
		if typed, ok := value.(bool); ok {
			return typed
		}
	}
	metadata := payloadcore.PayloadMap(payload, "entity_metadata")
	if metadata == nil {
		return false
	}
	value, ok := metadata[key]
	if !ok {
		return false
	}
	typed, ok := value.(bool)
	return ok && typed
}

func semanticPayloadTypeAnnotationSummary(payload map[string]any) (int, []string) {
	raw := payload["type_annotations"]
	switch typed := raw.(type) {
	case []map[string]any:
		return typeAnnotationSummaryFromMaps(typed)
	case []any:
		annotations := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			annotation, ok := item.(map[string]any)
			if !ok {
				continue
			}
			annotations = append(annotations, annotation)
		}
		return typeAnnotationSummaryFromMaps(annotations)
	default:
		return 0, nil
	}
}

func typeAnnotationSummaryFromMaps(annotations []map[string]any) (int, []string) {
	if len(annotations) == 0 {
		return 0, nil
	}

	kinds := make([]string, 0, len(annotations))
	for _, annotation := range annotations {
		if kind, _ := annotation["annotation_kind"].(string); strings.TrimSpace(kind) != "" {
			kinds = append(kinds, strings.TrimSpace(kind))
		}
	}
	if len(kinds) == 0 {
		return len(annotations), nil
	}
	return len(annotations), kinds
}

func semanticPayloadMetadataInt(payload map[string]any, key string) int {
	if value, ok := payload[key]; ok {
		switch typed := value.(type) {
		case int:
			return typed
		case int32:
			return int(typed)
		case int64:
			return int(typed)
		case float64:
			return int(typed)
		}
	}
	metadata := payloadcore.PayloadMap(payload, "entity_metadata")
	if metadata == nil {
		return 0
	}
	value, ok := metadata[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func semanticPayloadParameterCount(payload map[string]any) int {
	if count := semanticPayloadMetadataInt(payload, "parameter_count"); count > 0 {
		return count
	}
	for _, key := range []string{"args", "parameters"} {
		if values := semanticPayloadMetadataStringSlice(payload, key); len(values) > 0 {
			return len(values)
		}
	}
	return 0
}

func isSemanticEntityType(payload map[string]any, entityType string) bool {
	switch entityType {
	case "Annotation", "Typedef", "TypeAlias", "TypeAnnotation", "Component", "Module", "ImplBlock", "Protocol", "ProtocolImplementation":
		return true
	case "Variable":
		return isElixirModuleAttributeSemanticEntity(payload) ||
			isTypeScriptJSXComponentTypeAssertionSemanticEntity(payload)
	case "Function":
		return isJavaScriptCallableSemanticEntity(payload) ||
			isGoSemanticFunction(payload) ||
			isPythonSemanticFunction(payload) ||
			isElixirSemanticFunction(payload) ||
			isRustSemanticFunction(payload) ||
			isTypeScriptSemanticFunction(payload) ||
			isTypeScriptJSXFragmentSemanticEntity(payload)
	default:
		return false
	}
}

func isJavaScriptCallableSemanticEntity(payload map[string]any) bool {
	if payloadcore.SemanticPayloadString(payload, "language") != "javascript" {
		return false
	}
	return semanticPayloadMetadataString(payload, "docstring") != "" || semanticPayloadMetadataString(payload, "method_kind") != ""
}

func isPythonSemanticFunction(payload map[string]any) bool {
	if payloadcore.SemanticPayloadString(payload, "language") != "python" {
		return false
	}
	if semanticPayloadMetadataString(payload, "semantic_kind") == "lambda" {
		return true
	}
	if semanticPayloadMetadataString(payload, "semantic_kind") == "generator" {
		return true
	}
	if semanticPayloadMetadataBool(payload, "async") {
		return true
	}
	if len(semanticPayloadMetadataStringSlice(payload, "decorators")) > 0 {
		return true
	}
	count, kinds := semanticPayloadTypeAnnotationSummary(payload)
	return count > 0 || len(kinds) > 0
}

func isGoSemanticFunction(payload map[string]any) bool {
	if payloadcore.SemanticPayloadString(payload, "language") != "go" {
		return false
	}
	return hasSemanticFunctionMetadata(payload)
}

func isElixirSemanticFunction(payload map[string]any) bool {
	if payloadcore.SemanticPayloadString(payload, "language") != "elixir" {
		return false
	}
	return semanticPayloadMetadataString(payload, "semantic_kind") == "guard"
}

func isElixirModuleAttributeSemanticEntity(payload map[string]any) bool {
	if payloadcore.SemanticPayloadString(payload, "language") != "elixir" {
		return false
	}
	return semanticPayloadMetadataString(payload, "attribute_kind") == "module_attribute"
}

func isTypeScriptSemanticFunction(payload map[string]any) bool {
	if payloadcore.SemanticPayloadString(payload, "language") != "typescript" {
		return false
	}
	return len(semanticPayloadMetadataStringSlice(payload, "decorators")) > 0 ||
		len(semanticPayloadMetadataStringSlice(payload, "type_parameters")) > 0
}

func isTypeScriptJSXFragmentSemanticEntity(payload map[string]any) bool {
	if payloadcore.SemanticPayloadString(payload, "language") != "tsx" {
		return false
	}
	return semanticPayloadMetadataBool(payload, "jsx_fragment_shorthand")
}

func isTypeScriptJSXComponentTypeAssertionSemanticEntity(payload map[string]any) bool {
	if payloadcore.SemanticPayloadString(payload, "language") != "tsx" {
		return false
	}
	return semanticPayloadMetadataString(payload, "component_type_assertion") != ""
}

func isRustSemanticFunction(payload map[string]any) bool {
	if payloadcore.SemanticPayloadString(payload, "language") != "rust" {
		return false
	}
	return semanticPayloadMetadataString(payload, "impl_context") != "" ||
		semanticPayloadMetadataString(payload, "trait") != "" ||
		semanticPayloadMetadataString(payload, "target") != ""
}

func hasSemanticFunctionMetadata(payload map[string]any) bool {
	for _, key := range []string{
		"docstring",
		"class_context",
		"method_kind",
		"constructor_kind",
		"annotation_kind",
		"context",
		"enclosing_function",
		"impl_context",
	} {
		if semanticPayloadMetadataString(payload, key) != "" {
			return true
		}
	}
	if len(semanticPayloadMetadataStringSlice(payload, "decorators")) > 0 {
		return true
	}
	if len(semanticPayloadMetadataStringSlice(payload, "type_parameters")) > 0 {
		return true
	}
	if semanticPayloadMetadataBool(payload, "async") ||
		semanticPayloadMetadataBool(payload, "jsx_fragment_shorthand") {
		return true
	}
	count, kinds := semanticPayloadTypeAnnotationSummary(payload)
	return count > 0 || len(kinds) > 0
}

func semanticPayloadInt(payload map[string]any, key string) int {
	if payload == nil {
		return 0
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func cloneAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func collectSemanticRepoIDs(envelopes []facts.Envelope) []string {
	seen := make(map[string]struct{})
	repoIDs := make([]string, 0)
	for _, env := range envelopes {
		repoID := payloadcore.SemanticPayloadString(env.Payload, "repo_id")
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		repoIDs = append(repoIDs, repoID)
	}

	sort.Strings(repoIDs)
	return repoIDs
}

// maxSemanticUnmatchedUIDsLogged bounds the uid list on the unmatched-uid
// WARN (#8359): the count is always exact, the names are a prefix sample so
// one pathological generation cannot flood the log.
const maxSemanticUnmatchedUIDsLogged = 10

// countSemanticMatchedRows totals per-label matched counts for the status
// summary. A nil map totals zero; writers that cannot verify leave the map
// empty rather than claiming matches they did not check.
func countSemanticMatchedRows(matchedByLabel map[string]int) int {
	total := 0
	for _, matched := range matchedByLabel {
		total += matched
	}
	return total
}

// logSemanticEntityUnmatchedUIDs WARNs on admitted rows that matched no
// canonical node identity (#8359). The WARN stays as the operator-facing
// signal; Handle additionally fails the work item (#8587) because unmatched
// uids are deterministic, never races — see semanticEntityIdentityDivergentError.

func logSemanticEntityUnmatchedUIDs(
	ctx context.Context,
	intent reducercontract.Intent,
	writeResult EntityWriteResult,
) {
	uids := writeResult.UnmatchedEntityIDs
	shown, truncated := truncateSemanticUIDsForReport(uids)
	slog.WarnContext(
		ctx, "semantic entity rows matched no canonical node",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.Domain(string(intent.Domain)),
		slog.Int("unmatched_uid_count", len(uids)),
		slog.Any("unmatched_uids", shown),
		slog.Int("unmatched_uids_truncated", truncated),
		slog.Any("rows_attempted_by_label", writeResult.RowsAttemptedByLabel),
		slog.Any("rows_matched_by_label", writeResult.RowsMatchedByLabel),
	)
}
