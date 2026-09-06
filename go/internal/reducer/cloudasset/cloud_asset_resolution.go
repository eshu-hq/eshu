// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudasset

import (
	"context"
	"fmt"
	"strings"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
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
	PhasePublisher gpphase.PhasePublisher
}

// Handle executes the cloud asset resolution path.
func (h CloudAssetResolutionHandler) Handle(
	ctx context.Context,
	intent reducercontract.Intent,
) (reducercontract.Result, error) {
	if intent.Domain != reducercontract.DomainCloudAssetResolution {
		return reducercontract.Result{}, fmt.Errorf(
			"cloud asset resolution handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.Writer == nil {
		return reducercontract.Result{}, fmt.Errorf("cloud asset resolution writer is required")
	}

	request, err := cloudAssetResolutionWriteFromIntent(intent)
	if err != nil {
		return reducercontract.Result{}, err
	}

	writeResult, err := h.Writer.WriteCloudAssetResolution(ctx, request)
	if err != nil {
		return reducercontract.Result{}, err
	}
	if err := gpphase.PublishIntentGraphPhase(
		ctx,
		h.PhasePublisher,
		intent,
		gpphase.KeyspaceCloudResourceUID,
		gpphase.PhaseCanonicalNodesCommitted,
		time.Now().UTC(),
	); err != nil {
		return reducercontract.Result{}, err
	}

	evidenceSummary := strings.TrimSpace(writeResult.EvidenceSummary)
	if evidenceSummary == "" {
		evidenceSummary = fmt.Sprintf(
			"reconciled %d cloud asset key(s) across %d scope(s)",
			len(request.EntityKeys),
			len(request.RelatedScopeIDs),
		)
	}

	return reducercontract.Result{
		IntentID:        intent.IntentID,
		Domain:          reducercontract.DomainCloudAssetResolution,
		Status:          reducercontract.ResultStatusSucceeded,
		EvidenceSummary: evidenceSummary,
		CanonicalWrites: writeResult.CanonicalWrites,
	}, nil
}

func cloudAssetResolutionWriteFromIntent(intent reducercontract.Intent) (CloudAssetResolutionWrite, error) {
	entityKeys := payloadcore.UniqueSortedStrings(intent.EntityKeys)
	if len(entityKeys) == 0 {
		return CloudAssetResolutionWrite{}, fmt.Errorf(
			"cloud asset resolution intent %q must include at least one entity key",
			intent.IntentID,
		)
	}

	relatedScopeIDs := payloadcore.UniqueSortedStrings(append(intent.RelatedScopeIDs, intent.ScopeID))
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
