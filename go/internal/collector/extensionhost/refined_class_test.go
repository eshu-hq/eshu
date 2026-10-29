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
		{name: "first past the bound", class: "a." + strings.Repeat("b", 95), wantErr: true},
		{name: "far over the bound", class: "invalid_config." + strings.Repeat("a", 90), wantErr: true},
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

// TestValidateBoundedStatusesErrorOmitsRawValue pins eshu-hq/eshu#9310: a
// status with an over-long failure_class containing characters outside the
// pattern is rejected with an error that never carries the raw
// extension-supplied value, so untrusted text cannot reach operator logs or
// the stored failure message through invalid_result.
func TestValidateBoundedStatusesErrorOmitsRawValue(t *testing.T) {
	t.Parallel()

	hostile := "invalid_config." + strings.Repeat("a", 90) + "!$(touch /tmp/eshu-9310)\nSECOND-LINE"
	result := sdkcollector.Result{Statuses: []sdkcollector.Status{{
		Class:        sdkcollector.StatusFailure,
		FailureClass: hostile,
	}}}
	err := validateBoundedStatuses(result)
	if err == nil {
		t.Fatal("validateBoundedStatuses(hostile) error = nil, want rejection")
	}
	// %q escapes control characters, so assert on the long distinctive run
	// that survives quoting verbatim: the unbounded extension text reaches
	// the error either way.
	if run := strings.Repeat("a", 90); strings.Contains(err.Error(), run) {
		t.Fatalf("validateBoundedStatuses error echoes raw failure_class: %v", err)
	}
}
