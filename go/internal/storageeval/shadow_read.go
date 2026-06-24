// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storageeval

import (
	"fmt"
	"strings"
	"time"
)

// ReadModel names a read-model family included in the shadow-read proof gate.
type ReadModel string

const (
	// ReadModelRepositoryFile compares repository file content read models.
	ReadModelRepositoryFile ReadModel = "repository_file"
	// ReadModelContentEntity compares indexed content entity read models.
	ReadModelContentEntity ReadModel = "content_entity"
	// ReadModelStructuralInventory compares code structural inventory pages.
	ReadModelStructuralInventory ReadModel = "structural_inventory"
	// ReadModelSearchDocument compares curated Eshu search documents.
	ReadModelSearchDocument ReadModel = "search_document"
	// ReadModelRepositoryContext compares repository story and context models.
	ReadModelRepositoryContext ReadModel = "repository_context"
	// ReadModelRelationshipEvidence compares relationship evidence drilldowns.
	ReadModelRelationshipEvidence ReadModel = "relationship_evidence"
)

// ScopeKind identifies the smallest bounded comparison scope.
type ScopeKind string

const (
	// ScopeRepository bounds comparison to one repository.
	ScopeRepository ScopeKind = "repository"
	// ScopeFile bounds comparison to one repository file.
	ScopeFile ScopeKind = "file"
	// ScopeEntity bounds comparison to one content or graph entity.
	ScopeEntity ScopeKind = "entity"
	// ScopeRelationship bounds comparison to one relationship evidence target.
	ScopeRelationship ScopeKind = "relationship"
	// ScopeDocument bounds comparison to one curated search document.
	ScopeDocument ScopeKind = "document"
)

// Scope records the bounded target for one comparison.
type Scope struct {
	Kind ScopeKind `json:"kind"`
	ID   string    `json:"id"`
}

// Backend identifies the producer of one compared result.
type Backend string

const (
	// BackendPostgresReadModel is the current production Postgres baseline.
	BackendPostgresReadModel Backend = "postgres_read_model"
	// BackendNornicDBShadowReadModel is the candidate NornicDB shadow reader.
	BackendNornicDBShadowReadModel Backend = "nornicdb_shadow_read_model"
)

// TruthLevel records the authority of a compared read result.
type TruthLevel string

const (
	// TruthLevelExact is authoritative durable truth for the scoped capability.
	TruthLevelExact TruthLevel = "exact"
	// TruthLevelDerived is deterministic truth from indexed state or read models.
	TruthLevelDerived TruthLevel = "derived"
	// TruthLevelFallback is an exploratory fallback and is not accepted here.
	TruthLevelFallback TruthLevel = "fallback"
)

// TruthBasis records the evidence family behind the truth level.
type TruthBasis string

const (
	// TruthBasisAuthoritativeGraph comes from canonical graph truth.
	TruthBasisAuthoritativeGraph TruthBasis = "authoritative_graph"
	// TruthBasisSemanticFacts comes from durable semantic fact rows.
	TruthBasisSemanticFacts TruthBasis = "semantic_facts"
	// TruthBasisContentIndex comes from indexed content rows.
	TruthBasisContentIndex TruthBasis = "content_index"
	// TruthBasisReadModel comes from a reducer or query read model.
	TruthBasisReadModel TruthBasis = "read_model"
	// TruthBasisSearchDocument comes from curated Eshu search documents.
	TruthBasisSearchDocument TruthBasis = "search_document"
	// TruthBasisHybrid combines graph, fact, content, or read-model evidence.
	TruthBasisHybrid TruthBasis = "hybrid"
)

// TruthLabel is the per-result truth label that must match across backends.
type TruthLabel struct {
	Level TruthLevel `json:"level"`
	Basis TruthBasis `json:"basis"`
}

// FreshnessState records whether a compared result is current enough to prove.
type FreshnessState string

const (
	// FreshnessFresh means the result is current for the compared scope.
	FreshnessFresh FreshnessState = "fresh"
	// FreshnessStale means the result is known to lag the compared scope.
	FreshnessStale FreshnessState = "stale"
	// FreshnessBuilding means the compared read model is still being built.
	FreshnessBuilding FreshnessState = "building"
	// FreshnessUnavailable means freshness could not be established.
	FreshnessUnavailable FreshnessState = "unavailable"
)

