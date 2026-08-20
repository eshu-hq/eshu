// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// CodeCallFamilyLocalPath is the code-call family's repository local_path.
// Exported (#6053) because sibling-identity collision guards in the
// materializededges subpackage compare against it: a frozen copy there sits on
// the REFERENCE side of the assertion and stops detecting the collision it
// exists to catch, which is the shape that made three neighbouring constants
// fail open before this was noticed.
const CodeCallFamilyLocalPath = "/repo-code-calls"

// CodeCallFamilyGenerationID is the code-call family's generation ID. Exported
// (#6053) so the sibling-identity collision guards in materializededges compare
// against one value: a copy there sits on the reference side of the assertion
// and stops detecting the collision it exists to catch.
const CodeCallFamilyGenerationID = "gen-ifa-code-call-family-1"

// codeCallFamilyOdu returns the binary-portable catalog representation of the
// code_calls cassette. The checked-in cassette remains the live replay source;
// TestCodeCallFamilyIsCatalogedAndResolvable deeply pins this compiled literal
// to the same strict projection.
func codeCallFamilyOdu() CatalogOdu {
	sourceRunID := "run-ifa-code-call-family-1"
	localPath := CodeCallFamilyLocalPath
	factsForOdu := []facts.Envelope{
		codeCallCatalogRepositoryFact(codegraphv1.Repository{
			RepoID: "repo-ifa-code-call-family", SourceRunID: &sourceRunID, LocalPath: &localPath,
		}),
		codeCallCatalogFileFact(codegraphv1.File{
			RepoID: "repo-ifa-code-call-family", RelativePath: "app/calls.py", ParsedFileData: map[string]any{
				"path": "/repo-code-calls/app/calls.py",
				"functions": []any{
					codeCallCatalogEntity("ifaCcAlpha", "content-entity:e_4e5b59c348d9", 1, 10),
					codeCallCatalogEntity("ifaCcBeta", "content-entity:e_2b7f36111994", 12, 14),
				},
				"classes": []any{
					codeCallCatalogEntity("IfaCcGamma", "content-entity:e_8193b04a84bf", 16, 17),
					codeCallCatalogEntity("IfaCcDelta", "content-entity:e_807a1220c2fa", 19, 20),
					codeCallCatalogEntityWithMetaclass("IfaCcEpsilon", "content-entity:e_b2d33afd68dc", 22, 23, "IfaCcMeta"),
					codeCallCatalogEntity("IfaCcMeta", "content-entity:e_48ef8138ee01", 25, 26),
					codeCallCatalogEntityWithMetaclass("IfaCcZeta", "content-entity:e_8df6b0ebdf64", 28, 29, "IfaCcUnknownMeta"),
				},
				"function_calls": []any{
					map[string]any{"name": "ifaCcBeta", "line_number": float64(3), "lang": "python"},
					map[string]any{"name": "IfaCcGamma", "line_number": float64(4), "call_kind": "python.class_reference", "lang": "python"},
					map[string]any{"name": "IfaCcDelta", "line_number": float64(5), "call_kind": "constructor_call", "lang": "python"},
					map[string]any{"name": "ifaCcGhost", "line_number": float64(6), "lang": "python"},
					map[string]any{"name": "ifaCcBeta", "line_number": float64(0), "lang": "python"},
					map[string]any{"name": "ifaCcDup", "line_number": float64(7), "lang": "python"},
				},
			},
		}),
		codeCallCatalogFileFact(codegraphv1.File{
			RepoID: "repo-ifa-code-call-family", RelativePath: "app/dup_one.py", ParsedFileData: map[string]any{
				"path":      "/repo-code-calls/app/dup_one.py",
				"functions": []any{codeCallCatalogEntity("ifaCcDup", "content-entity:e_730bb649336f", 1, 2)},
			},
		}),
		codeCallCatalogFileFact(codegraphv1.File{
			RepoID: "repo-ifa-code-call-family", RelativePath: "app/dup_two.py", ParsedFileData: map[string]any{
				"path":      "/repo-code-calls/app/dup_two.py",
				"functions": []any{codeCallCatalogEntity("ifaCcDup", "content-entity:e_b1bd721e0dcd", 1, 2)},
			},
		}),
	}
	factsForOdu = append(factsForOdu,
		codeCallCatalogContentEntityFact("content-entity:e_4e5b59c348d9", "Function", "ifaCcAlpha", "app/calls.py", 1, 10),
		codeCallCatalogContentEntityFact("content-entity:e_2b7f36111994", "Function", "ifaCcBeta", "app/calls.py", 12, 14),
		codeCallCatalogContentEntityFact("content-entity:e_8193b04a84bf", "Class", "IfaCcGamma", "app/calls.py", 16, 17),
		codeCallCatalogContentEntityFact("content-entity:e_807a1220c2fa", "Class", "IfaCcDelta", "app/calls.py", 19, 20),
		codeCallCatalogContentEntityFact("content-entity:e_b2d33afd68dc", "Class", "IfaCcEpsilon", "app/calls.py", 22, 23),
		codeCallCatalogContentEntityFact("content-entity:e_48ef8138ee01", "Class", "IfaCcMeta", "app/calls.py", 25, 26),
		codeCallCatalogContentEntityFact("content-entity:e_8df6b0ebdf64", "Class", "IfaCcZeta", "app/calls.py", 28, 29),
		codeCallCatalogContentEntityFact("content-entity:e_730bb649336f", "Function", "ifaCcDup", "app/dup_one.py", 1, 2),
		codeCallCatalogContentEntityFact("content-entity:e_b1bd721e0dcd", "Function", "ifaCcDup", "app/dup_two.py", 1, 2),
		codeCallCatalogFact("shared_followup", "shared_followup:repo-ifa-code-call-family:code_call_materialization", map[string]any{
			"reducer_domain": "code_call_materialization", "entity_key": "repo:repo-ifa-code-call-family",
			"reason": "repository snapshot emitted code-call materialization follow-up", "repo_id": "repo-ifa-code-call-family",
		}),
	)
	return CatalogOdu{
		Odu:    Odu{Name: codeCallFamilyOduName, Facts: factsForOdu},
		Detail: "one code-call repository exercising CALLS, REFERENCES, INSTANTIATES, and USES_METACLASS plus unresolved, invalid-line, ambiguous, and unknown-metaclass exclusions",
	}
}

