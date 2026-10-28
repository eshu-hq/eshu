// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package node

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

type stubOverlayResolver struct {
	rows []KustomizeOverlayRow
	err  error
}

func (s stubOverlayResolver) ListKustomizeOverlays(_ context.Context, _ string) ([]KustomizeOverlayRow, error) {
	return s.rows, s.err
}

func kustomizeMaterialization() canonical.CanonicalMaterialization {
	return canonical.CanonicalMaterialization{
		GenerationID: "gen-11",
		RepoID:       "repo-1",
		Entities: []canonical.EntityRow{
			{
				EntityID: "overlay-app", Label: "KustomizeOverlay", FilePath: "overlays/app/kustomization.yaml",
				Metadata: map[string]any{"bases": []any{"../base", "missing"}},
			},
			{
				EntityID: "overlay-base", Label: "KustomizeOverlay", FilePath: "overlays/base/kustomization.yaml",
				Metadata: map[string]any{"bases": []any{}},
			},
			{EntityID: "other-1", Label: "Function", EntityName: "other", FilePath: "main.go"},
		},
	}
}

func TestKustomizeEdgePartsEmptyWithoutOverlays(t *testing.T) {
	t.Parallel()

	if parts := KustomizeEdgeParts(context.Background(), canonical.CanonicalMaterialization{}, stubOverlayResolver{}); parts != nil {
		t.Fatalf("parts = %v, want nil without touched or deleted overlays", parts)
	}
}

func TestKustomizeEdgePartsPropertyWriteWithoutResolver(t *testing.T) {
	t.Parallel()

	// A nil resolver still emits the base_refs property write (it needs
	// no live read); only the edge rebuild fails closed.
	parts := KustomizeEdgeParts(context.Background(), kustomizeMaterialization(), nil)
	if len(parts) != 1 {
		t.Fatalf("len(parts) = %d, want only the property write", len(parts))
	}
	if parts[0].IsRetract {
		t.Fatalf("property write part is marked retract: %+v", parts[0])
	}
	rows, ok := parts[0].Parameters["rows"].([]map[string]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("property rows = %v, want one per touched overlay", parts[0].Parameters["rows"])
	}
}

func TestKustomizeEdgePartsFailClosedOnResolverError(t *testing.T) {
	t.Parallel()

	resolver := stubOverlayResolver{err: errors.New("connection refused")}
	parts := KustomizeEdgeParts(context.Background(), kustomizeMaterialization(), resolver)
	// Fail closed: the property write survives, no retract or merge that
	// a partial read could poison.
	if len(parts) != 1 || parts[0].IsRetract {
		t.Fatalf("parts = %v, want only the property write on resolver error", parts)
	}
}

func TestKustomizeEdgePartsRebuildsEdges(t *testing.T) {
	t.Parallel()

	resolver := stubOverlayResolver{rows: []KustomizeOverlayRow{
		{UID: "overlay-persisted", Path: "overlays/legacy/kustomization.yaml", BaseRefs: []string{"../base"}},
	}}
	parts := KustomizeEdgeParts(context.Background(), kustomizeMaterialization(), resolver)
	// Property write + one Drain-marked retract + one EXTENDS_BASE merge.
	if len(parts) != 3 {
		t.Fatalf("len(parts) = %d, want 3 (property + retract + merge)", len(parts))
	}
	var retract, merge *Part
	for i := range parts {
		switch {
		case parts[i].IsRetract:
			retract = &parts[i]
		case strings.Contains(parts[i].Cypher, "EXTENDS_BASE"):
			merge = &parts[i]
		}
	}
	if retract == nil || !retract.Drain {
		t.Fatalf("no Drain-marked retract in %d parts", len(parts))
	}
	uids, ok := retract.Parameters["source_uids"].([]string)
	if !ok || len(uids) != 3 {
		t.Fatalf("retract source_uids = %v, want all three considered overlays", retract.Parameters["source_uids"])
	}
	if merge == nil {
		t.Fatalf("no EXTENDS_BASE merge in %d parts", len(parts))
	}
	rows, ok := merge.Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("merge rows = %T, want []map[string]any", merge.Parameters["rows"])
	}
	// app -> base (touched) and legacy -> base (persisted); the dangling
	// "missing" base resolves nowhere and is dropped, never MERGEd.
	if len(rows) != 2 {
		t.Fatalf("merge rows = %v, want exactly the two resolvable edges", rows)
	}
	for _, row := range rows {
		if row["target_uid"] != "overlay-base" {
			t.Fatalf("merge row = %v, want every edge to target the base overlay", row)
		}
	}
}

