package reducer

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	codeCallEvidenceSource            = "parser/code-calls"
	pythonMetaclassEvidenceSource     = "parser/python-metaclass"
	codeCallRepoRefreshEvidenceSource = "reducer/code-call-refresh"
)

// CanonicalNodeChecker checks whether canonical code entity nodes (Function,
// Class, File) exist in the graph. The code-call handler no longer uses this
// preflight check, but the type remains for compatibility with older wiring.
type CanonicalNodeChecker interface {
	HasCanonicalCodeTargets(ctx context.Context) (bool, error)
}

// CodeCallIntentWriter persists durable shared-intent rows for code-call
// materialization.
type CodeCallIntentWriter interface {
	UpsertIntents(ctx context.Context, rows []SharedProjectionIntentRow) error
}

// CodeCallMaterializationHandler reduces one parser relationship follow-up into
// durable shared-intent emission for code-call and Python metaclass rows.
type CodeCallMaterializationHandler struct {
	FactLoader   FactLoader
	IntentWriter CodeCallIntentWriter

	// EdgeWriter is retained for compatibility with older wiring and tests.
	// The handler no longer writes canonical edges directly.
	EdgeWriter SharedProjectionEdgeWriter
}

// Handle executes the code-call materialization path.
func (h CodeCallMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	if intent.Domain != DomainCodeCallMaterialization {
		return Result{}, fmt.Errorf(
			"code call materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("code call materialization fact loader is required")
	}
	if h.IntentWriter == nil {
		return Result{}, fmt.Errorf("code call materialization intent writer is required")
	}

	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{factKindRepository, factKindFile},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for code call materialization: %w", err)
	}

	contextByRepoID := buildCodeCallProjectionContexts(envelopes, intent.GenerationID)
	if len(contextByRepoID) == 0 {
		return Result{
			IntentID:        intent.IntentID,
			Domain:          DomainCodeCallMaterialization,
			Status:          ResultStatusSucceeded,
			EvidenceSummary: "no repositories available for code call materialization",
		}, nil
	}

	_, codeCallRows, _, metaclassRows := ExtractAllCodeRelationshipRows(envelopes)
	createdAt := intent.EnqueuedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	intentRows := buildCodeCallRefreshIntents(contextByRepoID, createdAt)
	intentRows = append(
		intentRows,
		buildCodeCallSharedIntentRows(codeCallRows, contextByRepoID, createdAt, codeCallEvidenceSource)...,
	)
	intentRows = append(
		intentRows,
		buildCodeCallSharedIntentRows(metaclassRows, contextByRepoID, createdAt, pythonMetaclassEvidenceSource)...,
	)

	if len(intentRows) == 0 {
		return Result{
			IntentID:        intent.IntentID,
			Domain:          DomainCodeCallMaterialization,
			Status:          ResultStatusSucceeded,
			EvidenceSummary: "no code-call or metaclass intents available for materialization",
		}, nil
	}

	if err := h.IntentWriter.UpsertIntents(ctx, intentRows); err != nil {
		return Result{}, fmt.Errorf("write code call intents: %w", err)
	}

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainCodeCallMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"emitted %d durable code call intents across %d repositories",
			len(intentRows),
			len(contextByRepoID),
		),
		CanonicalWrites: len(intentRows),
	}, nil
}

// ExtractAllCodeRelationshipRows builds both code-call and metaclass edge rows
// from a single entity index pass. This eliminates the duplicate
// buildCodeEntityIndex call that occurs when ExtractCodeCallRows and
// ExtractPythonMetaclassRows are called separately.
func ExtractAllCodeRelationshipRows(envelopes []facts.Envelope) (
	codeCallRepoIDs []string,
	codeCallRows []map[string]any,
	metaclassRepoIDs []string,
	metaclassRows []map[string]any,
) {
	if len(envelopes) == 0 {
		return nil, nil, nil, nil
	}

	repositoryIDs := collectCodeCallRepositoryIDs(envelopes)
	if len(repositoryIDs) == 0 {
		return nil, nil, nil, nil
	}

	entityIndex := buildCodeEntityIndex(envelopes)
	repositoryImports := collectCodeCallRepositoryImports(envelopes)

	ccRepoIDs, ccRows := extractCodeCallRowsWithIndex(envelopes, repositoryIDs, entityIndex, repositoryImports)
	mcRepoIDs, mcRows := extractPythonMetaclassRowsWithIndex(envelopes, repositoryIDs, entityIndex, repositoryImports)
	return ccRepoIDs, ccRows, mcRepoIDs, mcRows
}

// ExtractCodeCallRows builds canonical caller/callee edge rows from repository
// and file facts.
func ExtractCodeCallRows(envelopes []facts.Envelope) ([]string, []map[string]any) {
	if len(envelopes) == 0 {
		return nil, nil
	}

	repositoryIDs := collectCodeCallRepositoryIDs(envelopes)
	if len(repositoryIDs) == 0 {
		return nil, nil
	}

	entityIndex := buildCodeEntityIndex(envelopes)
	repositoryImports := collectCodeCallRepositoryImports(envelopes)
	return extractCodeCallRowsWithIndex(envelopes, repositoryIDs, entityIndex, repositoryImports)
}

func extractCodeCallRowsWithIndex(
	envelopes []facts.Envelope,
	repositoryIDs []string,
	entityIndex codeEntityIndex,
	repositoryImports map[string]map[string][]string,
) ([]string, []map[string]any) {
	seenRows := make(map[string]struct{})
	rows := make([]map[string]any, 0)

	for _, env := range envelopes {
		if env.FactKind != "file" {
			continue
		}

		repositoryID := payloadStr(env.Payload, "repo_id")
		if repositoryID == "" {
			continue
		}

		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}
		relativePath := payloadStr(env.Payload, "relative_path")

		rows = append(rows, extractSCIPCodeCallRows(repositoryID, entityIndex, seenRows, fileData)...)
		rows = append(
			rows,
			extractGenericCodeCallRows(
				repositoryID,
				relativePath,
				anyToString(fileData["path"]),
				entityIndex,
				repositoryImports[repositoryID],
				seenRows,
				fileData,
			)...,
		)
	}

	sort.Slice(rows, func(i, j int) bool {
		left := anyToString(rows[i]["caller_entity_id"]) + "->" + anyToString(rows[i]["callee_entity_id"])
		right := anyToString(rows[j]["caller_entity_id"]) + "->" + anyToString(rows[j]["callee_entity_id"])
		if left == right {
			return anyToString(rows[i]["repo_id"]) < anyToString(rows[j]["repo_id"])
		}
		return left < right
	})

	return repositoryIDs, rows
}

func collectCodeCallRepositoryIDs(envelopes []facts.Envelope) []string {
	repositorySet := make(map[string]struct{})
	for _, env := range envelopes {
		switch env.FactKind {
		case "repository", "file":
			repositoryID := payloadStr(env.Payload, "repo_id")
			if repositoryID == "" {
				repositoryID = payloadStr(env.Payload, "graph_id")
			}
			if repositoryID != "" {
				repositorySet[repositoryID] = struct{}{}
			}
		}
	}

	repositoryIDs := make([]string, 0, len(repositorySet))
	for repositoryID := range repositorySet {
		repositoryIDs = append(repositoryIDs, repositoryID)
	}
	sort.Strings(repositoryIDs)
	return repositoryIDs
}
