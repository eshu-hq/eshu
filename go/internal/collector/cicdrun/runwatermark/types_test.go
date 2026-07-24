// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runwatermark

import "testing"

func TestKeyValidateRejectsMissingFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  Key
	}{
		{"missing scope_id", Key{Repository: "octo/repo"}},
		{"missing repository", Key{ScopeID: "scope-1"}},
		{"missing both", Key{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.key.Validate(); err == nil {
				t.Fatalf("Validate() error = nil, want error for %+v", test.key)
			}
		})
	}
}

func TestKeyValidateAcceptsCompleteKey(t *testing.T) {
	t.Parallel()

	key := Key{ScopeID: "scope-1", Repository: "octo/repo"}
	if err := key.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestWatermarkValidateRejectsIncompleteValues(t *testing.T) {
	t.Parallel()

	base := Watermark{
		Key:          Key{ScopeID: "scope-1", Repository: "octo/repo"},
		LastRunID:    "42",
		GenerationID: "generation-1",
		FencingToken: 1,
	}

	tests := []struct {
		name    string
		mutate  func(Watermark) Watermark
		wantErr bool
	}{
		{"complete", func(w Watermark) Watermark { return w }, false},
		{"missing last_run_id", func(w Watermark) Watermark { w.LastRunID = ""; return w }, true},
		{"missing generation_id", func(w Watermark) Watermark { w.GenerationID = ""; return w }, true},
		{"non-positive fencing_token", func(w Watermark) Watermark { w.FencingToken = 0; return w }, true},
		{"negative fencing_token", func(w Watermark) Watermark { w.FencingToken = -1; return w }, true},
		{"invalid key", func(w Watermark) Watermark { w.Key = Key{}; return w }, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := test.mutate(base).Validate()
			if test.wantErr && err == nil {
				t.Fatalf("Validate() error = nil, want error")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}
