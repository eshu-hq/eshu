// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonical

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildMaterializationFoldsImportFlagsOntoTheSharedEdge pins the #7345 fold
// rule. The extractor folds every parser entry for one (file, module) pair into
// ONE IMPORTS edge, and the parser flags (#7344) are per entry, so the edge must
// answer "can this file's import of that module close a load-time cycle?" from
// all of them.
//
// A file that imports a module once at load time really does depend on it at
// load time, so one unflagged entry keeps the edge a runtime edge. The subtle
// case is deferred: an `if TYPE_CHECKING:` import next to a function-local
// import of the same module never runs at load time, so the edge must not claim
// it does. Plain per-flag AND would report neither flag there and invent a
// load-time cycle, which is why deferred is folded over deferred-or-type-only.
func TestBuildMaterializationFoldsImportFlagsOntoTheSharedEdge(t *testing.T) {
	t.Parallel()

	entry := func(names ...string) map[string]any {
		row := map[string]any{"name": "X", "source": "./m", "line_number": 1}
		for _, name := range names {
			row[name] = true
		}
		return row
	}

	tests := []struct {
		name                             string
		entries                          []map[string]any
		wantTypeOnly, wantDeferred, want bool
	}{
		{name: "no flags", entries: []map[string]any{entry()}},
		{name: "single type_only", entries: []map[string]any{entry("type_only")}, wantTypeOnly: true},
		{name: "single deferred", entries: []map[string]any{entry("deferred")}, wantDeferred: true},
		{name: "single inferred", entries: []map[string]any{entry("inferred")}, want: true},
		{
			name:         "every entry type_only",
			entries:      []map[string]any{entry("type_only"), entry("type_only")},
			wantTypeOnly: true,
		},
		{
			name:    "one runtime entry keeps a type_only edge a runtime edge",
			entries: []map[string]any{entry("type_only"), entry()},
		},
		{
			name:         "every entry deferred",
			entries:      []map[string]any{entry("deferred"), entry("deferred")},
			wantDeferred: true,
		},
		{
			name:    "one runtime entry keeps a deferred edge a runtime edge",
			entries: []map[string]any{entry("deferred"), entry()},
		},
		{
			name:         "type_only beside deferred never runs at load time, so deferred",
			entries:      []map[string]any{entry("type_only"), entry("deferred")},
			wantDeferred: true,
		},
		{
			name:         "an entry with both flags counts as type_only",
			entries:      []map[string]any{entry("type_only", "deferred")},
			wantTypeOnly: true,
		},
		{
			name:         "both-flag entry beside a deferred entry is deferred",
			entries:      []map[string]any{entry("type_only", "deferred"), entry("deferred")},
			wantDeferred: true,
		},
		{
			name:    "both-flag entry beside a runtime entry is a runtime edge",
			entries: []map[string]any{entry("type_only", "deferred"), entry()},
		},
		{
			name:    "one resolved entry proves the target, so inferred is false",
			entries: []map[string]any{entry("inferred"), entry()},
		},
		{
			name:    "every entry inferred",
			entries: []map[string]any{entry("inferred"), entry("inferred")},
			want:    true,
		},

		// The fold must not depend on entry order: the accumulator is seeded
		// from the first entry, so every mixed case above puts the flagged
		// entry first and needs its mirror with the runtime entry first.
		{
			name:    "runtime entry first keeps a type_only edge a runtime edge",
			entries: []map[string]any{entry(), entry("type_only")},
		},
		{
			name:    "runtime entry first keeps a deferred edge a runtime edge",
			entries: []map[string]any{entry(), entry("deferred")},
		},
		{
			name:    "resolved entry first keeps an inferred edge confirmed",
			entries: []map[string]any{entry(), entry("inferred")},
		},
		{
			name:         "deferred then type_only is deferred, not runtime",
			entries:      []map[string]any{entry("deferred"), entry("type_only")},
			wantDeferred: true,
		},
		{
			name:         "deferred then a both-flag entry is deferred",
			entries:      []map[string]any{entry("deferred"), entry("type_only", "deferred")},
			wantDeferred: true,
		},
		{
			name:         "a both-flag entry then type_only stays type_only",
			entries:      []map[string]any{entry("type_only", "deferred"), entry("type_only")},
			wantTypeOnly: true,
		},
		{
			name:    "deferred, type_only, and a runtime entry in the middle is a runtime edge",
			entries: []map[string]any{entry("deferred"), entry(), entry("type_only")},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, quarantined := BuildMaterialization(testScope(), testGeneration(), []facts.Envelope{
				importRepositoryFact(),
				fileFactWithImports("f-1", "pkg/a.py", "python", tt.entries),
			})
			if len(quarantined) != 0 {
				t.Fatalf("quarantined = %d, want 0", len(quarantined))
			}
			if len(result.Imports) != 1 {
				t.Fatalf("len(Imports) = %d, want 1 (one edge per file and module): %+v", len(result.Imports), result.Imports)
			}
			row := result.Imports[0]
			if row.TypeOnly != tt.wantTypeOnly || row.Deferred != tt.wantDeferred || row.Inferred != tt.want {
				t.Fatalf("flags = type_only:%v deferred:%v inferred:%v, want type_only:%v deferred:%v inferred:%v",
					row.TypeOnly, row.Deferred, row.Inferred, tt.wantTypeOnly, tt.wantDeferred, tt.want)
			}
		})
	}
}
