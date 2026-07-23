// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestNornicDBIncomingOneHopCypherSeedsExactTarget(t *testing.T) {
	t.Parallel()

	for _, property := range []string{"uid", "id"} {
		property := property
		t.Run(property, func(t *testing.T) {
			t.Parallel()

			const entityID = "content-entity:target"
			cypher, params := nornicDBOneHopRelationshipsCypher(
				entityID,
				"incoming",
				"CALLS",
				"Function",
				property,
			)

			wantMatch := "MATCH (e:Function {" + property + ": $entity_id})<-[rel:CALLS]-(source)"
			if !strings.Contains(cypher, wantMatch) {
				t.Fatalf("cypher missing indexed target-first incoming match %q:\n%s", wantMatch, cypher)
			}
			if strings.Contains(cypher, "MATCH (source)-[rel:CALLS]->") {
				t.Fatalf("cypher retains source-first incoming traversal:\n%s", cypher)
			}
			// The relationship core read must carry NO OPTIONAL MATCH: on the
			// pinned NornicDB build a trailing OPTIONAL MATCH corrupts every
			// function-call projection (type(rel), coalesce, head(labels)) to
			// literal text. File/repo metadata is enriched separately.
			if strings.Contains(cypher, "OPTIONAL MATCH") {
				t.Fatalf("relationship core cypher must not contain OPTIONAL MATCH:\n%s", cypher)
			}
			for _, fragment := range []string{
				"type(rel) as type",
				"coalesce(source.id, source.uid) as source_id",
				"coalesce(source.id, source.uid) as source_entity_uid",
				"coalesce(e.id, e.uid) as target_entity_uid",
				"head(labels(source)) as source_type",
				"LIMIT $row_limit",
			} {
				if !strings.Contains(cypher, fragment) {
					t.Fatalf("cypher missing core projection %q:\n%s", fragment, cypher)
				}
			}
			for _, forbidden := range []string{
				property + " STARTS WITH $entity_id",
				property + " CONTAINS $entity_id",
				"source <> e",
				"source.uid <> e.uid",
				"sourceRepo.id as source_repo_id",
				"targetRepo.id as target_repo_id",
			} {
				if strings.Contains(cypher, forbidden) {
					t.Fatalf("cypher contains forbidden fragment %q:\n%s", forbidden, cypher)
				}
			}
			if got, want := params, map[string]any{"entity_id": entityID, "row_limit": nornicDBRelationshipFetchLimit}; !reflect.DeepEqual(got, want) {
				t.Fatalf("params = %#v, want exact identity params %#v", got, want)
			}
		})
	}
}