func TestKustomizeEdgePartsDropsDeletedTargets(t *testing.T) {
	t.Parallel()

	// A deleted file carries no entity (it is gone), so the base
	// survives only as a persisted resolver row until the deleted path
	// drops it: the app overlay's base ref then dangles and no merge
	// may invent a placeholder target.
	mat := canonical.CanonicalMaterialization{
		GenerationID: "gen-11",
		RepoID:       "repo-1",
		Entities: []canonical.EntityRow{
			{
				EntityID: "overlay-app", Label: "KustomizeOverlay", FilePath: "overlays/app/kustomization.yaml",
				Metadata: map[string]any{"bases": []any{"../base"}},
			},
		},
		DeltaDeletedFilePaths: []string{"overlays/base/kustomization.yaml", "README.md"},
	}
	resolver := stubOverlayResolver{rows: []KustomizeOverlayRow{
		{UID: "overlay-base", Path: "overlays/base/kustomization.yaml"},
	}}
	parts := KustomizeEdgeParts(context.Background(), mat, resolver)
	for _, part := range parts {
		if !part.IsRetract && strings.Contains(part.Cypher, "EXTENDS_BASE") {
			rows := part.Parameters["rows"].([]map[string]any)
			if len(rows) != 0 {
				t.Fatalf("merge rows = %v, want none with the base deleted", rows)
			}
		}
	}
	if got := countPartsByFlag(parts, true); got != 1 {
		t.Fatalf("retract parts = %d, want 1", got)
	}
}

func TestKustomizeEdgePartsRejectsAboveRootRef(t *testing.T) {
	t.Parallel()

	mat := canonical.CanonicalMaterialization{
		GenerationID: "gen-11",
		RepoID:       "repo-1",
		Entities: []canonical.EntityRow{
			{
				EntityID: "overlay-top", Label: "KustomizeOverlay", FilePath: "kustomization.yaml",
				Metadata: map[string]any{"bases": []any{"../escape"}},
			},
		},
	}
	parts := KustomizeEdgeParts(context.Background(), mat, stubOverlayResolver{})
	for _, part := range parts {
		if !part.IsRetract && strings.Contains(part.Cypher, "EXTENDS_BASE") {
			t.Fatalf("unexpected merge for an above-root base ref: %v", part.Parameters)
		}
	}
}

func TestKustomizeOverlayDirToUIDPrefersSmallest(t *testing.T) {
	t.Parallel()

	byUID := map[string]KustomizeOverlayRow{
		"uid-b": {UID: "uid-b", Path: "overlays/app/kustomization.yaml"},
		"uid-a": {UID: "uid-a", Path: "overlays/app/kustomization.yml"},
	}
	got := kustomizeOverlayDirToUID(byUID)
	if got["overlays/app"] != "uid-a" {
		t.Fatalf("dir winner = %q, want the lexicographically smallest uid", got["overlays/app"])
	}
}

func TestKustomizeBaseDirectory(t *testing.T) {
	t.Parallel()

	dir, ok := kustomizeBaseDirectory("overlays/app/kustomization.yaml", "../base")
	if !ok || dir != "overlays/base" {
		t.Fatalf("kustomizeBaseDirectory() = (%q, %v), want (overlays/base, true)", dir, ok)
	}
	dir, ok = kustomizeBaseDirectory("overlays/app/kustomization.yaml", ".")
	if !ok || dir != "overlays/app" {
		t.Fatalf("kustomizeBaseDirectory(self) = (%q, %v), want (overlays/app, true)", dir, ok)
	}
	if _, ok = kustomizeBaseDirectory("kustomization.yaml", "../escape"); ok {
		t.Fatal("kustomizeBaseDirectory(above root) = ok, want rejection")
	}
}

func TestKustomizePathPredicates(t *testing.T) {
	t.Parallel()

	if !isKustomizationFilePath("overlays/app/Kustomization.YAML") {
		t.Fatal("isKustomizationFilePath() = false, want case-insensitive match")
	}
	if isKustomizationFilePath("overlays/app/deployment.yaml") {
		t.Fatal("isKustomizationFilePath(deployment) = true, want false")
	}
	if got := kustomizeOverlayDirectory("kustomization.yaml"); got != "" {
		t.Fatalf("kustomizeOverlayDirectory(root) = %q, want empty", got)
	}
	if got := kustomizeOverlayDirectory("overlays/app/kustomization.yaml"); got != "overlays/app" {
		t.Fatalf("kustomizeOverlayDirectory() = %q, want overlays/app", got)
	}
	deleted := kustomizeOverlayDeletedFilePaths([]string{"a/kustomization.yml", "b/main.go"})
	if !slices.Equal(deleted, []string{"a/kustomization.yml"}) {
		t.Fatalf("deleted kustomization paths = %v, want only the kustomization file", deleted)
	}
}

