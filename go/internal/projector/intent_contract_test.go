// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"

	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

var (
	_ *projectorintent.ReducerIntent = (*runtime.ReducerIntent)(nil)
	_ *runtime.ReducerIntent         = (*projectorintent.ReducerIntent)(nil)
)

func TestReducerIntentCompatibilityAliasPreservesContract(t *testing.T) {
	t.Parallel()

	intent := runtime.ReducerIntent{
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		Domain:       reducer.DomainPackageSourceCorrelation,
	}
	if got, want := intent.ScopeGenerationKey(), "scope-1:generation-1"; got != want {
		t.Fatalf("ReducerIntent.ScopeGenerationKey() = %q, want %q", got, want)
	}
}
