// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const inheritanceEvidenceSource = "reducer/inheritance"

// inheritableEntityTypes lists the entity types that can participate in
// inheritance relationships.
var inheritableEntityTypes = map[string]struct{}{
	"Class":     {},
	"Interface": {},
	"Struct":    {},
	"Trait":     {},
	"Protocol":  {},
	"Enum":      {},
}

var inheritanceContentEntityTypes = []string{
	"Class",
	"Interface",
	"Struct",
	"Trait",
	"Protocol",
	"Enum",
	"Function",
}

// InheritanceIntentWriter persists durable shared-projection intents for
// inheritance edge materialization (#2867). The promoted handler emits intents
// instead of writing edges directly so the #2755 partitioned runner projects
// them under file-scoped partition keys and the #2898 refresh fence owns the
// single per-repo retract.
type InheritanceIntentWriter interface {
	UpsertIntents(ctx context.Context, rows []SharedProjectionIntentRow) error
}

// InheritanceMaterializationHandler reduces one inheritance follow-up into
// durable shared-projection intent emission for INHERITS, IMPLEMENTS, OVERRIDES,
// and ALIASES edges using parser entity bases and PHP trait adaptation metadata.
// Each repository gets one whole-scope refresh intent that owns the retract, and
// each edge gets a write-only per-edge intent under a file-scoped partition key
// fenced behind that refresh (#2867).
type InheritanceMaterializationHandler struct {
	FactLoader   FactLoader
	IntentWriter InheritanceIntentWriter
}

// inheritanceMaterializationTiming records success-path stage timings for the
// inheritance_materialization reducer domain. Timing wrappers are time.Now
// diffs around existing work and add negligible overhead per intent.
type inheritanceMaterializationTiming struct {
	loadFactsDuration    time.Duration
	buildIntentsDuration time.Duration
	upsertDuration       time.Duration
	totalDuration        time.Duration
}

// Handle executes the inheritance materialization path.
func (h InheritanceMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStarted := time.Now()
	var timing inheritanceMaterializationTiming

	if intent.Domain != DomainInheritanceMaterialization {
		return Result{}, fmt.Errorf(
			"inheritance materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("inheritance materialization fact loader is required")
	}
	if h.IntentWriter == nil {
		return Result{}, fmt.Errorf("inheritance materialization intent writer is required")
	}

	slog.InfoContext(
		ctx, "inheritance materialization started",
		slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
		slog.String(telemetry.LogKeyDomain, string(intent.Domain)),
	)

	loadStarted := time.Now()
	envelopes, err := loadInheritanceMaterializationFacts(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID)
	timing.loadFactsDuration = time.Since(loadStarted)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for inheritance materialization: %w", err)
	}

	deltaScope := buildInheritanceDeltaScope(envelopes)
	repoIDs, rows := ExtractInheritanceRows(envelopes)
	repoIDs = mergeInheritanceRepositoryIDs(repoIDs, deltaScope.repositoryIDs)
	contextByRepoID := buildCodeCallProjectionContexts(envelopes, intent.GenerationID)
	if len(contextByRepoID) == 0 {
		timing.totalDuration = time.Since(totalStarted)
		// No projection context built from the loaded facts: the handler ran
		// before its upstream repository/content facts existed — an ordering
		// stall, signaled by input_ready=0.
		return Result{
			IntentID:        intent.IntentID,
			Domain:          DomainInheritanceMaterialization,
			Status:          ResultStatusSucceeded,
			EvidenceSummary: "no projection context available for inheritance materialization",
			SubDurations:    inheritanceMaterializationSubDurations(timing),
			SubSignals:      materializationDiagnosticSignals(false, 0),
		}, nil
	}
	if len(repoIDs) == 0 {
		timing.totalDuration = time.Since(totalStarted)
		// Projection context exists but the loaded facts carried no inheritance
		// content entities: the input was ready and the work was genuinely empty
		// (input_ready=1, written_rows=0), not an upstream ordering stall.
		return Result{
			IntentID:        intent.IntentID,
			Domain:          DomainInheritanceMaterialization,
			Status:          ResultStatusSucceeded,
			EvidenceSummary: "no inheritance entities for inheritance materialization",
			SubDurations:    inheritanceMaterializationSubDurations(timing),
			SubSignals:      materializationDiagnosticSignals(true, 0),
		}, nil
	}

	createdAt := intent.EnqueuedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	buildStarted := time.Now()
	intentRows := buildInheritanceSharedIntentRows(rows, deltaScope, repoIDs, contextByRepoID, createdAt)
	timing.buildIntentsDuration = time.Since(buildStarted)

	if len(intentRows) > 0 {
		upsertStarted := time.Now()
		if err := h.IntentWriter.UpsertIntents(ctx, intentRows); err != nil {
			return Result{}, fmt.Errorf("write inheritance intents: %w", err)
		}
		timing.upsertDuration = time.Since(upsertStarted)
	}
	timing.totalDuration = time.Since(totalStarted)

	slog.InfoContext(
		ctx, "inheritance materialization completed",
		slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
		slog.Int("intent_count", len(intentRows)),
		slog.Int("edge_count", len(rows)),
		slog.Int("repo_count", len(repoIDs)),
		slog.Float64("load_facts_duration_seconds", timing.loadFactsDuration.Seconds()),
		slog.Float64("build_intents_duration_seconds", timing.buildIntentsDuration.Seconds()),
		slog.Float64("upsert_intents_duration_seconds", timing.upsertDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainInheritanceMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"emitted %d durable inheritance intents across %d repositories",
			len(intentRows),
			len(repoIDs),
		),
		CanonicalWrites: len(intentRows),
		SubDurations:    inheritanceMaterializationSubDurations(timing),
		// Projection context was built (input present), so input_ready=1. The
		// refresh-intent emission always yields >=1 row per repo with context,
		// so written_rows tracks the durable intent count.
		SubSignals: materializationDiagnosticSignals(true, len(intentRows)),
	}, nil
}

