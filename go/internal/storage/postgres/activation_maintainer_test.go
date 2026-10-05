// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
)

// TestActivationMaintainerMapsTheTargetedPassToTheConsumerContract pins every
// branch of the production maintainer's mapping (#7584): the owed
// partition's own outcome decides not_active and inapplicable even when the
// pass refused; typed refusals become holds with their stable reason; a
// published phase lets Finalize run; retry and untyped errors are failures.
func TestActivationMaintainerMapsTheTargetedPassToTheConsumerContract(t *testing.T) {
	t.Parallel()
	owed := OwedPartition{ScopeID: "git:owed", GenerationID: "gen-owed"}
	result := func(kind TargetedMaintenanceOutcomeKind) TargetedMaintenanceResult {
		return TargetedMaintenanceResult{Outcomes: []TargetedMaintenanceOutcome{
			{Partition: OwedPartition{ScopeID: "git:other", GenerationID: "gen-other"}, Kind: TargetedMaintenancePublished},
			{Partition: owed, Kind: kind},
		}}
	}
	refused := func(sentinel *TargetedMaintenanceError) error {
		return fmt.Errorf("partition-scoped maintenance: %w", sentinel)
	}
	for _, tc := range []struct {
		name       string
		result     TargetedMaintenanceResult
		err        error
		wantNil    bool
		wantIs     error
		wantHold   string
		wantNoHold bool
	}{
		{name: "published proceeds to finalize", result: result(TargetedMaintenancePublished), wantNil: true},
		{name: "not_active proceeds so finalize retires obsolete", result: result(TargetedMaintenanceNotActive), wantNil: true},
		{name: "not_active wins over a catalog refusal", result: result(TargetedMaintenanceNotActive), err: refused(ErrTargetedMaintenanceCatalogChanged), wantNil: true},
		{name: "inapplicable retires", result: result(TargetedMaintenanceInapplicable), wantIs: maintenance.ErrActivationInapplicable, wantNoHold: true},
		{name: "inapplicable wins over a catalog refusal", result: result(TargetedMaintenanceInapplicable), err: refused(ErrTargetedMaintenanceCatalogChanged), wantIs: maintenance.ErrActivationInapplicable, wantNoHold: true},
		{name: "catalog_changed holds", result: result(TargetedMaintenanceRetry), err: refused(ErrTargetedMaintenanceCatalogChanged), wantHold: "catalog_changed"},
		{name: "no_memo_baseline holds", result: result(TargetedMaintenanceRetry), err: refused(ErrTargetedMaintenanceNoMemoBaseline), wantHold: "no_memo_baseline"},
		{name: "closure_too_deep holds", result: result(TargetedMaintenanceRetry), err: refused(ErrTargetedMaintenanceClosureTooDeep), wantHold: "closure_too_deep"},
		{name: "retry is a failure", result: result(TargetedMaintenanceRetry), wantIs: ErrTargetedMaintenanceRetry, wantNoHold: true},
		{name: "untyped error is a failure", result: result(TargetedMaintenanceRetry), err: errors.New("connection reset"), wantNoHold: true},
		{name: "missing outcome is a failure", result: TargetedMaintenanceResult{}, wantNoHold: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotOwed []OwedPartition
			maintainer := ActivationMaintainer{pass: func(_ context.Context, partitions []OwedPartition) (TargetedMaintenanceResult, error) {
				gotOwed = partitions
				return tc.result, tc.err
			}}
			err := maintainer.MaintainActivation(context.Background(), maintenance.ActivationObligation{
				ScopeID: owed.ScopeID, GenerationID: owed.GenerationID, LeaseOwner: "o", LeaseToken: 3,
			})
			if len(gotOwed) != 1 || gotOwed[0] != owed {
				t.Fatalf("owed partitions = %+v, want exactly %+v", gotOwed, owed)
			}
			if tc.wantNil {
				if err != nil {
					t.Fatalf("MaintainActivation() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("MaintainActivation() = nil, want an error")
			}
			var hold *maintenance.ActivationHoldError
			isHold := errors.As(err, &hold)
			if tc.wantHold != "" && (!isHold || hold.Reason() != tc.wantHold) {
				t.Fatalf("MaintainActivation() = %v, want a hold with reason %q", err, tc.wantHold)
			}
			if tc.wantNoHold && isHold {
				t.Fatalf("MaintainActivation() = %v is a hold, want none", err)
			}
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Fatalf("MaintainActivation() = %v, want errors.Is %v", err, tc.wantIs)
			}
		})
	}
}
