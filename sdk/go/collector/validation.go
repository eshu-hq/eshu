// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	factKindPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*[a-z0-9]$|^[a-z0-9]$`)
	semverPattern         = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	sensitiveQueryPattern = regexp.MustCompile(`(?i)(token|secret|password|credential|api[_-]?key|authorization)`)
)

// redactionSafePayloadKeys allowlists exact payload field names that trip
// sensitiveQueryPattern's substring heuristic but are not, in fact, raw
// credentials: they are redaction-safe join-key fingerprints a collector
// emits by design to correlate facts (for example a hash of a Vault ACL
// policy name), never the secret value itself. The allowlist is keyed by
// EXACT field name, not a broader pattern, so it cannot silently widen the
// heuristic to cover an actually-sensitive field that happens to share a
// name; each entry is a deliberate, reviewed exception, not a generic escape
// hatch. Add an entry here only when the field's own type documents why it
// is a fingerprint/reference rather than a credential (see
// secretsiam/v1.VaultAuthRole.TokenPolicyJoinKeys for the motivating case).
var redactionSafePayloadKeys = map[string]struct{}{
	"token_policy_join_keys": {},
}

// Validator validates extension results against the host-declared contract.
type Validator struct {
	protocolVersion string
	facts           map[string]factDeclaration
}

type factDeclaration struct {
	schemaVersions   map[string]struct{}
	sourceConfidence map[SourceConfidence]struct{}
	tombstoneAllowed bool
}

// NewValidator creates a validator for one host-supported collector contract.
func NewValidator(contract Contract) Validator {
	version := strings.TrimSpace(contract.ProtocolVersion)
	if version == "" {
		version = ProtocolVersionV1Alpha1
	}
	validator := Validator{
		protocolVersion: version,
		facts:           make(map[string]factDeclaration, len(contract.Facts)),
	}
	for _, declared := range contract.Facts {
		kind := strings.TrimSpace(declared.Kind)
		if kind == "" {
			continue
		}
		spec := factDeclaration{
			schemaVersions:   make(map[string]struct{}, len(declared.SchemaVersions)),
			sourceConfidence: make(map[SourceConfidence]struct{}, len(declared.SourceConfidence)),
			tombstoneAllowed: declared.TombstoneAllowed,
		}
		for _, version := range declared.SchemaVersions {
			spec.schemaVersions[strings.TrimSpace(version)] = struct{}{}
		}
		for _, confidence := range declared.SourceConfidence {
			spec.sourceConfidence[confidence] = struct{}{}
		}
		validator.facts[kind] = spec
	}
	return validator
}

// ValidateResult validates a complete extension result before host commit.
func (v Validator) ValidateResult(result Result) (ValidationReport, error) {
	var report ValidationReport
	if err := v.validateResultEnvelope(result); err != nil {
		return report, err
	}
	for _, status := range result.Statuses {
		if err := validateStatus(result.State, status); err != nil {
			return report, err
		}
		report.StatusCount++
	}
	if err := validateStateFactAllowance(result.State, len(result.Facts)); err != nil {
		return report, err
	}

	seen := make(map[string]Fact, len(result.Facts))
	for _, fact := range result.Facts {
		if err := v.validateFact(result, fact); err != nil {
			return report, err
		}
		key := fact.Kind + "\x00" + fact.StableKey
		if existing, ok := seen[key]; ok {
			if !equalFact(existing, fact) {
				return report, fmt.Errorf("fact %q has conflicting duplicate stable key %q", fact.Kind, fact.StableKey)
			}
			report.DuplicateCount++
			continue
		}
		seen[key] = fact
		report.FactCount++
		if fact.Tombstone {
			report.TombstoneCount++
		}
		report.RedactionCount += len(fact.Redactions)
	}
	if err := validateStateStatuses(result.State, result.Statuses); err != nil {
		return report, err
	}
	return report, nil
}