// Freshness records freshness state and observation time for one result.
type Freshness struct {
	State      FreshnessState `json:"state"`
	ObservedAt time.Time      `json:"observed_at"`
}

// ReadResult is one bounded read-model output summarized for comparison.
type ReadResult struct {
	Backend    Backend       `json:"backend"`
	Digest     string        `json:"digest"`
	TruthLabel TruthLabel    `json:"truth_label"`
	Freshness  Freshness     `json:"freshness"`
	Latency    time.Duration `json:"latency_ns"`
	Truncated  bool          `json:"truncated"`
	Supported  bool          `json:"supported"`
}

// Verdict is the comparison outcome recorded by the shadow-read proof.
type Verdict string

const (
	// VerdictMatch means the shadow result matched the Postgres baseline.
	VerdictMatch Verdict = "match"
	// VerdictDivergent means the two compared results disagree.
	VerdictDivergent Verdict = "divergent"
	// VerdictMissingShadow means the NornicDB shadow result was absent.
	VerdictMissingShadow Verdict = "missing_shadow"
	// VerdictStaleShadow means the NornicDB shadow result was stale.
	VerdictStaleShadow Verdict = "stale_shadow"
	// VerdictUnsupportedCapability means the shadow backend could not answer.
	VerdictUnsupportedCapability Verdict = "unsupported_capability"
	// VerdictTruncated means at least one side returned partial evidence.
	VerdictTruncated Verdict = "truncated"
)

// FallbackBehavior records what production would do when shadow proof fails.
type FallbackBehavior string

const (
	// FallbackKeepPostgres keeps Postgres as the production owner.
	FallbackKeepPostgres FallbackBehavior = "keep_postgres"
	// FallbackFailClosed refuses a promoted read rather than returning drift.
	FallbackFailClosed FallbackBehavior = "fail_closed"
	// FallbackReturnUnsupported returns unsupported_capability to callers.
	FallbackReturnUnsupported FallbackBehavior = "return_unsupported_capability"
)

// FailureClass identifies the operator-visible reason a comparison failed.
type FailureClass string

const (
	// FailureClassNone means no failure was observed.
	FailureClassNone FailureClass = "none"
	// FailureClassMissingShadow records absent shadow read output.
	FailureClassMissingShadow FailureClass = "missing_shadow"
	// FailureClassStaleShadow records stale shadow read output.
	FailureClassStaleShadow FailureClass = "stale_shadow"
	// FailureClassDivergent records a baseline/shadow mismatch.
	FailureClassDivergent FailureClass = "divergent"
	// FailureClassTruncated records partial comparison evidence.
	FailureClassTruncated FailureClass = "truncated"
	// FailureClassUnsupportedCapability records unsupported shadow behavior.
	FailureClassUnsupportedCapability FailureClass = "unsupported_capability"
)

// ShadowReadComparison is one evidence record for the #1287 proof gate.
type ShadowReadComparison struct {
	ReadModel        ReadModel        `json:"read_model"`
	Capability       string           `json:"capability"`
	Scope            Scope            `json:"scope"`
	Limit            int              `json:"limit"`
	Baseline         ReadResult       `json:"baseline"`
	Shadow           ReadResult       `json:"shadow"`
	Verdict          Verdict          `json:"verdict"`
	FallbackBehavior FallbackBehavior `json:"fallback_behavior"`
	FailureClass     FailureClass     `json:"failure_class"`
}