func TestKustomizeMetadataStringSlice(t *testing.T) {
	t.Parallel()

	if got := kustomizeMetadataStringSlice(nil, "bases"); got != nil {
		t.Fatalf("nil metadata = %v, want nil", got)
	}
	if got := kustomizeMetadataStringSlice(map[string]any{}, "bases"); got != nil {
		t.Fatalf("missing key = %v, want nil", got)
	}
	if got := kustomizeMetadataStringSlice(map[string]any{"bases": "../base"}, "bases"); got != nil {
		t.Fatalf("scalar value = %v, want nil (only lists coerce)", got)
	}
	want := []string{"a", "b"}
	if got := kustomizeMetadataStringSlice(map[string]any{"bases": []string{" a ", "", "b"}}, "bases"); !slices.Equal(got, want) {
		t.Fatalf("[]string coercion = %v, want %v", got, want)
	}
	if got := kustomizeMetadataStringSlice(map[string]any{"bases": []any{"a", 42, " ", "b"}}, "bases"); !slices.Equal(got, want) {
		t.Fatalf("[]any coercion = %v, want %v (non-strings skipped)", got, want)
	}
}

func TestKustomizeRetractSourceUIDs(t *testing.T) {
	t.Parallel()

	got := kustomizeRetractSourceUIDs(
		[]KustomizeOverlayRow{{UID: "a"}, {UID: ""}, {UID: "b"}},
		[]KustomizeOverlayRow{{UID: "b"}, {UID: "c"}},
	)
	if !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("retract uids = %v, want deduplicated persisted-then-touched order", got)
	}
}

func TestKustomizeOverlayBaseRefPropertyRows(t *testing.T) {
	t.Parallel()

	if rows := kustomizeOverlayBaseRefPropertyRows(nil); rows != nil {
		t.Fatalf("rows = %v, want nil without touched overlays", rows)
	}
	rows := kustomizeOverlayBaseRefPropertyRows([]KustomizeOverlayRow{{UID: "u1"}})
	if len(rows) != 1 {
		t.Fatalf("rows = %v, want one row", rows)
	}
	refs, ok := rows[0]["base_refs"].([]string)
	if !ok || len(refs) != 0 {
		t.Fatalf("nil base_refs = %v, want an explicit empty list", rows[0]["base_refs"])
	}
}

func TestEntityMetadataProperties(t *testing.T) {
	t.Parallel()

	if got := EntityMetadataProperties(nil); got != nil {
		t.Fatalf("nil row = %v, want nil", got)
	}
	if got := EntityMetadataProperties(map[string]any{"entity_metadata": "nope"}); got != nil {
		t.Fatalf("non-map metadata = %v, want nil", got)
	}
	got := EntityMetadataProperties(map[string]any{"entity_metadata": map[string]any{
		"custom":     " value ",
		"uid":        "reserved-off-graph",
		"  ":         "blank-key",
		"complex":    map[string]any{"nested": true},
		"active":     true,
		"line":       12,
		"ratio":      0.5,
		"tags":       []string{"a", " ", "b"},
		"empty_tags": []string{"  "},
		"blank":      "   ",
	}})
	want := map[string]any{
		"custom": "value",
		"active": true,
		"line":   12,
		"ratio":  0.5,
		"tags":   []string{"a", "b"},
	}
	if len(got) != len(want) {
		t.Fatalf("properties = %v, want %v", got, want)
	}
	if got["custom"] != "value" || got["active"] != true || got["line"] != 12 || got["ratio"] != 0.5 {
		t.Fatalf("scalar properties = %v, want trimmed and typed values", got)
	}
	if tags, ok := got["tags"].([]string); !ok || !slices.Equal(tags, []string{"a", "b"}) {
		t.Fatalf("tags = %v, want [a b]", got["tags"])
	}
}