func TestNornicDBIncomingOneHopRelationshipsPreservesRowBehavior(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		rowsByID    map[string][]map[string]any
		wantLookups []string
		wantRows    int
	}{
		{
			name: "positive uid",
			rowsByID: map[string][]map[string]any{
				"uid": {{
					"direction": "incoming",
					"type":      "CALLS",
					"source_id": "content-entity:caller",
					"target_id": "content-entity:target",
				}},
			},
			wantLookups: []string{"uid"},
			wantRows:    1,
		},
		{
			name:        "empty",
			rowsByID:    map[string][]map[string]any{},
			wantLookups: []string{"uid", "id"},
			wantRows:    0,
		},
		{
			name: "id only",
			rowsByID: map[string][]map[string]any{
				"id": {{
					"direction": "incoming",
					"type":      "CALLS",
					"source_id": "content-entity:caller",
					"target_id": "content-entity:target",
				}},
			},
			wantLookups: []string{"uid", "id"},
			wantRows:    1,
		},
		{
			name: "duplicate rows remain duplicate",
			rowsByID: map[string][]map[string]any{
				"uid": {
					{
						"direction": "incoming",
						"type":      "CALLS",
						"source_id": "content-entity:caller",
						"target_id": "content-entity:target",
					},
					{
						"direction": "incoming",
						"type":      "CALLS",
						"source_id": "content-entity:caller",
						"target_id": "content-entity:target",
					},
				},
			},
			wantLookups: []string{"uid"},
			wantRows:    2,
		},
		{
			name: "recursive self edge remains visible",
			rowsByID: map[string][]map[string]any{
				"uid": {{
					"direction": "incoming",
					"type":      "CALLS",
					"source_id": "content-entity:target",
					"target_id": "content-entity:target",
				}},
			},
			wantLookups: []string{"uid"},
			wantRows:    1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var lookups []string
			handler := &CodeHandler{Neo4j: fakeGraphReader{
				run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
					if got, want := params["entity_id"], "content-entity:target"; got != want {
						t.Fatalf("params[entity_id] = %#v, want exact id %#v", got, want)
					}
					for _, property := range []string{"uid", "id"} {
						match := "MATCH (e:Function {" + property + ": $entity_id})<-[rel:CALLS]-(source)"
						if strings.Contains(cypher, match) {
							lookups = append(lookups, property)
							return tt.rowsByID[property], nil
						}
					}
					t.Fatalf("unexpected incoming Cypher shape:\n%s", cypher)
					return nil, nil
				},
			}}

			got, _, err := handler.nornicDBOneHopRelationships(
				context.Background(),
				"content-entity:target",
				"incoming",
				"CALLS",
				"Function",
			)
			if err != nil {
				t.Fatalf("nornicDBOneHopRelationships() error = %v, want nil", err)
			}
			if !reflect.DeepEqual(lookups, tt.wantLookups) {
				t.Fatalf("lookups = %#v, want %#v", lookups, tt.wantLookups)
			}
			if len(got) != tt.wantRows {
				t.Fatalf("len(rows) = %d, want %d; rows=%#v", len(got), tt.wantRows, got)
			}
			if tt.wantRows > 0 {
				want := tt.rowsByID[tt.wantLookups[len(tt.wantLookups)-1]]
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("rows = %#v, want unchanged rows %#v", got, want)
				}
			}
		})
	}
}

func TestNornicDBRelationshipsGraphRowDoesNotMutateMetadataRow(t *testing.T) {
	t.Parallel()

	metadataRow := map[string]any{
		"id":        "function-1",
		"name":      "handlePayment",
		"labels":    []any{"Function"},
		"file_path": "src/payments.go",
		"repo_id":   "repo-1",
	}
	handler := &CodeHandler{
		GraphBackend: GraphBackendNornicDB,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "<-[:CONTAINS]-(f:File)"):
					return []map[string]any{metadataRow}, nil
				case strings.Contains(cypher, "-[rel:CALLS]->(target)"):
					return []map[string]any{{
						"direction":   "outgoing",
						"type":        "CALLS",
						"target_name": "chargeCard",
						"target_id":   "function-2",
					}}, nil
				case strings.Contains(cypher, "MATCH (e:Function {uid: $entity_id})<-[rel:CALLS]-(source)"):
					return []map[string]any{{
						"direction":   "incoming",
						"type":        "CALLS",
						"source_name": "authorizePayment",
						"source_id":   "function-0",
					}}, nil
				default:
					t.Fatalf("unexpected cypher: %q", cypher)
				}
				return nil, nil
			},
		},
	}

	got, err := handler.nornicDBRelationshipsGraphRow(context.Background(), "", "handlePayment", "", "", "CALLS")
	if err != nil {
		t.Fatalf("nornicDBRelationshipsGraphRow() error = %v, want nil", err)
	}
	if got == nil {
		t.Fatal("nornicDBRelationshipsGraphRow() = nil, want row")
	}
	if _, ok := metadataRow["outgoing"]; ok {
		t.Fatalf("metadataRow[outgoing] = %#v, want absent", metadataRow["outgoing"])
	}
	if _, ok := metadataRow["incoming"]; ok {
		t.Fatalf("metadataRow[incoming] = %#v, want absent", metadataRow["incoming"])
	}
	if outgoing := mapRelationships(got["outgoing"]); len(outgoing) != 1 {
		t.Fatalf("got[outgoing] = %#v, want one relationship", got["outgoing"])
	}
	if incoming := mapRelationships(got["incoming"]); len(incoming) != 1 {
		t.Fatalf("got[incoming] = %#v, want one relationship", got["incoming"])
	}
}

