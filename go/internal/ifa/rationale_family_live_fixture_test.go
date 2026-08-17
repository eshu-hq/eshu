// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

var (
	_ func(codegraphv1.Repository) facts.Envelope = rationaleFamilyRepositoryFact
	_ func(codegraphv1.File) facts.Envelope       = rationaleFamilyFileFact
)

func TestRationaleCanonicalTargetIDLiterals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		name string
		line int
		want string
	}{
		{rationaleFamilyChargePath, rationaleFamilyChargeName, rationaleFamilyChargeLine, "content-entity:e_763200c9adc3"},
		{rationaleFamilyInvoicePath, rationaleFamilyInvoiceName, rationaleFamilyInvoiceLine, "content-entity:e_2dc98238d686"},
		{rationaleFamilyRefundPath, "refund", 3, "content-entity:e_a6fb2b86785c"},
		{rationaleFamilyReconcilePath, "reconcile", 3, "content-entity:e_6861e03e94da"},
		{rationaleFamilyHealthcheckPath, "healthcheck", 1, "content-entity:e_02d9fec13a93"},
	}
	for _, test := range tests {
		got := content.CanonicalEntityID(rationaleFamilyRepoID, test.path, "Function", test.name, test.line)
		if got != test.want {
			t.Errorf("CanonicalEntityID(%q, %q, Function, %q, %d) = %q, want %q", rationaleFamilyRepoID, test.path, test.name, test.line, got, test.want)
		}
	}
}

func TestRationaleCassettePositivesAreParserReachablePython(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path     string
		name     string
		line     int
		source   string
		comments []map[string]any
	}{
		{
			path: rationaleFamilyChargePath, name: rationaleFamilyChargeName, line: rationaleFamilyChargeLine,
			source: "# WHY: Retries are capped at three to bound tail latency.\n" +
				"# NOTE: Retries are capped at three to bound tail latency.\n" +
				"# WHY: Retries are capped at three to bound tail latency.\n" +
				"# TODO:\ndef charge():\n    pass\n",
			comments: []map[string]any{
				{"kind": "WHY", "text": "Retries are capped at three to bound tail latency."},
				{"kind": "NOTE", "text": "Retries are capped at three to bound tail latency."},
				{"kind": "WHY", "text": "Retries are capped at three to bound tail latency."},
				{"kind": "TODO", "text": ""},
			},
		},
		{
			path: rationaleFamilyInvoicePath, name: rationaleFamilyInvoiceName, line: rationaleFamilyInvoiceLine,
			source: "# HACK: Invoices are immutable once issued.\n" +
				"def issue_invoice():\n    pass\n",
			comments: []map[string]any{{"kind": "HACK", "text": "Invoices are immutable once issued."}},
		},
		{path: rationaleFamilyRefundPath, name: "refund", line: 3, source: "# why: lowercase markers are ignored\n# CAVEAT: unsupported markers are ignored\ndef refund():\n    pass\n"},
		{path: rationaleFamilyReconcilePath, name: "reconcile", line: 3, source: "# FIXME: detached by a blank line\n\ndef reconcile():\n    pass\n"},
		{path: rationaleFamilyHealthcheckPath, name: "healthcheck", line: 1, source: "def healthcheck():\n    pass\n"},
	}
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine: %v", err)
	}
	odu := CatalogByName()[rationaleFamilyOduName]
	for _, test := range tests {
		test := test
		t.Run(test.path, func(t *testing.T) {
			repoRoot := t.TempDir()
			absolutePath := filepath.Join(repoRoot, filepath.FromSlash(test.path))
			if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
				t.Fatalf("mkdir parser fixture: %v", err)
			}
			if err := os.WriteFile(absolutePath, []byte(test.source), 0o600); err != nil {
				t.Fatalf("write parser fixture: %v", err)
			}
			parsed, err := engine.ParsePath(repoRoot, absolutePath, false, parser.Options{})
			if err != nil {
				t.Fatalf("parse reachable rationale source: %v", err)
			}
			parsedFunction := rationaleNamedParserItem(t, parsed, "functions", test.name)
			if got := parsedFunction["line_number"]; got != test.line {
				t.Fatalf("parser line_number = %#v, want %d", got, test.line)
			}
			if got := rationaleCommentMaps(parsedFunction["rationale_comments"]); !reflect.DeepEqual(got, test.comments) {
				t.Fatalf("parser rationale_comments = %#v, want %#v", got, test.comments)
			}

			entity := rationaleCatalogFactByPath(t, odu.Facts, "content_entity", test.path)
			if _, ok := entity.Payload["rationale_comments"]; ok {
				t.Fatal("reachable positive carries synthetic top-level rationale_comments; production nests parser metadata")
			}
			metadata, _ := entity.Payload["entity_metadata"].(map[string]any)
			if got, want := rationaleCommentMaps(metadata["rationale_comments"]), rationaleCommentMaps(parsedFunction["rationale_comments"]); !reflect.DeepEqual(got, want) {
				t.Fatalf("cassette comments = %#v, parser comments = %#v", got, want)
			}
			wantID := content.CanonicalEntityID(rationaleFamilyRepoID, test.path, "Function", test.name, test.line)
			if got := entity.Payload["entity_id"]; got != wantID {
				t.Fatalf("cassette entity_id = %#v, want parser-shaped canonical id %q", got, wantID)
			}

			file := rationaleCatalogFactByPath(t, odu.Facts, factschema.FactKindCodegraphFile, test.path)
			parsedFileFunction := rationaleNamedParserItem(t, file.Payload["parsed_file_data"].(map[string]any), "functions", test.name)
			if got, want := rationaleCommentMaps(parsedFileFunction["rationale_comments"]), rationaleCommentMaps(parsedFunction["rationale_comments"]); !reflect.DeepEqual(got, want) {
				t.Fatalf("typed file comments = %#v, parser comments = %#v", got, want)
			}
		})
	}
}

