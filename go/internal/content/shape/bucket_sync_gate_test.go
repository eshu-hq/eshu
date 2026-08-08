// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shape

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
)

// A content-entity bucket has to be registered in three independently
// hand-maintained places before it reaches the graph:
//
//  1. contentEntityBuckets here, the canonical bucket->label table;
//  2. snapshotEntityBuckets in go/internal/collector, the list
//     entityBucketsFromParsed walks to turn a parsed file into content_entity
//     facts;
//  3. entityTypeLabelMap in go/internal/projector, which gives the projected
//     node its canonical label.
//
// Missing (2) is silent and total. entityBucketsFromParsed only reads
// payload[bucket] for buckets in ITS list, so a bucket the parser fills but
// that list omits emits no fact, materializes no node, and fails nothing:
// parser tests prove parse->bucket, projector tests prove fact->node, and
// nothing in between exercises the collector's emission. #5483 C1 registered a
// bucket in (1) and (3) and missed the collector twin (2); only the live
// golden-corpus gate caught it, which is a slow and expensive way to learn
// about a typo (#5531).
//
// These tests read the three declarations out of their real source with go/ast
// rather than importing them — two are unexported in other packages, and
// parsing the source is also what makes the check immune to a same-package
// helper that "explains" a divergence. Three separate regex attempts at the
// same extraction produced three different wrong answers while this was being
// written, each stopping at the closing brace of the anonymous struct type
// instead of the slice; that is the argument for go/ast here, not neatness.

// knownBucketSyncDrift records divergences that exist in the tree today. These
// are NOT exemptions in the sense of "acceptable" — every entry is a bucket
// that does not reach the graph, or a label the projector cannot name. They are
// pinned so the gate can land and hold the line against NEW drift while the
// existing entries are fixed, which changes projected truth and needs its own
// golden-corpus proof.
//
// Removing an entry is the fix. Adding one needs a reason a reviewer accepts.
var knownBucketSyncDrift = map[string]string{
	"cloudformation_conditions":          "parser fills it (7 files) but the collector list omits it, so no fact and no node; the projector has no CloudFormationCondition label either",
	"cloudformation_cross_stack_exports": "same shape as cloudformation_conditions; no CloudFormationExport label in the projector",
	"cloudformation_cross_stack_imports": "same shape as cloudformation_conditions; no CloudFormationImport label in the projector",
	"terraform_blocks":                   "parser fills it (8 files) but the collector list omits it; no TerraformBlock label in the projector",
	"protocol_implementations":           "declared here and labelled by the projector, but no parser produces this bucket, so the collector omitting it costs nothing today",
}

// knownMissingProjectorLabels records labels this table declares that the
// projector cannot name. Same rule: an entry is a defect, not a licence.
var knownMissingProjectorLabels = map[string]string{
	"CloudFormationCondition": "see knownBucketSyncDrift[cloudformation_conditions]",
	"CloudFormationExport":    "see knownBucketSyncDrift[cloudformation_cross_stack_exports]",
	"CloudFormationImport":    "see knownBucketSyncDrift[cloudformation_cross_stack_imports]",
	"TerraformBlock":          "see knownBucketSyncDrift[terraform_blocks]",
	"PagerDutyDeclaration":    "emitted as a content entity (the collector DOES carry pagerduty_declarations) but the projector has no label for it, so the node cannot be canonically labelled",
}

// TestContentEntityBucketsMatchCollectorTwin is the gate: the canonical table
// and the collector's twin must carry the same bucket->label pairs.
func TestContentEntityBucketsMatchCollectorTwin(t *testing.T) {
	root := bucketSyncRepoRoot(t)

	canonical := parseBucketLabelSlice(t,
		filepath.Join(root, "go/internal/content/shape/materialize_tables.go"), "contentEntityBuckets")
	twin := parseBucketLabelSlice(t,
		filepath.Join(root, "go/internal/collector/git_snapshot_entity_buckets.go"), "snapshotEntityBuckets")

	if len(canonical) == 0 || len(twin) == 0 {
		t.Fatalf("extracted %d canonical and %d twin entries; an empty side means the parser lost the "+
			"declaration, not that the lists agree", len(canonical), len(twin))
	}

	for _, bucket := range sortedKeysOf(canonical) {
		if _, ok := twin[bucket]; ok {
			continue
		}
		if _, pinned := knownBucketSyncDrift[bucket]; pinned {
			continue
		}
		t.Errorf("bucket %q (label %q) is in contentEntityBuckets but not in the collector's "+
			"snapshotEntityBuckets: entityBucketsFromParsed will never read it out of a parsed file, so it "+
			"emits no content_entity fact and materializes no graph node — silently, with no test failure",
			bucket, canonical[bucket])
	}

	for _, bucket := range sortedKeysOf(twin) {
		if _, ok := canonical[bucket]; ok {
			continue
		}
		if _, pinned := knownBucketSyncDrift[bucket]; pinned {
			continue
		}
		t.Errorf("bucket %q (label %q) is in the collector's snapshotEntityBuckets but not in "+
			"contentEntityBuckets: the collector emits a fact this table cannot materialize", bucket, twin[bucket])
	}

	for _, bucket := range sortedKeysOf(canonical) {
		twinLabel, ok := twin[bucket]
		if !ok || twinLabel == canonical[bucket] {
			continue
		}
		t.Errorf("bucket %q maps to label %q here and %q in the collector; the node would be written "+
			"under one label and read under the other", bucket, canonical[bucket], twinLabel)
	}
}