func TestNornicDBGraphLabelForContentEntityTypeStaysAlignedWithGraphLabels(t *testing.T) {
	t.Parallel()

	labels := []string{
		"Annotation",
		"Function",
		"Class",
		"Interface",
		"Module",
		"Variable",
		"Struct",
		"Enum",
		"Union",
		"Macro",
		"ImplBlock",
		"Typedef",
		"TypeAlias",
		"TypeAnnotation",
		"Component",
		"TerraformModule",
		"TerragruntConfig",
		"TerragruntDependency",
	}
	for _, label := range labels {
		label := label
		t.Run(label, func(t *testing.T) {
			t.Parallel()

			if got, want := nornicDBGraphLabelForContentEntityType(label), graphLabelToContentEntityType(label); got != want {
				t.Fatalf("nornicDBGraphLabelForContentEntityType(%q) = %q, want shared graph label %q", label, got, want)
			}
		})
	}
	if got := nornicDBGraphLabelForContentEntityType(" Protocol "); got != "" {
		t.Fatalf("nornicDBGraphLabelForContentEntityType(%q) = %q, want empty unsupported label", " Protocol ", got)
	}
}

func TestNornicDBOneHopRelationshipsCypherUsesIndexedEntityLookup(t *testing.T) {
	t.Parallel()

	cypher, params := nornicDBOneHopRelationshipsCypher("content-entity:handleRelationships", "outgoing", "CALLS", "Function", "uid")

	if !strings.Contains(cypher, "MATCH (e:Function {uid: $entity_id})-[rel:CALLS]->(target)") {
		t.Fatalf("cypher = %q, want single-match indexed outgoing relationship lookup", cypher)
	}
	if strings.Contains(cypher, graphEntityIDPredicate("e", "$entity_id")) {
		t.Fatalf("cypher = %q, must not use broad entity-id OR predicate on NornicDB", cypher)
	}
	if got, want := params, map[string]any{"entity_id": "content-entity:handleRelationships", "row_limit": nornicDBRelationshipFetchLimit}; !reflect.DeepEqual(got, want) {
		t.Fatalf("params = %#v, want %#v", got, want)
	}
}

func TestNornicDBOneHopRelationshipsFallsBackFromUIDToID(t *testing.T) {
	t.Parallel()

	var lookups []string
	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "MATCH (e:Function {uid: $entity_id})"):
					lookups = append(lookups, "uid")
					return []map[string]any{}, nil
				case strings.Contains(cypher, "MATCH (e:Function {id: $entity_id})"):
					lookups = append(lookups, "id")
					return []map[string]any{{
						"direction":   "outgoing",
						"type":        "CALLS",
						"target_name": "filterRelationshipResponse",
						"target_id":   "content-entity:filterRelationshipResponse",
					}}, nil
				default:
					t.Fatalf("unexpected cypher: %q", cypher)
				}
				return nil, nil
			},
		},
	}

	got, _, err := handler.nornicDBOneHopRelationships(
		context.Background(),
		"content-entity:handleRelationships",
		"outgoing",
		"CALLS",
		"Function",
	)
	if err != nil {
		t.Fatalf("nornicDBOneHopRelationships() error = %v, want nil", err)
	}
	if want := []string{"uid", "id"}; !reflect.DeepEqual(lookups, want) {
		t.Fatalf("lookups = %#v, want %#v", lookups, want)
	}
	if len(got) != 1 {
		t.Fatalf("nornicDBOneHopRelationships() = %#v, want one relationship", got)
	}
	if gotName, want := got[0]["target_name"], "filterRelationshipResponse"; gotName != want {
		t.Fatalf("relationship[target_name] = %#v, want %#v", gotName, want)
	}
}
