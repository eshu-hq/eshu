// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"strings"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
)

// TestValidateBoundedStatusesRefinedClass pins how the host bounds a
// collector-refined "<class>.<reason>" failure class (eshu-hq/eshu#9289): the
// dotted token is one class under the existing pattern, and malformed tokens
// still fail the result as invalid.
func TestValidateBoundedStatusesRefinedClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		class   string
		wantErr bool
	}{
		{name: "base class", class: "invalid_config"},
		{name: "refined class", class: "invalid_config.target_scope_mismatch"},
		{name: "refined class at the bound", class: "a." + strings.Repeat("b", 94)},
		{name: "leading dot", class: ".target_scope_mismatch", wantErr: true},
		{name: "over the bound", class: "invalid_config." + strings.Repeat("a", 90), wantErr: true},
		{name: "uppercase reason", class: "invalid_config.Target", wantErr: true},
		{name: "space in reason", class: "invalid_config.bad reason", wantErr: true},
		{name: "padded", class: " invalid_config.target_scope_mismatch", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := sdkcollector.Result{Statuses: []sdkcollector.Status{{
				Class:        sdkcollector.StatusFailure,
				FailureClass: tc.class,
			}}}
			err := validateBoundedStatuses(result)
			if tc.wantErr && err == nil {
				t.Fatalf("validateBoundedStatuses(%q) error = nil, want rejection", tc.class)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateBoundedStatuses(%q) error = %v, want accepted", tc.class, err)
			}
		})
	}
}
