package serviceintelhttp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// supplyChainImpactAggregateCapability is the query capability whose truth
// contract governs the report's supply_chain section. It mirrors
// get_supply_chain_impact_inventory so the section carries supply-chain-impact
// truth, not the service-story platform truth.
const supplyChainImpactAggregateCapability = "supply_chain.impact_findings.aggregate"

// SupplyChainEvidenceSource loads durable, service-scoped supply-chain impact
// inventory for a workload. Implementations must stay bounded and return nil
// when no inventory exists, so callers leave the section unsupported rather than
// fabricating an empty supported section.
type SupplyChainEvidenceSource interface {
	// SupplyChainInventoryForWorkload returns a bounded inventory map compatible
	// with serviceintel.FromSupplyChainInventory, or nil when the workload has no
	// supply-chain impact inventory. It returns an error only for infrastructure
	// failures, never to signal "no findings".
	SupplyChainInventoryForWorkload(ctx context.Context, workloadID string) (map[string]any, error)
}

// DurableSupplyChainEvidenceSource is the production SupplyChainEvidenceSource:
// it reads the reducer-owned supply-chain impact inventory aggregate scoped to
// the resolved workload id.
type DurableSupplyChainEvidenceSource struct {
	store  query.SupplyChainImpactAggregateStore
	logger *slog.Logger
}

// NewDurableSupplyChainEvidenceSource constructs the durable supply-chain source
// over a supply-chain impact aggregate store. A nil logger is tolerated.
func NewDurableSupplyChainEvidenceSource(
	store query.SupplyChainImpactAggregateStore,
	logger *slog.Logger,
) DurableSupplyChainEvidenceSource {
	return DurableSupplyChainEvidenceSource{store: store, logger: logger}
}

// SupplyChainInventoryForWorkload loads one bounded impact-status inventory page
// for the resolved workload. Empty inventory yields nil with no error; store
// failures propagate after an operator-facing log.
func (s DurableSupplyChainEvidenceSource) SupplyChainInventoryForWorkload(
	ctx context.Context,
	workloadID string,
) (map[string]any, error) {
	if s.store == nil {
		return nil, nil
	}
	filter := query.SupplyChainImpactAggregateFilter{
		WorkloadID:       workloadID,
		DetectionProfile: query.SupplyChainImpactProfilePrecise,
	}
	limit := query.SupplyChainImpactAggregateMaxLimit
	rows, err := s.store.SupplyChainImpactInventory(
		ctx,
		filter,
		query.SupplyChainImpactInventoryByImpactStatus,
		limit+1,
		0,
	)
	if err != nil {
		s.warn(ctx, workloadID)
		return nil, fmt.Errorf("load supply-chain impact inventory: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	count := 0
	for _, row := range rows {
		count += row.Count
	}
	if count == 0 {
		return nil, nil
	}
	return map[string]any{
		"buckets":           rows,
		"count":             count,
		"limit":             limit,
		"offset":            0,
		"group_by":          string(query.SupplyChainImpactInventoryByImpactStatus),
		"detection_profile": query.SupplyChainImpactProfilePrecise,
		"truncated":         truncated,
		"next_offset":       nil,
		"scope":             map[string]string{"workload_id": workloadID, "profile": query.SupplyChainImpactProfilePrecise},
	}, nil
}

func (s DurableSupplyChainEvidenceSource) warn(ctx context.Context, workloadID string) {
	if s.logger == nil {
		return
	}
	s.logger.WarnContext(
		ctx,
		"service intelligence report supply-chain impact inventory load failed",
		slog.String("event", "serviceintel.supply_chain_load_error"),
		slog.String("workload_id", workloadID),
	)
}