func (v Validator) validateResultEnvelope(result Result) error {
	if strings.TrimSpace(result.ProtocolVersion) != v.protocolVersion {
		return fmt.Errorf("protocol_version %q is unsupported", result.ProtocolVersion)
	}
	if !validResultState(result.State) {
		return fmt.Errorf("state %q is unsupported", result.State)
	}
	if err := validateClaim(result.Claim); err != nil {
		return err
	}
	if strings.TrimSpace(result.Generation.ID) == "" {
		return fmt.Errorf("generation.id is required")
	}
	if result.Generation.ObservedAt.IsZero() {
		return fmt.Errorf("generation.observed_at is required")
	}
	if result.Generation.ID != result.Claim.GenerationID {
		return fmt.Errorf("generation.id %q does not match claim generation %q", result.Generation.ID, result.Claim.GenerationID)
	}
	return nil
}

func validateClaim(claim Claim) error {
	required := map[string]string{
		"claim.component_id":   claim.ComponentID,
		"claim.instance_id":    claim.InstanceID,
		"claim.collector_kind": claim.CollectorKind,
		"claim.source_system":  claim.SourceSystem,
		"claim.scope.id":       claim.Scope.ID,
		"claim.scope.kind":     claim.Scope.Kind,
		"claim.source_run_id":  claim.SourceRunID,
		"claim.generation_id":  claim.GenerationID,
		"claim.work_item_id":   claim.WorkItemID,
		"claim.fencing_token":  claim.FencingToken,
		"claim.config_handle":  claim.ConfigHandle,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", field)
		}
	}
	if claim.Attempt < 1 {
		return fmt.Errorf("claim.attempt must be >= 1")
	}
	if claim.Deadline.IsZero() {
		return fmt.Errorf("claim.deadline is required")
	}
	return nil
}

func (v Validator) validateFact(result Result, fact Fact) error {
	if !factKindPattern.MatchString(strings.TrimSpace(fact.Kind)) {
		return fmt.Errorf("fact.kind %q is invalid", fact.Kind)
	}
	spec, ok := v.facts[fact.Kind]
	if !ok {
		return fmt.Errorf("fact.kind %q is not declared by contract", fact.Kind)
	}
	if !semverPattern.MatchString(strings.TrimSpace(fact.SchemaVersion)) {
		return fmt.Errorf("fact.schema_version %q must be semantic version", fact.SchemaVersion)
	}
	if _, ok := spec.schemaVersions[fact.SchemaVersion]; !ok {
		return fmt.Errorf("fact %q schema_version %q is not declared", fact.Kind, fact.SchemaVersion)
	}
	if _, ok := spec.sourceConfidence[fact.SourceConfidence]; !ok || fact.SourceConfidence == SourceConfidenceUnknown {
		return fmt.Errorf("fact %q source_confidence %q is not declared", fact.Kind, fact.SourceConfidence)
	}
	if strings.TrimSpace(fact.StableKey) == "" {
		return fmt.Errorf("fact %q stable_key is required", fact.Kind)
	}
	if strings.ContainsAny(fact.StableKey, "\n\r\t") {
		return fmt.Errorf("fact %q stable_key contains whitespace control characters", fact.Kind)
	}
	if fact.ObservedAt.IsZero() {
		return fmt.Errorf("fact %q observed_at is required", fact.Kind)
	}
	if fact.Tombstone && !spec.tombstoneAllowed {
		return fmt.Errorf("fact %q does not allow tombstones", fact.Kind)
	}
	if err := validateSourceRef(result, fact); err != nil {
		return err
	}
	if len(fact.Payload) == 0 {
		return fmt.Errorf("fact %q payload is required", fact.Kind)
	}
	if err := validatePayload(fact.Payload); err != nil {
		return fmt.Errorf("fact %q payload: %w", fact.Kind, err)
	}
	for _, redaction := range fact.Redactions {
		if strings.TrimSpace(redaction.Field) == "" {
			return fmt.Errorf("fact %q redaction field is required", fact.Kind)
		}
		if strings.TrimSpace(redaction.Reason) == "" {
			return fmt.Errorf("fact %q redaction reason is required", fact.Kind)
		}
	}
	return nil
}