func TestGraphPropertyValue(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value any
		want  any
		ok    bool
	}{
		{name: "String", value: " x ", want: "x", ok: true},
		{name: "BlankString", value: "  ", ok: false},
		{name: "Bool", value: true, want: true, ok: true},
		{name: "Int", value: 3, want: 3, ok: true},
		{name: "Int8", value: int8(3), want: 3, ok: true},
		{name: "Int16", value: int16(3), want: 3, ok: true},
		{name: "Int32", value: int32(3), want: 3, ok: true},
		{name: "Int64", value: int64(9), want: int64(9), ok: true},
		{name: "Float32", value: float32(1.5), want: float32(1.5), ok: true},
		{name: "Float", value: 1.5, want: 1.5, ok: true},
		{name: "StringList", value: []string{"a"}, want: []string{"a"}, ok: true},
		{name: "EmptyStringList", value: []string{" "}, ok: false},
		{name: "AnyList", value: []any{"a", 1}, want: []string{"a"}, ok: true},
		{name: "Map", value: map[string]any{}, ok: false},
		{name: "Nil", value: nil, ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := GraphPropertyValue(tc.value)
			if ok != tc.ok {
				t.Fatalf("GraphPropertyValue(%v) ok = %v, want %v", tc.value, ok, tc.ok)
			}
			if ok {
				switch want := tc.want.(type) {
				case []string:
					gotList, isList := got.([]string)
					if !isList || !slices.Equal(gotList, want) {
						t.Fatalf("GraphPropertyValue(%v) = %v, want %v", tc.value, got, want)
					}
				case nil:
				default:
					if got != want {
						t.Fatalf("GraphPropertyValue(%v) = %v, want %v", tc.value, got, want)
					}
				}
			}
		})
	}
}

func TestTypeScriptClassFamilyMetadata(t *testing.T) {
	t.Parallel()

	// The production caller (canonical_node_writer_entities.go) puts
	// language and label at the row top level, with only the entity
	// fields inside entity_metadata.
	row := func(language, label string, metadata map[string]any) map[string]any {
		return map[string]any{"entity_metadata": metadata, "language": language, "label": label}
	}
	if got := TypeScriptClassFamilyMetadata(nil); got != nil {
		t.Fatalf("nil row = %v, want nil", got)
	}
	ts := map[string]any{"decorators": []any{"Component", 7}, "declaration_merge_count": 2}
	got := TypeScriptClassFamilyMetadata(row("typescript", "Class", ts))
	if got["declaration_merge_count"] != 2 {
		t.Fatalf("merge count = %v, want 2", got["declaration_merge_count"])
	}
	if decorators, ok := got["decorators"].([]string); !ok || !slices.Equal(decorators, []string{"Component"}) {
		t.Fatalf("decorators = %v, want [Component]", got["decorators"])
	}
	merged := map[string]any{
		"type_parameters":         []string{"T"},
		"declaration_merge_group": "group-1",
		"declaration_merge_kinds": []any{"Class", "Interface"},
	}
	got = TypeScriptClassFamilyMetadata(row("typescript", "Enum", merged))
	if got["declaration_merge_group"] != "group-1" {
		t.Fatalf("merge group = %v, want group-1", got["declaration_merge_group"])
	}
	if params, ok := got["type_parameters"].([]string); !ok || !slices.Equal(params, []string{"T"}) {
		t.Fatalf("type_parameters = %v, want [T]", got["type_parameters"])
	}
	if kinds, ok := got["declaration_merge_kinds"].([]string); !ok || !slices.Equal(kinds, []string{"Class", "Interface"}) {
		t.Fatalf("merge kinds = %v, want [Class Interface]", got["declaration_merge_kinds"])
	}
	if _, present := got["decorators"]; present {
		t.Fatalf("decorators present in %v, want absent without decorator metadata", got)
	}
	// Each rejection below carries decorator metadata so the nil comes
	// from the intended gate, not the empty-metadata early return.
	decorated := map[string]any{"decorators": []string{"Component"}}
	if got := TypeScriptClassFamilyMetadata(row("go", "Class", decorated)); got != nil {
		t.Fatalf("non-TypeScript row = %v, want nil", got)
	}
	if got := TypeScriptClassFamilyMetadata(row("typescript", "Function", decorated)); got != nil {
		t.Fatalf("non-class label = %v, want nil", got)
	}
	if got := TypeScriptClassFamilyMetadata(row("typescript", "Class", map[string]any{"unrelated": "x"})); got != nil {
		t.Fatalf("field-less class row = %v, want nil (nothing to project)", got)
	}
	if got := TypeScriptClassFamilyMetadata(row("tsx", "Interface", map[string]any{"declaration_merge_group": "g"})); got["declaration_merge_group"] != "g" {
		t.Fatalf("tsx Interface row = %v, want the group (tsx counts as TypeScript)", got)
	}
}

