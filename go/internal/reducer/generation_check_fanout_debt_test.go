// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestRuntimeExecuteRunsStaleFanOutContinuation is the runtime half of the
// #10060 regression: a supply_chain_impact intent whose generation was
// superseded while it still owed holder fan-out (a capped continuation list
// or a committed-but-unfanned owed marker) must still reach its handler so
// the stored holders drain, instead of returning a superseded result the
// service acks away. The carve-out must stay narrow: a manifest marker
// (covered by its completion-stamp baseline diff, #9874), a debt-free stale
// intent, and any other domain still skip, and a not-yet-active generation
// still defers with its error even when it carries debt.
func TestRuntimeExecuteRunsStaleFanOutContinuation(t *testing.T) {
	t.Parallel()

	stale := func(_ context.Context, _, _ string) (bool, error) { return false, nil }
	notYetActive := func(_ context.Context, scopeID, generationID string) (bool, error) {
		return false, reducercontract.GenerationNotYetActiveError{ScopeID: scopeID, GenerationID: generationID, ActiveGenerationID: "gen-new"}
	}

	cases := []struct {
		name      string
		domain    reducercontract.Domain
		payload   map[string]any
		check     GenerationFreshnessCheck
		wantCalls int
		wantErr   bool
	}{
		{
			name:   "continuation list runs",
			domain: DomainSupplyChainImpact,
			payload: map[string]any{
				supplychaincore.PayloadKeyFanOutRemaining: []any{"scope:holder-a"},
				supplychaincore.PayloadKeyFanOutOwed:      true,
			},
			check:     stale,
			wantCalls: 1,
		},
		{
			name:   "owed marker without a list runs",
			domain: DomainSupplyChainImpact,
			payload: map[string]any{
				supplychaincore.PayloadKeyFanOutOwed: true,
			},
			check:     stale,
			wantCalls: 1,
		},
		{
			name:   "remaining list without the marker runs",
			domain: DomainSupplyChainImpact,
			payload: map[string]any{
				supplychaincore.PayloadKeyFanOutRemaining: []any{"scope:holder-a"},
			},
			check:     stale,
			wantCalls: 1,
		},
		{
			name:   "manifest marker continuation still skips",
			domain: DomainSupplyChainImpact,
			payload: map[string]any{
				supplychaincore.PayloadKeyTrigger:         supplychaincore.TriggerManifestDependency,
				supplychaincore.PayloadKeyFanOutRemaining: []any{"scope:holder-a"},
			},
			check:     stale,
			wantCalls: 0,
		},
		{
			name:      "debt-free stale intent still skips",
			domain:    DomainSupplyChainImpact,
			payload:   map[string]any{"entity_key": "supply_chain_impact:scope-123"},
			check:     stale,
			wantCalls: 0,
		},
		{
			name:   "debt keys on another domain still skip",
			domain: DomainWorkloadIdentity,
			payload: map[string]any{
				supplychaincore.PayloadKeyFanOutRemaining: []any{"scope:holder-a"},
				supplychaincore.PayloadKeyFanOutOwed:      true,
			},
			check:     stale,
			wantCalls: 0,
		},
		{
			name:   "not-yet-active generation still defers with debt",
			domain: DomainSupplyChainImpact,
			payload: map[string]any{
				supplychaincore.PayloadKeyFanOutRemaining: []any{"scope:holder-a"},
				supplychaincore.PayloadKeyFanOutOwed:      true,
			},
			check:     notYetActive,
			wantCalls: 0,
			wantErr:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := &stubHandler{result: reducercontract.Result{
				IntentID: "intent-1", Domain: tc.domain, Status: ResultStatusSucceeded,
			}}
			registry := NewRegistry()
			if err := registry.Register(DomainDefinition{
				Domain:        tc.domain,
				Summary:       "test domain",
				Ownership:     OwnershipShape{CrossSource: true, CrossScope: true, CanonicalWrite: true},
				TruthContract: testTruthContract("fanout_debt"),
				Handler:       handler,
			}); err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			rt, err := NewRuntime(registry)
			if err != nil {
				t.Fatalf("NewRuntime() error = %v", err)
			}
			rt.GenerationCheck = tc.check

			result, err := rt.Execute(context.Background(), reducercontract.Intent{
				IntentID:     "intent-1",
				ScopeID:      "scope-123",
				GenerationID: "gen-old",
				SourceSystem: "vuln",
				Domain:       tc.domain,
				Cause:        "test",
				EntityKeys:   []string{"supply_chain_impact:scope-123"},
				EnqueuedAt:   time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
				AvailableAt:  time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
				Status:       IntentStatusClaimed,
				Payload:      tc.payload,
			})
			if tc.wantErr {
				if !errors.Is(err, reducercontract.ErrGenerationNotYetActive) {
					t.Fatalf("Execute() error = %v, want ErrGenerationNotYetActive", err)
				}
			} else if err != nil {
				t.Fatalf("Execute() error = %v, want nil", err)
			}
			if handler.handleCalls != tc.wantCalls {
				t.Fatalf("handler calls = %d, want %d (result status %q)", handler.handleCalls, tc.wantCalls, result.Status)
			}
			if tc.wantCalls == 0 && !tc.wantErr && result.Status != ResultStatusSuperseded {
				t.Fatalf("result status = %q, want superseded for a skipped stale intent", result.Status)
			}
		})
	}
}
