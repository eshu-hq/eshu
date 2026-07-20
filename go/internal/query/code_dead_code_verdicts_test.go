// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"
)

func rubyControllerActionResult(entityID string, rootKinds ...string) map[string]any {
	kinds := make([]any, 0, len(rootKinds))
	for _, k := range rootKinds {
		kinds = append(kinds, k)
	}
	return map[string]any{
		"entity_id": entityID,
		"name":      "index",
		"labels":    []any{"Function"},
		"file_path": "app/controllers/orders_controller.rb",
		"repo_id":   "repo-1",
		"language":  "ruby",
		"metadata":  map[string]any{"dead_code_root_kinds": kinds},
	}
}

// TestDeadCodeIsRubyRootHonorsDowngradeVerdict is the #5376 query flip: a
// ruby.rails_controller_action root is skipped (no longer a keep-alive root)
// only when a positive downgraded verdict exists for THAT entity; another root
// kind on the same entity still keeps it, and absence of a verdict keeps it.
func TestDeadCodeIsRubyRootHonorsDowngradeVerdict(t *testing.T) {
	downgraded := deadCodeDowngradedRoots{"orders-index": {rubyRailsControllerActionRootKind: {}}}

	tests := []struct {
		name       string
		result     map[string]any
		downgraded deadCodeDowngradedRoots
		wantRoot   bool
	}{
		{
			name:       "controller action with no verdict is kept (lag-safety)",
			result:     rubyControllerActionResult("orders-index", "ruby.rails_controller_action"),
			downgraded: nil,
			wantRoot:   true,
		},
		{
			name:       "controller action downgraded is no longer a root",
			result:     rubyControllerActionResult("orders-index", "ruby.rails_controller_action"),
			downgraded: downgraded,
			wantRoot:   false,
		},
		{
			name:       "downgraded controller action kept via another root kind",
			result:     rubyControllerActionResult("orders-index", "ruby.rails_controller_action", "ruby.rails_callback_method"),
			downgraded: downgraded,
			wantRoot:   true,
		},
		{
			name:       "different entity downgraded does not affect this one",
			result:     rubyControllerActionResult("other-index", "ruby.rails_controller_action"),
			downgraded: downgraded,
			wantRoot:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := &deadCodePolicyStats{}
			got := deadCodeIsRubyRoot(tt.result, nil, stats, tt.downgraded)
			if got != tt.wantRoot {
				t.Fatalf("deadCodeIsRubyRoot = %v, want %v", got, tt.wantRoot)
			}
		})
	}
}

// TestFilterDeadCodeResultsByDefaultPolicyDowngradeFlipsToDead proves the
// end-to-end policy effect: without a verdict a controller action is excluded
// from the dead list (kept alive); with a downgraded verdict it survives as a
// dead candidate. An empty/nil downgraded map is byte-for-byte today's behavior.
func TestFilterDeadCodeResultsByDefaultPolicyDowngradeFlipsToDead(t *testing.T) {
	result := rubyControllerActionResult("orders-index", "ruby.rails_controller_action")

	keptAlive, _ := filterDeadCodeResultsByDefaultPolicy([]map[string]any{result}, nil, nil)
	if len(keptAlive) != 0 {
		t.Fatalf("without a verdict a controller action must be kept alive (excluded), got %#v", keptAlive)
	}

	downgraded := deadCodeDowngradedRoots{"orders-index": {rubyRailsControllerActionRootKind: {}}}
	flagged, _ := filterDeadCodeResultsByDefaultPolicy([]map[string]any{result}, nil, downgraded)
	if len(flagged) != 1 {
		t.Fatalf("a downgraded controller action must survive as a dead candidate, got %#v", flagged)
	}
	if StringVal(flagged[0], "entity_id") != "orders-index" {
		t.Fatalf("wrong survivor: %#v", flagged[0])
	}
}

type fakeVerdictContentStore struct {
	fakePortContentStore
	downgraded map[string]map[string]struct{}
	err        error
	calls      int
}

func (f *fakeVerdictContentStore) DowngradedCodeRootKinds(
	_ context.Context,
	_ string,
	_ []string,
) (map[string]map[string]struct{}, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.downgraded, nil
}

// TestLoadDeadCodeDowngradedRootsFailOpen proves the lag-safety fail-open: a
// store error yields a nil map (KEEP everything, today's behavior), a store
// that does not implement the verdict interface yields nil, and a successful
// load surfaces the downgraded kinds.
func TestLoadDeadCodeDowngradedRootsFailOpen(t *testing.T) {
	results := []map[string]any{{"entity_id": "orders-index", "repo_id": "repo-1"}}

	t.Run("store error keeps everything", func(t *testing.T) {
		store := &fakeVerdictContentStore{err: errors.New("boom")}
		handler := &CodeHandler{Content: store}
		got := handler.loadDeadCodeDowngradedRoots(context.Background(), results)
		if got != nil {
			t.Fatalf("store error must fail-open to nil (KEEP), got %#v", got)
		}
		if store.calls != 1 {
			t.Fatalf("store calls = %d, want 1", store.calls)
		}
	})

	t.Run("store without verdict interface keeps everything", func(t *testing.T) {
		handler := &CodeHandler{Content: fakePortContentStore{}}
		if got := handler.loadDeadCodeDowngradedRoots(context.Background(), results); got != nil {
			t.Fatalf("non-verdict store must yield nil, got %#v", got)
		}
	})

	t.Run("successful load surfaces downgraded kinds", func(t *testing.T) {
		store := &fakeVerdictContentStore{downgraded: map[string]map[string]struct{}{
			"orders-index": {rubyRailsControllerActionRootKind: {}},
		}}
		handler := &CodeHandler{Content: store}
		got := handler.loadDeadCodeDowngradedRoots(context.Background(), results)
		if !got.isDowngraded("orders-index", rubyRailsControllerActionRootKind) {
			t.Fatalf("expected orders-index downgraded, got %#v", got)
		}
	})
}
