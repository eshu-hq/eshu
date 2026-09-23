// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// The semantic Module cases pin what the production semantic-entity write does
// to :Module on each backend (#6965 Phase 4, root cause owned by #6968).
//
// The two backends receive different Cypher for the same write. The reducer
// wires a MATCH-first writer for Neo4j (sourcecypher.NewSemanticEntityWriter)
// and a MERGE-first writer for NornicDB
// (NewSemanticEntityWriterWithCanonicalNodeRows + WithLabelScopedRetract,
// go/cmd/reducer/neo4j_wiring.go). Module is not canonical-node-owned, so on
// NornicDB its template goes through semanticEntityMergeFirstRowsUpsertCypher,
// which moves `MATCH (f:File ...)` from before the node MERGE to just before
// the containment MERGE. That rewrite exists to keep NornicDB on its
// UNWIND/MERGE batch hot path; nothing documents it as a change of meaning.
//
// Expected outcome for a row whose File is absent: NO Module. Reasons:
//   - The per-label template (semanticModuleUpsertCypher) is the source of
//     truth; it is what the default writer and Neo4j run, and it only creates
//     the node for a row whose File exists. The merge-first form is a
//     rewrite of that template for planner shape, so it owes the same result
//     (#6965 Problem 3: "different statements, same semantics").
//   - A semantic Module with no File has no CONTAINS edge, and the :Module
//     orphan sweep deliberately skips uid-bearing nodes
//     (orphanSweepClassPredicate, `n.uid IS NULL`), so nothing but the next
//     repo retract would remove it.
//   - It collides with the import graph: the canonical
//     `MERGE (m:Module {name, lang})` matches any Module with that name and
//     language, so it binds to the stray semantic node instead of creating
//     its own uid-NULL node. That is the uid-NULL count divergence #6968
//     measured (NornicDB 406 vs Neo4j 413 on the orphan sweep).
//
// The cases capture the statements from the production writer through a
// recording executor, so they run the exact Cypher each backend receives,
// never a copy. A cmd/reducer test pins the mirrored writer construction here
// to the reducer's own wiring.

// Case names. They are constants because the guards look the cases up by name.
const (
	semanticModuleAbsentFileWriteCaseName  = "semantic module upsert with absent file"
	semanticModuleAbsentFileReadCaseName   = "semantic module with absent file is not created"
	semanticModuleFileSeedCaseName         = "semantic module containing file seed"
	semanticModulePresentFileWriteCaseName = "semantic module upsert with present file"
	semanticModulePresentFileReadCaseName  = "semantic module with present file is contained"
	semanticModuleImportWriteCaseName      = "canonical import module after absent-file semantic module"
	semanticModuleImportReadCaseName       = "canonical import module stays uid-null"
)

// Fixture values. Each case owns its repo id, so one case's repo retract can
// never delete another case's nodes, and its own module name, so each read
// sees only its own case.
const (
	semanticModuleLanguage = "typescript"

	semanticModuleAbsentRepoID   = "repo:backend-conformance:semantic-module-absent"
	semanticModuleAbsentUID      = "module:backend-conformance:semantic-absent"
	semanticModuleAbsentName     = "BackendConformanceAbsentFileModule"
	semanticModuleAbsentFilePath = "backend-conformance/semantic-module/absent.ts"

	semanticModulePresentRepoID   = "repo:backend-conformance:semantic-module-present"
	semanticModulePresentUID      = "module:backend-conformance:semantic-present"
	semanticModulePresentName     = "BackendConformancePresentFileModule"
	semanticModulePresentFilePath = "backend-conformance/semantic-module/present.ts"

	semanticModuleImportRepoID   = "repo:backend-conformance:semantic-module-import"
	semanticModuleImportUID      = "module:backend-conformance:semantic-import"
	semanticModuleImportName     = "BackendConformanceImportedModule"
	semanticModuleImportFilePath = "backend-conformance/semantic-module/import-absent.ts"
)

// SemanticEntityWriterFactory builds the semantic-entity writer a backend runs,
// over the executor the case builder records through.
type SemanticEntityWriterFactory func(sourcecypher.Executor) *sourcecypher.SemanticEntityWriter

