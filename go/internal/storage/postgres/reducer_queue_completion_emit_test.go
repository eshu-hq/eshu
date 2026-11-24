// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/cicdrun"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestCompletionEmitArgsBindsOneFlagPerGatedDomain pins the single-ack $5
// binding (#10972 PR-4): ci_cd and container_image_identity each bind one emit
// flag, a missing verdict emits, and other domains bind nothing.
func TestCompletionEmitArgsBindsOneFlagPerGatedDomain(t *testing.T) {
	t.Parallel()

	unchanged := reducercontract.Result{SubSignals: map[string]float64{cicdrun.CompletionChangedSignal: 0}}
	cases := []struct {
		name   string
		domain reducercontract.Domain
		result reducercontract.Result
		want   []any
	}{
		{"ci_cd unchanged", reducer.DomainCICDRunCorrelation, unchanged, []any{false}},
		{
			"ci_cd changed", reducer.DomainCICDRunCorrelation,
			reducercontract.Result{SubSignals: map[string]float64{cicdrun.CompletionChangedSignal: 1}},
			[]any{true},
		},
		{"ci_cd without verdict", reducer.DomainCICDRunCorrelation, reducercontract.Result{}, []any{true}},
		{"identity without verdict", reducer.DomainContainerImageIdentity, reducercontract.Result{}, []any{true}},
		{"ungated domain", reducer.DomainWorkloadMaterialization, unchanged, nil},
	}
	for _, tc := range cases {
		got := completionEmitArgs(reducercontract.Intent{Domain: tc.domain}, tc.result)
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: args = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestCICDCompletionEmitIDsFailsOpen pins the batch ack ids: an unchanged
// paired result is left out, a changed one and an unpaired item emit.
func TestCICDCompletionEmitIDsFailsOpen(t *testing.T) {
	t.Parallel()

	intents := []reducercontract.Intent{{IntentID: "unchanged"}, {IntentID: "changed"}, {IntentID: "unpaired"}}
	results := map[string]reducercontract.Result{
		"unchanged": {SubSignals: map[string]float64{cicdrun.CompletionChangedSignal: 0}},
		"changed":   {SubSignals: map[string]float64{cicdrun.CompletionChangedSignal: 1}},
	}
	got := cicdCompletionEmitIDs(intents, results)
	if want := []string{"changed", "unpaired"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("emit ids = %v, want %v", got, want)
	}
}