// TestContentEntityLabelsHaveProjectorLabels covers the third list: a bucket
// can reach the collector and still produce a node the projector cannot name.
func TestContentEntityLabelsHaveProjectorLabels(t *testing.T) {
	root := bucketSyncRepoRoot(t)

	canonical := parseBucketLabelSlice(t,
		filepath.Join(root, "go/internal/content/shape/materialize_tables.go"), "contentEntityBuckets")
	projector := parseStringMapValues(t,
		filepath.Join(root, "go/internal/projector/canonical.go"), "entityTypeLabelMap")

	if len(projector) == 0 {
		t.Fatal("extracted 0 projector labels; the declaration moved or the parse failed")
	}

	for _, bucket := range sortedKeysOf(canonical) {
		for _, label := range materializedLabelsFor(canonical[bucket]) {
			if _, ok := projector[label]; ok {
				continue
			}
			if _, pinned := knownMissingProjectorLabels[label]; pinned {
				continue
			}
			t.Errorf("bucket %q materializes label %q, which entityTypeLabelMap does not name; the projected "+
				"node has no canonical label", bucket, label)
		}
	}
}

// materializedLabelsFor returns every label materializeEntities can actually
// write for a bucket whose table entry says tableLabel — the table value AND
// anything entityLabelForBucket rewrites it into.
//
// Checking only the table value is not enough. Production already rewrites a
// Module entity to ProtocolImplementation when its metadata says
// module_kind=protocol_implementation (materialize_labels.go), and that label
// reaches the projector without ever appearing in contentEntityBuckets. A
// future rewrite to a label entityTypeLabelMap lacks would make
// canonical_builder.go discard those entities silently, and a gate reading only
// the table would stay green (#5963 review, codex).
//
// The probes below drive the rewrite through its real function rather than
// restating its rules. That covers a rewrite keyed on the metadata these probes
// set; one keyed on a DIFFERENT metadata key needs its own probe added here,
// which is the residual this cannot close by construction.
func materializedLabelsFor(tableLabel string) []string {
	seen := map[string]struct{}{tableLabel: {}}
	for _, probe := range []Entity{
		{},
		{Metadata: map[string]any{"module_kind": "protocol_implementation"}},
	} {
		seen[entityLabelForBucket(tableLabel, probe)] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for label := range seen {
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

// TestBucketSyncDriftLedgerIsHonest keeps the ledgers from outliving the drift
// they describe. A pinned entry that no longer diverges is a stale licence, and
// the next real divergence with that name would be silently accepted.
func TestBucketSyncDriftLedgerIsHonest(t *testing.T) {
	root := bucketSyncRepoRoot(t)

	canonical := parseBucketLabelSlice(t,
		filepath.Join(root, "go/internal/content/shape/materialize_tables.go"), "contentEntityBuckets")
	twin := parseBucketLabelSlice(t,
		filepath.Join(root, "go/internal/collector/git_snapshot_entity_buckets.go"), "snapshotEntityBuckets")
	projector := parseStringMapValues(t,
		filepath.Join(root, "go/internal/projector/canonical.go"), "entityTypeLabelMap")

	for _, bucket := range sortedKeysOf(knownBucketSyncDrift) {
		_, inCanonical := canonical[bucket]
		_, inTwin := twin[bucket]
		if inCanonical && inTwin {
			t.Errorf("knownBucketSyncDrift[%q] is stale: the bucket is now in both lists. Delete the entry "+
				"so the gate protects it.", bucket)
		}
		if !inCanonical && !inTwin {
			t.Errorf("knownBucketSyncDrift[%q] names a bucket in neither list; delete the entry.", bucket)
		}
	}

	// An exemption expires two ways, and checking only the first leaves a
	// licence lying around: the projector may gain the label, OR the bucket
	// that produced it may be deleted. Without the second check a
	// TerraformBlock entry could outlive terraform_blocks, and silently cover a
	// reintroduced bucket that still has no projector mapping (#5963 review,
	// codex).
	materialized := make(map[string]struct{})
	for _, bucket := range sortedKeysOf(canonical) {
		for _, label := range materializedLabelsFor(canonical[bucket]) {
			materialized[label] = struct{}{}
		}
	}
	for _, label := range sortedKeysOf(knownMissingProjectorLabels) {
		if _, ok := projector[label]; ok {
			t.Errorf("knownMissingProjectorLabels[%q] is stale: the projector names it now. Delete the entry.", label)
		}
		if _, ok := materialized[label]; !ok {
			t.Errorf("knownMissingProjectorLabels[%q] is stale: no bucket in contentEntityBuckets "+
				"materializes that label any more, so the entry covers nothing and would silently "+
				"absorb the label's return. Delete the entry.", label)
		}
	}
}

// bucketSyncRepoRoot returns the repository root relative to this package.
func bucketSyncRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "specs", "ci-gates.v1.yaml")); err != nil {
		t.Fatalf("repo root %s does not look like the repository (%v)", root, err)
	}
	return root
}