func validateSourceRef(result Result, fact Fact) error {
	ref := fact.SourceRef
	required := map[string]string{
		"source_ref.source_system": ref.SourceSystem,
		"source_ref.scope_id":      ref.ScopeID,
		"source_ref.generation_id": ref.GenerationID,
		"source_ref.fact_key":      ref.FactKey,
		"source_ref.uri":           ref.URI,
		"source_ref.record_id":     ref.RecordID,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("fact %q %s is required", fact.Kind, field)
		}
	}
	if ref.SourceSystem != result.Claim.SourceSystem {
		return fmt.Errorf("fact %q source_ref.source_system %q does not match claim source_system %q", fact.Kind, ref.SourceSystem, result.Claim.SourceSystem)
	}
	if ref.ScopeID != result.Claim.Scope.ID {
		return fmt.Errorf("fact %q source_ref.scope_id %q does not match claim scope %q", fact.Kind, ref.ScopeID, result.Claim.Scope.ID)
	}
	if ref.GenerationID != result.Generation.ID {
		return fmt.Errorf("fact %q source_ref.generation_id %q does not match generation %q", fact.Kind, ref.GenerationID, result.Generation.ID)
	}
	if ref.FactKey != fact.StableKey {
		return fmt.Errorf("fact %q source_ref.fact_key %q does not match stable_key %q", fact.Kind, ref.FactKey, fact.StableKey)
	}
	return validateSourceURI(ref.URI)
}

func validateSourceURI(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(strings.ToLower(trimmed), "file:") {
		return fmt.Errorf("source_ref.uri must not contain host-local file paths")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("source_ref.uri is invalid: %w", err)
	}
	if parsed.User != nil {
		return fmt.Errorf("source_ref.uri must not contain credentials")
	}
	for key := range parsed.Query() {
		if sensitiveQueryPattern.MatchString(key) {
			return fmt.Errorf("source_ref.uri query key %q is not allowed", key)
		}
	}
	return nil
}

func validatePayload(payload map[string]any) error {
	if _, err := json.Marshal(payload); err != nil {
		return fmt.Errorf("must be JSON serializable: %w", err)
	}
	return validatePayloadKeys("", payload)
}

func validatePayloadKeys(prefix string, value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			if _, safe := redactionSafePayloadKeys[key]; !safe && sensitiveQueryPattern.MatchString(key) {
				return fmt.Errorf("sensitive-looking key %q must be redacted before emission", path)
			}
			if err := validatePayloadKeys(path, child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := validatePayloadKeys(prefix, child); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateStatus(state ResultState, status Status) error {
	if !validStatusClass(status.Class) {
		return fmt.Errorf("status.class %q is unsupported", status.Class)
	}
	if status.Class == StatusFailure && strings.TrimSpace(status.FailureClass) == "" {
		return fmt.Errorf("status.failure_class is required for failure status")
	}
	if status.RetryAfterSeconds < 0 {
		return fmt.Errorf("status.retry_after_seconds must be >= 0")
	}
	if status.WarningCount < 0 || status.FactCount < 0 || status.SourceLatencyMS < 0 {
		return fmt.Errorf("status counts and latency must be >= 0")
	}
	if state == ResultRetryable && status.Class == StatusFailure && status.RetryAfterSeconds == 0 {
		return fmt.Errorf("retryable status requires retry_after_seconds")
	}
	return nil
}

func validateStateFactAllowance(state ResultState, factCount int) error {
	switch state {
	case ResultUnchanged, ResultRetryable, ResultTerminal:
		if factCount > 0 {
			return fmt.Errorf("%s result must not include facts", state)
		}
	}
	return nil
}

func validateStateStatuses(state ResultState, statuses []Status) error {
	switch state {
	case ResultRetryable, ResultTerminal:
		for _, status := range statuses {
			if status.Class == StatusFailure {
				return nil
			}
		}
		return fmt.Errorf("%s result requires a failure status", state)
	case ResultPartial:
		for _, status := range statuses {
			if status.Partial || status.Class == StatusWarning {
				return nil
			}
		}
		return fmt.Errorf("partial result requires a partial or warning status")
	}
	return nil
}

func validResultState(state ResultState) bool {
	switch state {
	case ResultComplete, ResultUnchanged, ResultPartial, ResultRetryable, ResultTerminal:
		return true
	default:
		return false
	}
}

func validStatusClass(class StatusClass) bool {
	switch class {
	case StatusProgress, StatusWarning, StatusFailure, StatusComplete:
		return true
	default:
		return false
	}
}

func equalFact(left, right Fact) bool {
	leftRaw, err := json.Marshal(left)
	if err != nil {
		return false
	}
	rightRaw, err := json.Marshal(right)
	if err != nil {
		return false
	}
	return string(leftRaw) == string(rightRaw)
}