func TestMetadataScalars(t *testing.T) {
	t.Parallel()

	if got := MetadataString(map[string]any{"k": " v "}, "k"); got != "v" {
		t.Fatalf("MetadataString() = %q, want trimmed v", got)
	}
	if got := MetadataString(map[string]any{"k": 42}, "k"); got != "" {
		t.Fatalf("MetadataString(non-string) = %q, want empty", got)
	}
	if got := MetadataString(map[string]any{}, "missing"); got != "" {
		t.Fatalf("MetadataString(missing) = %q, want empty", got)
	}
	if got := MetadataStringSlice(map[string]any{"k": []string{"a", " "}}, "k"); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("MetadataStringSlice() = %v, want [a]", got)
	}
	if got := MetadataStringSlice(map[string]any{"k": 42}, "k"); got != nil {
		t.Fatalf("MetadataStringSlice(scalar) = %v, want nil", got)
	}
	if got := MetadataInt(map[string]any{"k": 3}, "k"); got != 3 {
		t.Fatalf("MetadataInt(int) = %d, want 3", got)
	}
	narrowCases := []struct {
		value any
		want  int
	}{
		{int8(1), 1}, {int16(2), 2}, {int32(3), 3}, {int64(4), 4}, {float32(5.5), 5},
	}
	for _, tc := range narrowCases {
		if got := MetadataInt(map[string]any{"k": tc.value}, "k"); got != tc.want {
			t.Fatalf("MetadataInt(%T) = %d, want %d", tc.value, got, tc.want)
		}
	}
	if got := MetadataInt(map[string]any{"k": 5.9}, "k"); got != 5 {
		t.Fatalf("MetadataInt(float) = %d, want truncation to 5", got)
	}
	if got := MetadataInt(map[string]any{"k": "x"}, "k"); got != 0 {
		t.Fatalf("MetadataInt(string) = %d, want 0", got)
	}
	if got := MetadataInt(map[string]any{}, "k"); got != 0 {
		t.Fatalf("MetadataInt(missing) = %d, want 0", got)
	}
}

func TestStringListHelpers(t *testing.T) {
	t.Parallel()

	if got := StringList([]string{" a ", "", "b"}); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("StringList() = %v, want [a b]", got)
	}
	if got := StringList([]string{" ", ""}); got != nil {
		t.Fatalf("StringList(blank) = %v, want nil", got)
	}
	if got := AnyStringList([]any{"a", 1, " ", "b"}); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("AnyStringList() = %v, want [a b]", got)
	}
	if got := AnyStringList([]any{1, 2}); got != nil {
		t.Fatalf("AnyStringList(non-strings) = %v, want nil", got)
	}
}

func TestSourceUIDCollectorsSkipBlanks(t *testing.T) {
	t.Parallel()

	if got := gitlabPipelineSourceUIDs([]map[string]any{{"source_uid": ""}, {"source_uid": "p1"}, {"source_uid": "p1"}}); !slices.Equal(got, []string{"p1"}) {
		t.Fatalf("gitlab pipeline uids = %v, want deduplicated non-blank", got)
	}
	if got := helmTemplateValueSourceUIDs([]map[string]any{{"source_uid": ""}, {"source_uid": "u1"}, {"source_uid": "u1"}}); !slices.Equal(got, []string{"u1"}) {
		t.Fatalf("helm usage uids = %v, want deduplicated non-blank", got)
	}
}

func TestHelmEdgePartsWithoutResolvingRows(t *testing.T) {
	t.Parallel()

	// Definitions exist but no usage name matches: rows stay empty and
	// no statement (not even a retract) is emitted.
	mat := helmMaterialization(false)
	mat.Entities[2].EntityName = "other.key"
	if parts := HelmTemplateValueEdgeParts(mat); parts != nil {
		t.Fatalf("parts = %v, want nil without a resolving usage", parts)
	}
}

func TestHelmEdgePartsSameUIDUsages(t *testing.T) {
	t.Parallel()

	// Two usages sharing one uid exercise the deterministic sort
	// tiebreak; only the name matching the definition resolves.
	mat := helmMaterialization(false)
	extra := mat.Entities[0]
	extra.EntityName = "aaa.first"
	mat.Entities = append(mat.Entities, extra)
	parts := HelmTemplateValueEdgeParts(mat)
	rows := mergeRows(t, parts, "HELM_VALUE_REFERENCE")
	if len(rows) != 1 || rows[0]["source_uid"] != "use-1" || rows[0]["target_uid"] != "def-1" {
		t.Fatalf("merge rows = %v, want only use-1 -> def-1", rows)
	}
}