// parseBucketLabelSlice extracts a `[]…{{bucket: "x", label: "Y"}, …}` slice
// literal declared as `var <name> = …` in path. It reads keyed fields by name,
// so it works for both the named-struct and anonymous-struct forms the two
// declarations use.
func parseBucketLabelSlice(t *testing.T, path, name string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	for _, elt := range varCompositeElements(t, path, name) {
		lit, ok := elt.(*ast.CompositeLit)
		if !ok {
			// A maintainer extracting an entry into a variable and putting the
			// identifier here would otherwise be skipped silently: the
			// production table still carries the bucket, but this gate would
			// never see it, so an omission in the collector twin would pass
			// (#5963 review, codex). Fail closed instead.
			t.Fatalf("%s in %s has a non-literal element (%T); this extractor only understands "+
				"composite literals, and skipping one would hide a bucket from every check here",
				name, path, elt)
		}
		var bucket, label string
		for _, field := range lit.Elts {
			kv, ok := field.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			value, ok := stringLit(kv.Value)
			if !ok {
				continue
			}
			switch key.Name {
			case "bucket":
				bucket = value
			case "label":
				label = value
			}
		}
		if bucket == "" || label == "" {
			t.Fatalf("%s in %s: element with bucket=%q label=%q — the declaration shape changed and this "+
				"gate would silently under-report", name, path, bucket, label)
		}
		if prior, dup := out[bucket]; dup {
			t.Errorf("%s in %s declares bucket %q twice (%q then %q)", name, path, bucket, prior, label)
		}
		out[bucket] = label
	}
	return out
}

// parseStringMapValues extracts the values of a `var <name> = map[string]string{…}`
// declaration, as a set.
func parseStringMapValues(t *testing.T, path, name string) map[string]struct{} {
	t.Helper()
	out := make(map[string]struct{})
	for _, elt := range varCompositeElements(t, path, name) {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if value, ok := stringLit(kv.Value); ok {
			out[value] = struct{}{}
		}
	}
	return out
}

// varCompositeElements returns the elements of the composite literal assigned to
// `var <name> = …` in path. It fails the test when the declaration is absent,
// so a rename cannot turn this gate into a silent no-op.
func varCompositeElements(t *testing.T, path, name string) []ast.Expr {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || value.Names[0].Name != name || len(value.Values) != 1 {
				continue
			}
			lit, ok := value.Values[0].(*ast.CompositeLit)
			if !ok {
				t.Fatalf("%s in %s is not a composite literal", name, path)
			}
			return lit.Elts
		}
	}
	t.Fatalf("%s not found in %s — it was renamed or moved, and this gate cannot check what it cannot find", name, path)
	return nil
}

// stringLit unwraps a basic string literal expression.
func stringLit(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

// sortedKeysOf returns a map's keys in a stable order.
func sortedKeysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
