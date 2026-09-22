// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"errors"
	"fmt"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestClassifyCloudRetractErrorMarksUndrainedAdmissionRetryable pins the
// readiness classification of a fence refusal: the sentinel (wrapped with
// scope detail, as storage returns it) becomes a Retryable error in the
// cloud_admission_not_ready class that still unwraps to the sentinel, while
// an unrelated retract error passes through untouched and unclassified.
func TestClassifyCloudRetractErrorMarksUndrainedAdmissionRetryable(t *testing.T) {
	t.Parallel()

	cause := fmt.Errorf("%w: scope-b/gen-2=pending", reducercontract.ErrCloudAdmissionUndrained)
	got := ClassifyCloudRetractError(fmt.Errorf("retract dead cloud resource nodes: %w", cause))
	if !errors.Is(got, reducercontract.ErrCloudAdmissionUndrained) {
		t.Fatalf("classified error %v does not unwrap to the sentinel", got)
	}
	if !IsRetryable(got) {
		t.Fatalf("classified error %v is not Retryable", got)
	}
	var classed interface{ FailureClass() string }
	if !errors.As(got, &classed) || classed.FailureClass() != CloudAdmissionNotReadyFailureClass {
		t.Fatalf("classified error %v does not report %q", got, CloudAdmissionNotReadyFailureClass)
	}

	other := errors.New("graph delete failed")
	if got := ClassifyCloudRetractError(other); got != other {
		t.Fatalf("unrelated error was rewritten: %v", got)
	}
	if ClassifyCloudRetractError(nil) != nil {
		t.Fatal("nil must stay nil")
	}
}