// SemanticEntityWriterFor returns the writer construction the reducer wires for
// backend (go/cmd/reducer/neo4j_wiring.go semanticEntityWriterForGraphBackend,
// without the per-label batch caps, which only split batches and cannot change
// a one-row statement). A cmd/reducer test pins the two to identical
// statements.
func SemanticEntityWriterFor(backend BackendID) (SemanticEntityWriterFactory, error) {
	switch backend {
	case BackendNornicDB:
		return func(executor sourcecypher.Executor) *sourcecypher.SemanticEntityWriter {
			return sourcecypher.NewSemanticEntityWriterWithCanonicalNodeRows(executor, 0).WithLabelScopedRetract()
		}, nil
	case BackendNeo4j:
		return func(executor sourcecypher.Executor) *sourcecypher.SemanticEntityWriter {
			return sourcecypher.NewSemanticEntityWriter(executor, 0)
		}, nil
	default:
		return nil, fmt.Errorf("no semantic entity writer wiring for backend %q", backend)
	}
}

// WriteCorpusFor returns DefaultWriteCorpus plus the write cases whose
// statements depend on the backend dialect: the semantic Module cases, built
// from the production writer backend runs. The live test runs this corpus, so
// every read in DefaultReadCorpus has its seed on both backends.
func WriteCorpusFor(backend BackendID) ([]WriteCase, error) {
	factory, err := SemanticEntityWriterFor(backend)
	if err != nil {
		return nil, err
	}
	moduleCases, err := SemanticModuleWriteCases(factory)
	if err != nil {
		return nil, err
	}
	return append(DefaultWriteCorpus(), moduleCases...), nil
}

// SemanticModuleWriteCases builds the semantic Module write cases from the
// statements newWriter's writer emits. Order matters: the File seed precedes
// the present-file write, and the canonical import upsert follows the
// absent-file semantic write it is meant to collide with.
func SemanticModuleWriteCases(newWriter SemanticEntityWriterFactory) ([]WriteCase, error) {
	absent, err := semanticModuleStatements(newWriter, semanticModuleAbsentRepoID,
		semanticModuleAbsentUID, semanticModuleAbsentName, semanticModuleAbsentFilePath)
	if err != nil {
		return nil, err
	}
	present, err := semanticModuleStatements(newWriter, semanticModulePresentRepoID,
		semanticModulePresentUID, semanticModulePresentName, semanticModulePresentFilePath)
	if err != nil {
		return nil, err
	}
	importAbsent, err := semanticModuleStatements(newWriter, semanticModuleImportRepoID,
		semanticModuleImportUID, semanticModuleImportName, semanticModuleImportFilePath)
	if err != nil {
		return nil, err
	}
	canonicalImport := sourcecypher.SanitizeStatements(sourcecypher.BuildBatchedStatements(
		sourcecypher.CanonicalNodeModuleUpsertCypher,
		[]map[string]any{{"name": semanticModuleImportName, "language": semanticModuleLanguage}},
		sourcecypher.DefaultBatchSize,
	))

	const visibility = "the semantic retract and upsert commit together, as the reducer groups them"
	return []WriteCase{
		{
			Name:                  semanticModuleAbsentFileWriteCaseName,
			Capability:            CapabilityCanonicalWrites,
			RequireAtomicGroup:    true,
			TransactionVisibility: visibility,
			Statements:            append(absent, importAbsent...),
		},
		{
			Name:       semanticModuleFileSeedCaseName,
			Capability: CapabilityCanonicalWrites,
			Statements: []sourcecypher.Statement{{
				Operation: sourcecypher.OperationCanonicalUpsert,
				Cypher: `MERGE (f:File {path: $file_path})
SET f.repo_id = $repo_id,
    f.language = $language,
    f.lang = $language`,
				Parameters: map[string]any{
					"file_path": semanticModulePresentFilePath,
					"repo_id":   semanticModulePresentRepoID,
					"language":  semanticModuleLanguage,
				},
			}},
		},
		{
			Name:                  semanticModulePresentFileWriteCaseName,
			Capability:            CapabilityCanonicalWrites,
			RequireAtomicGroup:    true,
			TransactionVisibility: visibility,
			Statements:            present,
		},
		{
			// A separate transaction after the semantic write, as a later
			// canonical projection of a repository importing that module is.
			Name:       semanticModuleImportWriteCaseName,
			Capability: CapabilityCanonicalWrites,
			Statements: canonicalImport,
		},
	}, nil
}