// inheritanceMaterializationSubDurations converts per-phase timing into the
// Result.SubDurations map so the service layer emits sub_duration_<key>_seconds
// log attributes. Keys follow the workload_materialization convention. The
// non-duration diagnostic signals (input_ready, written_rows) are carried
// separately in Result.SubSignals so the _seconds suffix stays honest.
func inheritanceMaterializationSubDurations(t inheritanceMaterializationTiming) map[string]float64 {
	return map[string]float64{
		"load_facts":     t.loadFactsDuration.Seconds(),
		"build_intents":  t.buildIntentsDuration.Seconds(),
		"upsert_intents": t.upsertDuration.Seconds(),
		"total":          t.totalDuration.Seconds(),
	}
}

// ExtractInheritanceRows builds canonical child/parent edge rows from content
// entity facts that carry bases or trait adaptation metadata. It performs
// intra-repo name matching only; cross-repo inheritance is out of scope.
func ExtractInheritanceRows(envelopes []facts.Envelope) ([]string, []map[string]any) {
	if len(envelopes) == 0 {
		return nil, nil
	}

	repoIDs := collectInheritanceRepoIDs(envelopes)
	if len(repoIDs) == 0 {
		return nil, nil
	}

	// Build entity index: entity_name -> entity_id for intra-repo matching.
	entityIndex := buildInheritanceEntityIndex(envelopes)
	methodIndex := buildInheritanceMethodIndex(envelopes)

	seenEdges := make(map[string]struct{})
	rows := make([]map[string]any, 0)

	for _, env := range envelopes {
		if env.FactKind != "content_entity" {
			continue
		}

		entityType := semanticPayloadString(env.Payload, "entity_type")
		if _, ok := inheritableEntityTypes[entityType]; !ok {
			continue
		}

		repoID := semanticPayloadString(env.Payload, "repo_id")
		childEntityID := semanticPayloadString(env.Payload, "entity_id")
		if repoID == "" || childEntityID == "" {
			continue
		}

		bases := inheritancePayloadBases(env.Payload)
		traitAdaptations := inheritancePayloadTraitAdaptations(env.Payload)
		implementedInterfaces := inheritancePayloadImplementedInterfaces(env.Payload)
		if len(bases) == 0 && len(traitAdaptations) == 0 && len(implementedInterfaces) == 0 {
			continue
		}

		for _, baseName := range bases {
			parent, ok := entityIndex[inheritanceIndexKey{repoID: repoID, name: baseName}]
			if !ok {
				continue
			}

			edgeKey := childEntityID + "->" + parent.id
			if _, dup := seenEdges[edgeKey]; dup {
				continue
			}
			seenEdges[edgeKey] = struct{}{}

			rows = append(rows, declaredInheritanceRow(childEntityID, entityType, semanticPayloadString(env.Payload, "path"), parent, repoID, "INHERITS"))
		}

		if _, implementer := implementerEntityTypes[entityType]; implementer {
			for _, interfaceName := range implementedInterfaces {
				parent, ok := entityIndex[inheritanceIndexKey{repoID: repoID, name: interfaceName}]
				if !ok {
					continue
				}
				if _, isInterface := interfaceLikeEntityTypes[parent.entityType]; !isInterface {
					continue
				}

				edgeKey := childEntityID + "->" + parent.id + ":IMPLEMENTS"
				if _, dup := seenEdges[edgeKey]; dup {
					continue
				}
				seenEdges[edgeKey] = struct{}{}

				rows = append(rows, declaredInheritanceRow(childEntityID, entityType, semanticPayloadString(env.Payload, "path"), parent, repoID, "IMPLEMENTS"))
			}
		}

		if entityType != "Class" {
			continue
		}

		for _, adaptation := range traitAdaptations {
			for _, overriddenTrait := range inheritanceTraitOverrideTargets(adaptation) {
				parent, ok := entityIndex[inheritanceIndexKey{repoID: repoID, name: overriddenTrait}]
				if !ok {
					continue
				}

				edgeKey := childEntityID + "->" + parent.id + ":OVERRIDES"
				if _, dup := seenEdges[edgeKey]; dup {
					continue
				}
				seenEdges[edgeKey] = struct{}{}

				rows = append(rows, declaredInheritanceRow(childEntityID, entityType, semanticPayloadString(env.Payload, "path"), parent, repoID, "OVERRIDES"))
			}

			for _, aliasedTrait := range inheritanceTraitAliasTargets(adaptation) {
				parent, ok := entityIndex[inheritanceIndexKey{repoID: repoID, name: aliasedTrait}]
				if !ok {
					continue
				}

				edgeKey := childEntityID + "->" + parent.id + ":ALIASES"
				if _, dup := seenEdges[edgeKey]; dup {
					continue
				}
				seenEdges[edgeKey] = struct{}{}

				rows = append(rows, declaredInheritanceRow(childEntityID, entityType, semanticPayloadString(env.Payload, "path"), parent, repoID, "ALIASES"))
			}

			aliasMapping, ok := inheritanceTraitAliasMapping(adaptation)
			if !ok {
				continue
			}

			childMethod, childOK := methodIndex[inheritanceMethodIndexKey{
				repoID:       repoID,
				classContext: semanticPayloadString(env.Payload, "entity_name"),
				name:         aliasMapping.AliasMethodName,
			}]
			parentMethod, parentOK := methodIndex[inheritanceMethodIndexKey{
				repoID:       repoID,
				classContext: aliasMapping.TraitName,
				name:         aliasMapping.SourceMethodName,
			}]
			if !childOK || !parentOK {
				continue
			}

			edgeKey := childMethod.id + "->" + parentMethod.id + ":ALIASES"
			if _, dup := seenEdges[edgeKey]; dup {
				continue
			}
			seenEdges[edgeKey] = struct{}{}

			rows = append(rows, declaredInheritanceRow(childMethod.id, childMethod.entityType, childMethod.path, parentMethod, repoID, "ALIASES"))
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		left := anyToString(rows[i]["child_entity_id"]) + "->" + anyToString(rows[i]["parent_entity_id"])
		right := anyToString(rows[j]["child_entity_id"]) + "->" + anyToString(rows[j]["parent_entity_id"])
		if left == right {
			return anyToString(rows[i]["repo_id"]) < anyToString(rows[j]["repo_id"])
		}
		return left < right
	})

	return repoIDs, rows
}

