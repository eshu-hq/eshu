package reducer

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// CloudAssetResolutionWrite captures the bounded canonical reconciliation
// request for one cloud asset resolution reducer intent.
type CloudAssetResolutionWrite struct {
	IntentID        string
	ScopeID         string
	GenerationID    string
	SourceSystem    string
	Cause           string
	EntityKeys      []string
	RelatedScopeIDs []string
}

// CloudAssetResolutionWriteResult captures the canonical cloud asset write
// outcome returned by the backend adapter.
type CloudAssetResolutionWriteResult struct {
	CanonicalID      string
	CanonicalWrites  int
	ReconciledScopes int
	EvidenceSummary  string
}

// CloudAssetResolutionWriter persists one cloud asset reconciliation request
// into a canonical reducer-owned target.
type CloudAssetResolutionWriter interface {
	WriteCloudAssetResolution(context.Context, CloudAssetResolutionWrite) (CloudAssetResolutionWriteResult, error)
}

// CloudAssetResolutionHandler reduces one cloud asset resolution intent into a
// bounded canonical write request.
type CloudAssetResolutionHandler struct {
	Writer         CloudAssetResolutionWriter
	PhasePublisher GraphProjectionPhasePublisher
}

// Handle executes the cloud asset resolution path.
func (h CloudAssetResolutionHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	if intent.Domain != DomainCloudAssetResolution {
		return Result{}, fmt.Errorf(
			"cloud asset resolution handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("cloud asset resolution writer is required")
	}

	request, err := cloudAssetResolutionWriteFromIntent(intent)
	if err != nil {
		return Result{}, err
	}

	writeResult, err := h.Writer.WriteCloudAssetResolution(ctx, request)
	if err != nil {
		return Result{}, err
	}
	if err := publishIntentGraphPhase(
		ctx,
		h.PhasePublisher,
		intent,
		GraphProjectionKeyspaceCloudResourceUID,
		GraphProjectionPhaseCanonicalNodesCommitted,
		time.Now().UTC(),
	); err != nil {
		return Result{}, err
	}

	evidenceSummary := strings.TrimSpace(writeResult.EvidenceSummary)
	if evidenceSummary == "" {
		evidenceSummary = fmt.Sprintf(
			"reconciled %d cloud asset key(s) across %d scope(s)",
			len(request.EntityKeys),
			len(request.RelatedScopeIDs),
		)
	}

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainCloudAssetResolution,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: evidenceSummary,
		CanonicalWrites: writeResult.CanonicalWrites,
	}, nil
}

func cloudAssetResolutionWriteFromIntent(intent Intent) (CloudAssetResolutionWrite, error) {
	entityKeys := uniqueSortedStrings(intent.EntityKeys)
	if len(entityKeys) == 0 {
		return CloudAssetResolutionWrite{}, fmt.Errorf(
			"cloud asset resolution intent %q must include at least one entity key",
			intent.IntentID,
		)
	}

	relatedScopeIDs := uniqueSortedStrings(append(intent.RelatedScopeIDs, intent.ScopeID))
	if len(relatedScopeIDs) == 0 {
		return CloudAssetResolutionWrite{}, fmt.Errorf(
			"cloud asset resolution intent %q must include at least one related scope id",
			intent.IntentID,
		)
	}

	return CloudAssetResolutionWrite{
		IntentID:        intent.IntentID,
		ScopeID:         intent.ScopeID,
		GenerationID:    intent.GenerationID,
		SourceSystem:    intent.SourceSystem,
		Cause:           intent.Cause,
		EntityKeys:      entityKeys,
		RelatedScopeIDs: relatedScopeIDs,
	}, nil
}
