// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// transferProofRegistry mirrors the live manifest: the two Go umbrellas are
// ruleset-required, the aggregate publisher is not (#9089).
func transferProofRegistry() *cigates.Registry {
	return &cigates.Registry{RequiredStatusChecks: []cigates.RequiredStatusCheck{
		{Context: "go-core-complete", RulesetRequired: true},
		{Context: "go-race-complete", RulesetRequired: true},
		{Context: "required-gates-complete", RulesetRequired: false},
	}}
}

// The transfer paths (reuse, smoke, pr-plan) judge a PREVIOUS head, and the
// aggregate's status description carries the proof they transfer. #9094 asked
// whether they should narrow to the ruleset set: they must not, because the
// aggregate is the proof carrier, not a merge gate. This pins that decision.
func TestTransferProofContextsKeepsAggregateOutsideRuleset(t *testing.T) {
	t.Parallel()

	got := transferProofContexts(transferProofRegistry())
	want := []string{"go-core-complete", "go-race-complete", "required-gates-complete"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("transferProofContexts = %v, want %v", got, want)
	}
}