// inheritanceIndexKey is a composite key for intra-repo entity name lookup.
type inheritanceIndexKey struct {
	repoID string
	name   string
}

type inheritanceMethodIndexKey struct {
	repoID       string
	classContext string
	name         string
}

type inheritanceEntityRef struct {
	id         string
	entityType string
	// path is the repo-qualified file path of the entity, captured so the
	// promoted shared-projection path can place each edge under a file-scoped
	// partition key and so the file-scoped delta retract (which keys on
	// child.path) can target exactly the changed files (#2867).
	path string
}

func declaredInheritanceRow(
	childEntityID string,
	childEntityType string,
	childPath string,
	parent inheritanceEntityRef,
	repoID string,
	relationshipType string,
) map[string]any {
	return map[string]any{
		"child_entity_id":    childEntityID,
		"child_entity_type":  childEntityType,
		"child_path":         childPath,
		"parent_entity_id":   parent.id,
		"parent_entity_type": parent.entityType,
		"repo_id":            repoID,
		"relationship_type":  relationshipType,
		"resolution_method":  codeprovenance.MethodDeclared,
	}
}

// buildInheritanceEntityIndex builds a map from (repo_id, entity_name) to
// entity identity and label for all inheritable entity types.
func buildInheritanceEntityIndex(envelopes []facts.Envelope) map[inheritanceIndexKey]inheritanceEntityRef {
	index := make(map[inheritanceIndexKey]inheritanceEntityRef)
	for _, env := range envelopes {
		if env.FactKind != "content_entity" {
			continue
		}
		entityType := semanticPayloadString(env.Payload, "entity_type")
		if _, ok := inheritableEntityTypes[entityType]; !ok {
			continue
		}
		repoID := semanticPayloadString(env.Payload, "repo_id")
		entityName := semanticPayloadString(env.Payload, "entity_name")
		entityID := semanticPayloadString(env.Payload, "entity_id")
		if repoID == "" || entityName == "" || entityID == "" {
			continue
		}
		key := inheritanceIndexKey{repoID: repoID, name: entityName}
		// First-seen wins; duplicates are ignored for matching purposes.
		if _, exists := index[key]; !exists {
			index[key] = inheritanceEntityRef{
				id:         entityID,
				entityType: entityType,
				path:       semanticPayloadString(env.Payload, "path"),
			}
		}
	}
	return index
}

