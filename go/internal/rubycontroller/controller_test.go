// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rubycontroller_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/rubycontroller"
)

// fakeRegistry is a repo-wide-style multimap registry: a class name maps to a
// set (slice) of declared qualified bases, unioned across reopened definitions.
// A class present with a nil/empty base slice is known but declares no
// superclass.
type fakeRegistry struct {
	bases map[string][]string
}

func (r fakeRegistry) DeclaredBases(className string) ([]string, bool) {
	bases, ok := r.bases[className]
	if !ok || len(bases) == 0 {
		return nil, false
	}
	return bases, true
}

func (r fakeRegistry) IsKnownClass(className string) bool {
	_, ok := r.bases[className]
	return ok
}

func TestDecide(t *testing.T) {
	tests := []struct {
		name       string
		class      string
		reg        map[string][]string
		wantKeep   bool
		wantReason string
	}{
		{
			name:       "cross-file confirmed via intermediate to accepted base",
			class:      "OrdersController",
			reg:        map[string][]string{"OrdersController": {"Admin::BaseController"}, "Admin::BaseController": {"ActionController::Base"}},
			wantKeep:   true,
			wantReason: rubycontroller.ReasonAccepted,
		},
		{
			name:       "cross-file downgraded resolves onward to ActiveRecord",
			class:      "OrdersController",
			reg:        map[string][]string{"OrdersController": {"BaseController"}, "BaseController": {"ApplicationRecord"}, "ApplicationRecord": {"ActiveRecord::Base"}},
			wantKeep:   false,
			wantReason: rubycontroller.ReasonUnresolvedNonController,
		},
		{
			name:       "direct accepted base",
			class:      "WidgetsController",
			reg:        map[string][]string{"WidgetsController": {"ApplicationController"}},
			wantKeep:   true,
			wantReason: rubycontroller.ReasonAccepted,
		},
		{
			name:       "unresolved gem base but Controller-suffixed keeps",
			class:      "OrdersController",
			reg:        map[string][]string{"OrdersController": {"Api::V2::BaseController"}},
			wantKeep:   true,
			wantReason: rubycontroller.ReasonUnresolvedController,
		},
		{
			name:       "unresolved non-controller gem base downgrades",
			class:      "OrdersController",
			reg:        map[string][]string{"OrdersController": {"Sinatra::Base"}},
			wantKeep:   false,
			wantReason: rubycontroller.ReasonUnresolvedNonController,
		},
		{
			name:       "fizzle with controller-suffix name keeps",
			class:      "OrdersController",
			reg:        map[string][]string{"OrdersController": nil},
			wantKeep:   true,
			wantReason: rubycontroller.ReasonFizzled,
		},
		{
			name:       "fizzle without controller-suffix name downgrades",
			class:      "OrderService",
			reg:        map[string][]string{"OrderService": nil},
			wantKeep:   false,
			wantReason: rubycontroller.ReasonFizzled,
		},
		{
			name:       "reopened-class union reaches accepted base",
			class:      "OrdersController",
			reg:        map[string][]string{"OrdersController": {"ActionController::Base", "OrderConcern"}, "OrderConcern": {"Sinatra::Base"}},
			wantKeep:   true,
			wantReason: rubycontroller.ReasonAccepted,
		},
		{
			name:       "collision conflict all paths downgrade",
			class:      "BaseController",
			reg:        map[string][]string{"BaseController": {"ApplicationRecord", "Grape::API"}, "ApplicationRecord": {"ActiveRecord::Base"}},
			wantKeep:   false,
			wantReason: rubycontroller.ReasonCollision,
		},
		{
			name:       "collision but one path confirms keeps",
			class:      "BaseController",
			reg:        map[string][]string{"BaseController": {"ApplicationController", "Grape::API"}},
			wantKeep:   true,
			wantReason: rubycontroller.ReasonAccepted,
		},
		{
			name:       "cycle is keep-biased for controller name",
			class:      "FooController",
			reg:        map[string][]string{"FooController": {"BarController"}, "BarController": {"FooController"}},
			wantKeep:   true,
			wantReason: rubycontroller.ReasonCycle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rubycontroller.Decide(tt.class, fakeRegistry{bases: tt.reg})
			if got.Keep != tt.wantKeep {
				t.Fatalf("Decide(%q).Keep = %v, want %v (decision=%+v)", tt.class, got.Keep, tt.wantKeep, got)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("Decide(%q).Reason = %q, want %q (decision=%+v)", tt.class, got.Reason, tt.wantReason, got)
			}
			if rubycontroller.IsRailsController(tt.class, fakeRegistry{bases: tt.reg}) != tt.wantKeep {
				t.Fatalf("IsRailsController(%q) disagrees with Decide().Keep", tt.class)
			}
		})
	}
}

// TestDecideDepthCapIsKeepBiased builds a chain longer than MaxWalkDepth that
// would resolve to a non-controller reject if fully walked, and proves the
// depth cap falls back to the keep-biased residual instead of downgrading.
func TestDecideDepthCapIsKeepBiased(t *testing.T) {
	reg := map[string][]string{}
	// C0Controller < C1 < C2 < ... < C11 < Sinatra::Base (12 hops > cap of 10).
	reg["C0Controller"] = []string{"C1"}
	for i := 1; i <= 11; i++ {
		reg[itoa("C", i)] = []string{itoa("C", i+1)}
	}
	reg["C12"] = []string{"Sinatra::Base"}
	got := rubycontroller.Decide("C0Controller", fakeRegistry{bases: reg})
	if !got.Keep {
		t.Fatalf("deep chain from a *Controller must keep at the depth cap, got %+v", got)
	}
	if got.Reason != rubycontroller.ReasonDepthCap {
		t.Fatalf("Reason = %q, want %q", got.Reason, rubycontroller.ReasonDepthCap)
	}
}

func itoa(prefix string, n int) string {
	digits := ""
	if n == 0 {
		digits = "0"
	}
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return prefix + digits
}