// ValidateShadowReadComparison verifies one passing shadow-read proof record.
func ValidateShadowReadComparison(comparison ShadowReadComparison) error {
	if !supportedReadModel(comparison.ReadModel) {
		return fmt.Errorf("unsupported read model %q", comparison.ReadModel)
	}
	if strings.TrimSpace(comparison.Capability) == "" {
		return fmt.Errorf("capability is required")
	}
	if strings.TrimSpace(string(comparison.Scope.Kind)) == "" {
		return fmt.Errorf("scope kind is required")
	}
	if !supportedScopeKind(comparison.Scope.Kind) {
		return fmt.Errorf("unsupported scope kind %q", comparison.Scope.Kind)
	}
	if strings.TrimSpace(comparison.Scope.ID) == "" {
		return fmt.Errorf("scope id is required")
	}
	if comparison.Limit <= 0 {
		return fmt.Errorf("limit must be positive")
	}
	if err := validateReadResult("baseline", comparison.Baseline, BackendPostgresReadModel); err != nil {
		return err
	}
	if err := validateReadResult("shadow", comparison.Shadow, BackendNornicDBShadowReadModel); err != nil {
		return err
	}
	if comparison.Baseline.TruthLabel != comparison.Shadow.TruthLabel {
		return fmt.Errorf("truth labels must match")
	}
	if comparison.Baseline.Digest != comparison.Shadow.Digest {
		return fmt.Errorf("shadow digest differs from baseline")
	}
	if comparison.Verdict == "" {
		return fmt.Errorf("verdict is required")
	}
	if comparison.Verdict != VerdictMatch {
		return fmt.Errorf("verdict must be match")
	}
	if err := validateFallbackBehavior(comparison.FallbackBehavior); err != nil {
		return err
	}
	if comparison.FailureClass == "" {
		return fmt.Errorf("failure class is required")
	}
	if comparison.FailureClass != FailureClassNone {
		return fmt.Errorf("failure class must be none for match verdict")
	}
	return nil
}

func validateReadResult(role string, result ReadResult, backend Backend) error {
	if result.Backend != backend {
		return fmt.Errorf("%s backend must be %q", role, backend)
	}
	if strings.TrimSpace(result.Digest) == "" {
		return fmt.Errorf("%s result digest is required", role)
	}
	if err := validateTruthLabel(role, result.TruthLabel); err != nil {
		return err
	}
	if err := validateFreshness(role, result.Freshness); err != nil {
		return err
	}
	if result.Latency < 0 {
		return fmt.Errorf("%s latency must not be negative", role)
	}
	if result.Truncated {
		return fmt.Errorf("%s result must not be truncated", role)
	}
	if !result.Supported {
		return fmt.Errorf("%s capability is unsupported", role)
	}
	return nil
}

func validateTruthLabel(role string, label TruthLabel) error {
	if label.Level == "" || label.Basis == "" {
		return fmt.Errorf("%s truth label is required", role)
	}
	switch label.Level {
	case TruthLevelExact, TruthLevelDerived:
	case TruthLevelFallback:
		return fmt.Errorf("%s truth level fallback is not accepted", role)
	default:
		return fmt.Errorf("%s truth level %q is unsupported", role, label.Level)
	}
	switch label.Basis {
	case TruthBasisAuthoritativeGraph, TruthBasisSemanticFacts, TruthBasisContentIndex,
		TruthBasisReadModel, TruthBasisSearchDocument, TruthBasisHybrid:
	default:
		return fmt.Errorf("%s truth basis %q is unsupported", role, label.Basis)
	}
	return nil
}

func validateFreshness(role string, freshness Freshness) error {
	if freshness.State == "" {
		return fmt.Errorf("%s freshness is required", role)
	}
	if freshness.State != FreshnessFresh {
		return fmt.Errorf("%s freshness must be fresh", role)
	}
	if freshness.ObservedAt.IsZero() {
		return fmt.Errorf("%s observed_at is required", role)
	}
	return nil
}

func validateFallbackBehavior(fallback FallbackBehavior) error {
	switch fallback {
	case "":
		return fmt.Errorf("fallback behavior is required")
	case FallbackKeepPostgres, FallbackFailClosed, FallbackReturnUnsupported:
		return nil
	default:
		return fmt.Errorf("fallback behavior %q is unsupported", fallback)
	}
}

func supportedReadModel(readModel ReadModel) bool {
	switch readModel {
	case ReadModelRepositoryFile, ReadModelContentEntity, ReadModelStructuralInventory,
		ReadModelSearchDocument, ReadModelRepositoryContext, ReadModelRelationshipEvidence:
		return true
	default:
		return false
	}
}

func supportedScopeKind(scopeKind ScopeKind) bool {
	switch scopeKind {
	case ScopeRepository, ScopeFile, ScopeEntity, ScopeRelationship, ScopeDocument:
		return true
	default:
		return false
	}
}