func buildInheritanceMethodIndex(envelopes []facts.Envelope) map[inheritanceMethodIndexKey]inheritanceEntityRef {
	index := make(map[inheritanceMethodIndexKey]inheritanceEntityRef)
	for _, env := range envelopes {
		if env.FactKind != "content_entity" {
			continue
		}
		if semanticPayloadString(env.Payload, "entity_type") != "Function" {
			continue
		}
		repoID := semanticPayloadString(env.Payload, "repo_id")
		classContext := semanticPayloadMetadataString(env.Payload, "class_context")
		entityName := semanticPayloadString(env.Payload, "entity_name")
		entityID := semanticPayloadString(env.Payload, "entity_id")
		if repoID == "" || classContext == "" || entityName == "" || entityID == "" {
			continue
		}
		key := inheritanceMethodIndexKey{
			repoID:       repoID,
			classContext: classContext,
			name:         entityName,
		}
		if _, exists := index[key]; !exists {
			index[key] = inheritanceEntityRef{
				id:         entityID,
				entityType: "Function",
				path:       semanticPayloadString(env.Payload, "path"),
			}
		}
	}
	return index
}

// collectInheritanceRepoIDs returns sorted, deduplicated repository IDs from
// content entity envelopes.
func collectInheritanceRepoIDs(envelopes []facts.Envelope) []string {
	seen := make(map[string]struct{})
	repoIDs := make([]string, 0)
	for _, env := range envelopes {
		if env.FactKind != "content_entity" {
			continue
		}
		repoID := semanticPayloadString(env.Payload, "repo_id")
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

// inheritancePayloadBases extracts the bases string slice from the entity
// metadata in a content_entity fact payload.
func inheritancePayloadBases(payload map[string]any) []string {
	return semanticPayloadMetadataStringSlice(payload, "bases")
}