func TestRationaleCassetteEnqueuesMaterializationHandler(t *testing.T) {
	t.Parallel()
	factsForGeneration := loadRationaleCassetteFacts(t)

	stage := projector.ProjectWorkloadStage(factsForGeneration)
	if got, want := stage.SourceRunPairs[rationaleFamilyRepoID], rationaleFamilySourceRunID; got != want {
		t.Fatalf("rationale repository source run = %q, want %q", got, want)
	}
	if len(stage.Intents) != 1 {
		t.Fatalf("rationale cassette reducer intents = %d, want exactly 1", len(stage.Intents))
	}
	intent := stage.Intents[0]
	if intent.Domain != reducer.DomainRationaleMaterialization {
		t.Fatalf("rationale cassette intent domain = %q, want %q", intent.Domain, reducer.DomainRationaleMaterialization)
	}
	if intent.ScopeID != rationaleFamilyScopeID || intent.GenerationID != rationaleFamilyGenerationID {
		t.Fatalf("rationale cassette intent scope/generation = %q/%q, want %q/%q", intent.ScopeID, intent.GenerationID, rationaleFamilyScopeID, rationaleFamilyGenerationID)
	}
}

func TestRationaleCassetteCanonicalizesExpectedTargets(t *testing.T) {
	t.Parallel()
	factsForGeneration := loadRationaleCassetteFacts(t)
	expected, err := loadRationaleExpectedEdges(rationaleFamilyExpectedEdgesPath(repoRootDir(t)))
	if err != nil {
		t.Fatalf("loadRationaleExpectedEdges: %v", err)
	}

	entities := projector.ExtractEntityRows(factsForGeneration, rationaleFamilyRepoID, rationaleFamilyLocalPath)
	entitiesByID := make(map[string]projector.EntityRow, len(entities))
	for _, entity := range entities {
		entitiesByID[entity.EntityID] = entity
	}
	filePaths := rationaleCassetteFilePaths(t, factsForGeneration)
	for _, edge := range expected {
		entity, ok := entitiesByID[edge.TargetEntityID]
		if !ok {
			t.Errorf("expected EXPLAINS target %q was not emitted by canonical entity projection", edge.TargetEntityID)
			continue
		}
		if entity.Label != "Function" || entity.RepoID != edge.RepoID || entity.RelativePath != edge.TargetPath {
			t.Errorf("canonical target %q = label:%q repo:%q path:%q, want Function/%s/%s", edge.TargetEntityID, entity.Label, entity.RepoID, entity.RelativePath, edge.RepoID, edge.TargetPath)
		}
		if _, ok := filePaths[edge.TargetPath]; !ok {
			t.Errorf("expected EXPLAINS target %q has no typed file fact for %q", edge.TargetEntityID, edge.TargetPath)
		}
	}
}

func TestRationaleCassettePinsExactTwelveFactSeams(t *testing.T) {
	t.Parallel()
	envelopes := loadRationaleCassetteFacts(t)
	if len(envelopes) != 12 {
		t.Fatalf("rationale cassette facts = %d, want exact 12; adding or removing one proof seam must fail", len(envelopes))
	}

	counts := map[string]int{}
	for _, envelope := range envelopes {
		counts[envelope.FactKind]++
	}
	want := map[string]int{
		factschema.FactKindCodegraphRepository: 1,
		factschema.FactKindCodegraphFile:       5,
		"content_entity":                       5,
		"shared_followup":                      1,
	}
	for kind, wantCount := range want {
		if got := counts[kind]; got != wantCount {
			t.Errorf("rationale cassette %s facts = %d, want %d", kind, got, wantCount)
		}
	}
	if len(counts) != len(want) {
		t.Errorf("rationale cassette fact-kind inventory = %#v, want only %#v", counts, want)
	}
}