// semanticModuleReadCases returns the exact-row reads for the semantic Module
// write cases. Each read has distinct Cypher so the default fake answers each
// one with its own expected rows.
func semanticModuleReadCases() []ReadCase {
	return []ReadCase{
		{
			Name:       semanticModuleAbsentFileReadCaseName,
			Capability: CapabilityCanonicalWrites,
			Cypher: `MATCH (m:Module {name: $module_name})
RETURN m.uid AS uid, m.lang AS lang, m.evidence_source AS evidence_source`,
			Parameters: map[string]any{"module_name": semanticModuleAbsentName},
			WantRows:   []map[string]any{},
		},
		{
			Name:       semanticModulePresentFileReadCaseName,
			Capability: CapabilityCanonicalWrites,
			Cypher: `MATCH (f:File {path: $file_path})-[:CONTAINS]->(m:Module {name: $module_name})
RETURN f.path AS file_path, m.uid AS uid, m.lang AS lang, m.evidence_source AS evidence_source`,
			Parameters: map[string]any{
				"file_path":   semanticModulePresentFilePath,
				"module_name": semanticModulePresentName,
			},
			WantRows: []map[string]any{{
				"file_path":       semanticModulePresentFilePath,
				"uid":             semanticModulePresentUID,
				"lang":            semanticModuleLanguage,
				"evidence_source": "parser/semantic-entities",
			}},
		},
		{
			Name:       semanticModuleImportReadCaseName,
			Capability: CapabilityCanonicalWrites,
			Cypher: `MATCH (m:Module {name: $module_name, lang: $module_lang})
RETURN m.uid AS uid, m.evidence_source AS evidence_source`,
			Parameters: map[string]any{
				"module_name": semanticModuleImportName,
				"module_lang": semanticModuleLanguage,
			},
			WantRows: []map[string]any{{
				"uid":             nil,
				"evidence_source": "projector/canonical",
			}},
		},
	}
}

// semanticModuleStatements runs one Module row through the writer newWriter
// builds and returns the statements it dispatched, stripped of the `_eshu_*`
// diagnostic keys exactly as every production executor strips them before the
// driver sees them.
func semanticModuleStatements(
	newWriter SemanticEntityWriterFactory,
	repoID, uid, name, filePath string,
) ([]sourcecypher.Statement, error) {
	recorder := &statementRecorder{}
	writer := newWriter(recorder)
	if writer == nil {
		return nil, fmt.Errorf("semantic entity writer factory returned nil")
	}
	_, err := writer.WriteSemanticEntities(context.Background(), semantic.EntityWrite{
		RepoIDs: []string{repoID},
		Rows: []semantic.EntityRow{{
			RepoID:       repoID,
			EntityID:     uid,
			EntityType:   "Module",
			EntityName:   name,
			FilePath:     filePath,
			RelativePath: filePath,
			Language:     semanticModuleLanguage,
			StartLine:    1,
			EndLine:      3,
			Metadata:     map[string]any{"module_kind": "namespace"},
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("build semantic module statements for %q: %w", name, err)
	}
	if len(recorder.statements) == 0 {
		return nil, fmt.Errorf("semantic entity writer emitted no statements for %q", name)
	}
	return sourcecypher.SanitizeStatements(recorder.statements), nil
}

// statementRecorder captures the statements a writer dispatches without
// running them. It is deliberately Execute-only: the writer then takes its
// per-statement path, which emits the same statements in the same order as
// the grouped path (grouping only splits the list by row count), and the
// recorder needs no ExecuteProbe to satisfy the repo's group-executor guard.
type statementRecorder struct {
	statements []sourcecypher.Statement
}

// Execute records one statement.
func (r *statementRecorder) Execute(_ context.Context, stmt sourcecypher.Statement) error {
	r.statements = append(r.statements, stmt)
	return nil
}
