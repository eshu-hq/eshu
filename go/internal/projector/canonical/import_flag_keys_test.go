// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonical

import (
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// TestImportFlagKeysMatchTheSDKFieldTags pins the one string contract between
// the parsers and the projector. The parsers write the flags under the
// shared.ImportFlag* keys; the projector reads them through named fields on
// codegraph/v1.Import, which decode by their json tags. The two live in
// different modules and nothing else links them, so a rename on one side would
// leave every import reading as unflagged, with no error: every type-only import
// would silently become a runtime edge.
func TestImportFlagKeysMatchTheSDKFieldTags(t *testing.T) {
	t.Parallel()

	importType := reflect.TypeOf(codegraphv1.Import{})
	for field, key := range map[string]string{
		"TypeOnly": shared.ImportFlagTypeOnly,
		"Deferred": shared.ImportFlagDeferred,
		"Inferred": shared.ImportFlagInferred,
	} {
		structField, ok := importType.FieldByName(field)
		if !ok {
			t.Fatalf("codegraphv1.Import has no field %s", field)
		}
		name, _, _ := strings.Cut(structField.Tag.Get("json"), ",")
		if name != key {
			t.Errorf("codegraphv1.Import.%s json tag = %q, parser key shared.ImportFlag* = %q: the projector would read the flag under a name the parsers never write", field, name, key)
		}
	}
}

// TestParserFlagKeysReachTheImportEdgeFold drives each parser flag key, spelled
// with the shared constant a parser uses, through the real materialization path
// and requires it to land on the matching edge property. It is the end-to-end
// half of the key contract above: the tag check proves the names agree, this
// proves a payload written with the parser's constant is actually read.
func TestParserFlagKeysReachTheImportEdgeFold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key                                      string
		wantTypeOnly, wantDeferred, wantInferred bool
	}{
		{key: shared.ImportFlagTypeOnly, wantTypeOnly: true},
		{key: shared.ImportFlagDeferred, wantDeferred: true},
		{key: shared.ImportFlagInferred, wantInferred: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()

			result, quarantined := BuildMaterialization(testScope(), testGeneration(), []facts.Envelope{
				importRepositoryFact(),
				fileFactWithImports("f-1", "pkg/a.py", "python", []map[string]any{
					{"name": "X", "source": "./m", "line_number": 1, tt.key: true},
				}),
			})
			if len(quarantined) != 0 {
				t.Fatalf("quarantined = %d, want 0", len(quarantined))
			}
			if len(result.Imports) != 1 {
				t.Fatalf("len(Imports) = %d, want 1", len(result.Imports))
			}
			row := result.Imports[0]
			if row.TypeOnly != tt.wantTypeOnly || row.Deferred != tt.wantDeferred || row.Inferred != tt.wantInferred {
				t.Fatalf("payload key %q gave flags type_only:%v deferred:%v inferred:%v, want type_only:%v deferred:%v inferred:%v",
					tt.key, row.TypeOnly, row.Deferred, row.Inferred, tt.wantTypeOnly, tt.wantDeferred, tt.wantInferred)
			}
		})
	}
}