func TestRationaleCassetteCodegraphFactsRoundTrip(t *testing.T) {
	t.Parallel()
	counts := map[string]int{}
	for _, envelope := range loadRationaleCassetteFacts(t) {
		var (
			reencoded map[string]any
			err       error
		)
		switch envelope.FactKind {
		case factschema.FactKindCodegraphRepository:
			var repository codegraphv1.Repository
			repository, err = factschema.DecodeCodegraphRepository(factschema.Envelope{SchemaVersion: envelope.SchemaVersion, Payload: envelope.Payload})
			if err == nil {
				reencoded, err = factschema.EncodeCodegraphRepository(repository)
			}
		case factschema.FactKindCodegraphFile:
			var file codegraphv1.File
			file, err = factschema.DecodeCodegraphFile(factschema.Envelope{SchemaVersion: envelope.SchemaVersion, Payload: envelope.Payload})
			if err == nil {
				reencoded, err = factschema.EncodeCodegraphFile(file)
			}
		default:
			continue
		}
		if err != nil {
			t.Fatalf("typed round trip for %s %q: %v", envelope.FactKind, envelope.StableFactKey, err)
		}
		wantPayload := envelope.Payload
		if envelope.FactKind == factschema.FactKindCodegraphRepository {
			// imports_map is a deliberate open-object extension: Repository v1
			// documents why its map-of-arrays shape is not modeled by the typed
			// conformance subset. The native source lock still compares it exactly.
			wantPayload = make(map[string]any, len(envelope.Payload)-1)
			for key, value := range envelope.Payload {
				if key != "imports_map" {
					wantPayload[key] = value
				}
			}
		}
		if !reflect.DeepEqual(reencoded, wantPayload) {
			t.Errorf("typed round trip changed %s %q", envelope.FactKind, envelope.StableFactKey)
		}
		counts[envelope.FactKind]++
	}
	if counts[factschema.FactKindCodegraphRepository] != 1 || counts[factschema.FactKindCodegraphFile] != 5 {
		t.Fatalf("typed fact counts = repository:%d file:%d, want 1/5", counts[factschema.FactKindCodegraphRepository], counts[factschema.FactKindCodegraphFile])
	}
}

func loadRationaleCassetteFacts(t *testing.T) []facts.Envelope {
	t.Helper()
	source, err := cassette.NewSource(filepath.Join(repoRootDir(t), rationaleFamilyCassetteRelPath))
	if err != nil {
		t.Fatalf("cassette.NewSource: %v", err)
	}
	generation, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("cassette source Next: %v", err)
	}
	if !ok {
		t.Fatal("rationale cassette source returned no generation")
	}

	var envelopes []facts.Envelope
	for envelope := range generation.Facts {
		envelopes = append(envelopes, envelope)
	}
	return envelopes
}

func rationaleCassetteFilePaths(t *testing.T, envelopes []facts.Envelope) map[string]struct{} {
	t.Helper()
	paths := map[string]struct{}{}
	for _, envelope := range envelopes {
		if envelope.FactKind != factschema.FactKindCodegraphFile {
			continue
		}
		file, err := factschema.DecodeCodegraphFile(factschema.Envelope{
			SchemaVersion: envelope.SchemaVersion,
			Payload:       envelope.Payload,
		})
		if err != nil {
			t.Fatalf("decode rationale file %q: %v", envelope.StableFactKey, err)
		}
		paths[file.RelativePath] = struct{}{}
	}
	return paths
}

func rationaleNamedParserItem(t *testing.T, payload map[string]any, bucket, name string) map[string]any {
	t.Helper()
	for _, item := range rationaleMapItems(payload[bucket]) {
		if item["name"] == name {
			return item
		}
	}
	t.Fatalf("%s %q not found in %#v", bucket, name, payload[bucket])
	return nil
}

func rationaleCatalogFactByPath(t *testing.T, envelopes []facts.Envelope, factKind, relativePath string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind == factKind && envelope.Payload["relative_path"] == relativePath {
			return envelope
		}
	}
	t.Fatalf("catalog has no %s fact for %q", factKind, relativePath)
	return facts.Envelope{}
}

func rationaleMapItems(value any) []map[string]any {
	switch items := value.(type) {
	case []map[string]any:
		return items
	case []any:
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	default:
		return nil
	}
}

func rationaleCommentMaps(value any) []map[string]any {
	return rationaleMapItems(value)
}
