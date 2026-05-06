package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

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

// InheritanceMaterializationHandler reduces one inheritance follow-up into
// canonical INHERITS, OVERRIDES, and ALIASES edge writes using parser entity
// bases and PHP trait adaptation metadata.
type InheritanceMaterializationHandler struct {
	FactLoader           FactLoader
	EdgeWriter           SharedProjectionEdgeWriter
	PriorGenerationCheck PriorGenerationCheck
}

// Handle executes the inheritance materialization path.
func (h InheritanceMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	if intent.Domain != DomainInheritanceMaterialization {
		return Result{}, fmt.Errorf(
			"inheritance materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("inheritance materialization fact loader is required")
	}
	if h.EdgeWriter == nil {
		return Result{}, fmt.Errorf("inheritance materialization edge writer is required")
	}

	slog.InfoContext(ctx, "inheritance materialization started",
		slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
		slog.String(telemetry.LogKeyDomain, string(intent.Domain)),
	)

	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{factKindContentEntity},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for inheritance materialization: %w", err)
	}

	repoIDs, rows := ExtractInheritanceRows(envelopes)
	if len(repoIDs) == 0 {
		return Result{
			IntentID:        intent.IntentID,
			Domain:          DomainInheritanceMaterialization,
			Status:          ResultStatusSucceeded,
			EvidenceSummary: "no repositories available for inheritance materialization",
		}, nil
	}

	skipRetract, err := h.shouldSkipInheritanceRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	if skipRetract {
		slog.InfoContext(ctx, "inheritance materialization skipped first-generation retract",
			slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
			slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			slog.String(telemetry.LogKeyDomain, string(intent.Domain)),
		)
	} else {
		if err := h.EdgeWriter.RetractEdges(
			ctx,
			DomainInheritanceEdges,
			buildInheritanceRetractRows(repoIDs),
			inheritanceEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical inheritance edges: %w", err)
		}
	}

	writeRows := buildInheritanceIntentRows(rows)
	if len(writeRows) > 0 {
		if err := h.EdgeWriter.WriteEdges(
			ctx,
			DomainInheritanceEdges,
			writeRows,
			inheritanceEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical inheritance edges: %w", err)
		}
	}

	slog.InfoContext(ctx, "inheritance materialization completed",
		slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
		slog.Int("edge_count", len(writeRows)),
		slog.Int("repo_count", len(repoIDs)),
	)

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainInheritanceMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d canonical inheritance edges across %d repositories",
			len(writeRows),
			len(repoIDs),
		),
		CanonicalWrites: len(writeRows),
	}, nil
}

func (h InheritanceMaterializationHandler) shouldSkipInheritanceRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for inheritance retract: %w", err)
	}
	return !hasPrior, nil
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
		if len(bases) == 0 && len(traitAdaptations) == 0 {
			continue
		}

		for _, baseName := range bases {
			parentEntityID, ok := entityIndex[inheritanceIndexKey{repoID: repoID, name: baseName}]
			if !ok {
				continue
			}

			edgeKey := childEntityID + "->" + parentEntityID
			if _, dup := seenEdges[edgeKey]; dup {
				continue
			}
			seenEdges[edgeKey] = struct{}{}

			rows = append(rows, map[string]any{
				"child_entity_id":   childEntityID,
				"parent_entity_id":  parentEntityID,
				"repo_id":           repoID,
				"relationship_type": "INHERITS",
			})
		}

		if entityType != "Class" {
			continue
		}

		for _, adaptation := range traitAdaptations {
			for _, overriddenTrait := range inheritanceTraitOverrideTargets(adaptation) {
				parentEntityID, ok := entityIndex[inheritanceIndexKey{repoID: repoID, name: overriddenTrait}]
				if !ok {
					continue
				}

				edgeKey := childEntityID + "->" + parentEntityID + ":OVERRIDES"
				if _, dup := seenEdges[edgeKey]; dup {
					continue
				}
				seenEdges[edgeKey] = struct{}{}

				rows = append(rows, map[string]any{
					"child_entity_id":   childEntityID,
					"parent_entity_id":  parentEntityID,
					"repo_id":           repoID,
					"relationship_type": "OVERRIDES",
				})
			}

			for _, aliasedTrait := range inheritanceTraitAliasTargets(adaptation) {
				parentEntityID, ok := entityIndex[inheritanceIndexKey{repoID: repoID, name: aliasedTrait}]
				if !ok {
					continue
				}

				edgeKey := childEntityID + "->" + parentEntityID + ":ALIASES"
				if _, dup := seenEdges[edgeKey]; dup {
					continue
				}
				seenEdges[edgeKey] = struct{}{}

				rows = append(rows, map[string]any{
					"child_entity_id":   childEntityID,
					"parent_entity_id":  parentEntityID,
					"repo_id":           repoID,
					"relationship_type": "ALIASES",
				})
			}

			aliasMapping, ok := inheritanceTraitAliasMapping(adaptation)
			if !ok {
				continue
			}

			childMethodID, childOK := methodIndex[inheritanceMethodIndexKey{
				repoID:       repoID,
				classContext: semanticPayloadString(env.Payload, "entity_name"),
				name:         aliasMapping.AliasMethodName,
			}]
			parentMethodID, parentOK := methodIndex[inheritanceMethodIndexKey{
				repoID:       repoID,
				classContext: aliasMapping.TraitName,
				name:         aliasMapping.SourceMethodName,
			}]
			if !childOK || !parentOK {
				continue
			}

			edgeKey := childMethodID + "->" + parentMethodID + ":ALIASES"
			if _, dup := seenEdges[edgeKey]; dup {
				continue
			}
			seenEdges[edgeKey] = struct{}{}

			rows = append(rows, map[string]any{
				"child_entity_id":   childMethodID,
				"parent_entity_id":  parentMethodID,
				"repo_id":           repoID,
				"relationship_type": "ALIASES",
			})
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

// buildInheritanceEntityIndex builds a map from (repo_id, entity_name) to
// entity_id for all inheritable entity types.
func buildInheritanceEntityIndex(envelopes []facts.Envelope) map[inheritanceIndexKey]string {
	index := make(map[inheritanceIndexKey]string)
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
			index[key] = entityID
		}
	}
	return index
}

func buildInheritanceMethodIndex(envelopes []facts.Envelope) map[inheritanceMethodIndexKey]string {
	index := make(map[inheritanceMethodIndexKey]string)
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
			index[key] = entityID
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

// buildInheritanceRetractRows builds retract rows for each repository.
func buildInheritanceRetractRows(repoIDs []string) []SharedProjectionIntentRow {
	rows := make([]SharedProjectionIntentRow, 0, len(repoIDs))
	for _, repoID := range repoIDs {
		rows = append(rows, SharedProjectionIntentRow{RepositoryID: repoID})
	}
	return rows
}

// buildInheritanceIntentRows converts extracted edge rows into shared
// projection intent rows.
func buildInheritanceIntentRows(rows []map[string]any) []SharedProjectionIntentRow {
	intents := make([]SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		childID := anyToString(row["child_entity_id"])
		parentID := anyToString(row["parent_entity_id"])
		intents = append(intents, SharedProjectionIntentRow{
			ProjectionDomain: DomainInheritanceEdges,
			PartitionKey:     childID + "->" + parentID,
			RepositoryID:     anyToString(row["repo_id"]),
			Payload:          copyPayload(row),
		})
	}
	return intents
}