func TestEntityMetadataAllReservedYieldsNil(t *testing.T) {
	t.Parallel()

	row := map[string]any{"entity_metadata": map[string]any{"uid": "u1", "repo_id": "r", "  ": "blank"}}
	if got := EntityMetadataProperties(row); got != nil {
		t.Fatalf("properties = %v, want nil when every key is reserved or blank", got)
	}
}

func TestKustomizeBaseDirectoryAtRoot(t *testing.T) {
	t.Parallel()

	dir, ok := kustomizeBaseDirectory("kustomization.yaml", ".")
	if !ok || dir != "" {
		t.Fatalf("kustomizeBaseDirectory(root self) = (%q, %v), want (\"\", true)", dir, ok)
	}
}

func TestKustomizeMetadataStringSliceBlankList(t *testing.T) {
	t.Parallel()

	if got := kustomizeMetadataStringSlice(map[string]any{"bases": []string{" ", ""}}, "bases"); got != nil {
		t.Fatalf("blank []string = %v, want nil", got)
	}
	if got := kustomizeMetadataStringSlice(map[string]any{"bases": []any{1, " "}}, "bases"); got != nil {
		t.Fatalf("blank []any = %v, want nil", got)
	}
}

func TestCodeCallRetractParts(t *testing.T) {
	t.Parallel()

	if got := codeCallRetractRelTypes("parser/code-calls"); got != "CALLS|REFERENCES|INSTANTIATES" {
		t.Fatalf("code-calls rel types = %q", got)
	}
	if got := codeCallRetractRelTypes("parser/python-metaclass"); got != "USES_METACLASS" {
		t.Fatalf("metaclass rel types = %q", got)
	}
	if got := codeCallRetractRelTypes("other"); got != "CALLS|REFERENCES|USES_METACLASS|INSTANTIATES" {
		t.Fatalf("default rel types = %q", got)
	}
	if got := CodeCallRetractSourceLabelsFor("parser/python-metaclass"); !slices.Equal(got, CodeCallMetaclassRetractSourceLabels) {
		t.Fatalf("metaclass labels = %v, want the narrow set", got)
	}
	if got := CodeCallRetractSourceLabelsFor("parser/code-calls"); !slices.Equal(got, CodeCallRetractSourceLabels) {
		t.Fatalf("default labels = %v, want the full set", got)
	}

	parts := BuildCodeCallRetractParts("repo_id", "repo_ids", []string{"r1"}, "parser/code-calls")
	if len(parts) != len(CodeCallRetractSourceLabels) {
		t.Fatalf("len(parts) = %d, want one per source label (%d)", len(parts), len(CodeCallRetractSourceLabels))
	}
	for _, part := range parts {
		if !part.IsRetract || part.Drain {
			t.Errorf("part %+v: retracts are IsRetract without Drain", part.Parameters)
		}
		if !strings.Contains(part.Cypher, "CALLS|REFERENCES|INSTANTIATES") {
			t.Errorf("part cypher %q misses the rel-type disjunction", part.Cypher)
		}
		if part.Parameters["repo_ids"] == nil || part.Parameters["evidence_source"] != "parser/code-calls" {
			t.Errorf("part parameters = %v, want scope values and evidence source", part.Parameters)
		}
	}
	meta := BuildCodeCallRetractParts("path", "paths", []string{"p"}, "parser/python-metaclass")
	if len(meta) != len(CodeCallMetaclassRetractSourceLabels) {
		t.Fatalf("len(metaclass parts) = %d, want the narrow fan-out", len(meta))
	}
}

func TestRetractEntityLabels(t *testing.T) {
	t.Parallel()

	labels := RetractEntityLabels()
	if len(labels) == 0 {
		t.Fatal("RetractEntityLabels() is empty, want every retract family represented")
	}
	if !slices.IsSorted(labels) {
		t.Fatal("RetractEntityLabels() is not sorted")
	}
	for _, want := range []string{"Function", "GitlabJob", "KustomizeOverlay", "AtlantisProject"} {
		if !slices.Contains(labels, want) {
			t.Errorf("RetractEntityLabels() misses %q: %v", want, labels)
		}
	}
	if len(labels) != len(slices.Compact(slices.Clone(labels))) {
		t.Fatal("RetractEntityLabels() contains duplicates")
	}
}