func codeCallCatalogFact(kind, stableKey string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		ScopeID: "scope-ifa-code-call-family", GenerationID: CodeCallFamilyGenerationID, FactKind: kind,
		StableFactKey: stableKey, SchemaVersion: "1.0.0", CollectorKind: "git",
		SourceConfidence: "observed", Payload: payload,
	}
}

func codeCallCatalogRepositoryFact(repository codegraphv1.Repository) facts.Envelope {
	payload, err := factschema.EncodeCodegraphRepository(repository)
	if err != nil {
		panic(fmt.Sprintf("ifa: encode code-call catalog repository %q: %v", repository.RepoID, err))
	}
	return codeCallCatalogFact(factschema.FactKindCodegraphRepository, "repository:"+repository.RepoID, payload)
}

func codeCallCatalogFileFact(file codegraphv1.File) facts.Envelope {
	payload, err := factschema.EncodeCodegraphFile(file)
	if err != nil {
		panic(fmt.Sprintf("ifa: encode code-call catalog file %q: %v", file.RelativePath, err))
	}
	return codeCallCatalogFact(factschema.FactKindCodegraphFile, "file:"+file.RepoID+":"+file.RelativePath, payload)
}

func codeCallCatalogContentEntityFact(id, entityType, name, relativePath string, startLine, endLine int) facts.Envelope {
	return codeCallCatalogFact("content_entity", "content_entity:"+id, map[string]any{
		"repo_id": "repo-ifa-code-call-family", "entity_id": id, "entity_type": entityType,
		"entity_name": name, "relative_path": relativePath, "start_line": float64(startLine), "end_line": float64(endLine),
	})
}

func codeCallCatalogEntity(name, uid string, startLine, endLine int) map[string]any {
	return map[string]any{"name": name, "uid": uid, "line_number": float64(startLine), "end_line": float64(endLine)}
}

func codeCallCatalogEntityWithMetaclass(name, uid string, startLine, endLine int, metaclass string) map[string]any {
	entity := codeCallCatalogEntity(name, uid, startLine, endLine)
	entity["metaclass"] = metaclass
	return entity
}
