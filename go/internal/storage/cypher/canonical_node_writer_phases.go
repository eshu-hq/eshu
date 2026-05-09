package cypher

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (w *CanonicalNodeWriter) buildRepositoryStatements(mat projector.CanonicalMaterialization) []Statement {
	if mat.Repository == nil {
		return nil
	}
	r := mat.Repository
	params := map[string]any{
		"repo_id":       r.RepoID,
		"name":          r.Name,
		"path":          r.Path,
		"local_path":    r.LocalPath,
		"remote_url":    r.RemoteURL,
		"repo_slug":     r.RepoSlug,
		"has_remote":    r.HasRemote,
		"scope_id":      mat.ScopeID,
		"generation_id": mat.GenerationID,
	}
	return []Statement{{
		Operation:  OperationCanonicalUpsert,
		Cypher:     canonicalNodeRepositoryUpsertCypher,
		Parameters: params,
	}}
}

func (w *CanonicalNodeWriter) buildRepositoryCleanupStatements(mat projector.CanonicalMaterialization) []Statement {
	if mat.Repository == nil {
		return nil
	}
	r := mat.Repository
	return []Statement{
		{
			Operation: OperationCanonicalRetract,
			Cypher:    canonicalNodeRepositoryIDCleanupCypher,
			Parameters: map[string]any{
				"repo_id": r.RepoID,
			},
		},
		{
			Operation: OperationCanonicalRetract,
			Cypher:    canonicalNodeRepositoryPathCleanupCypher,
			Parameters: map[string]any{
				"repo_id": r.RepoID,
				"path":    r.Path,
			},
		},
	}
}

// --- Phase C: Directories (depth-ordered) ---

func (w *CanonicalNodeWriter) buildDirectoryStatements(mat projector.CanonicalMaterialization) []Statement {
	if len(mat.Directories) == 0 {
		return nil
	}

	// Group by depth, sorted ascending.
	byDepth := map[int][]projector.DirectoryRow{}
	for _, d := range mat.Directories {
		byDepth[d.Depth] = append(byDepth[d.Depth], d)
	}

	depths := make([]int, 0, len(byDepth))
	for d := range byDepth {
		depths = append(depths, d)
	}
	sort.Ints(depths)

	var stmts []Statement
	for _, depth := range depths {
		dirs := byDepth[depth]
		rows := make([]map[string]any, len(dirs))
		for i, d := range dirs {
			rows[i] = map[string]any{
				"path":          d.Path,
				"name":          d.Name,
				"parent_path":   d.ParentPath,
				"repo_id":       d.RepoID,
				"scope_id":      mat.ScopeID,
				"generation_id": mat.GenerationID,
			}
		}

		cypher := canonicalNodeDirectoryDepthNCypher
		if depth == 0 {
			cypher = canonicalNodeDirectoryDepth0Cypher
		}

		stmts = append(stmts, buildBatchedStatements(cypher, rows, w.batchSize)...)
	}
	return stmts
}

// --- Phase D: Files ---

func (w *CanonicalNodeWriter) buildFileStatements(mat projector.CanonicalMaterialization) []Statement {
	if len(mat.Files) == 0 {
		return nil
	}

	nestedRows := make([]map[string]any, 0, len(mat.Files))
	rootRows := make([]map[string]any, 0)
	for _, f := range mat.Files {
		row := map[string]any{
			"uid":           f.RepoID + ":" + f.RelativePath,
			"path":          f.Path,
			"name":          f.Name,
			"relative_path": f.RelativePath,
			"language":      f.Language,
			"repo_id":       f.RepoID,
			"dir_path":      f.DirPath,
			"scope_id":      mat.ScopeID,
			"generation_id": mat.GenerationID,
		}
		if canonicalNodeFileRowIsRepositoryRoot(f) {
			rootRows = append(rootRows, row)
			continue
		}
		nestedRows = append(nestedRows, row)
	}

	var stmts []Statement
	batchSize := w.batchSize
	if w.fileBatchSize > 0 {
		batchSize = w.fileBatchSize
	}
	nestedCyphers := []string{
		canonicalNodeFileUpdateExistingCypher,
		canonicalNodeFileCreateMissingCypher,
	}
	rootCyphers := []string{
		canonicalNodeRootFileUpdateExistingCypher,
		canonicalNodeRootFileCreateMissingCypher,
	}
	if mat.FirstGeneration {
		nestedCyphers = []string{canonicalNodeFileFirstGenerationMergeCypher}
		rootCyphers = []string{canonicalNodeRootFileFirstGenerationMergeCypher}
	}
	stmts = append(stmts, w.buildFileStatementsForRows(nestedRows, batchSize, nestedCyphers)...)
	stmts = append(stmts, w.buildFileStatementsForRows(rootRows, batchSize, rootCyphers)...)
	return stmts
}

func canonicalNodeFileRowIsRepositoryRoot(file projector.FileRow) bool {
	return strings.TrimSpace(file.RelativePath) != "" && !strings.Contains(file.RelativePath, "/")
}

func (w *CanonicalNodeWriter) buildFileStatementsForRows(rows []map[string]any, batchSize int, cyphers []string) []Statement {
	var stmts []Statement
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		batchRows := rows[start:end]
		summary := fmt.Sprintf(
			"phase=files rows=%d first_path=%v last_path=%v",
			len(batchRows),
			batchRows[0]["path"],
			batchRows[len(batchRows)-1]["path"],
		)
		for _, cypher := range cyphers {
			stmts = append(stmts, Statement{
				Operation: OperationCanonicalUpsert,
				Cypher:    cypher,
				Parameters: map[string]any{
					"rows":                      batchRows,
					StatementMetadataPhaseKey:   CanonicalPhaseFiles,
					StatementMetadataSummaryKey: summary,
				},
			})
		}
	}
	return stmts
}

// --- Phase F: Modules ---

func (w *CanonicalNodeWriter) buildModuleStatements(mat projector.CanonicalMaterialization) []Statement {
	if len(mat.Modules) == 0 {
		return nil
	}

	rows := make([]map[string]any, len(mat.Modules))
	for i, m := range mat.Modules {
		rows[i] = map[string]any{
			"name":     m.Name,
			"language": m.Language,
		}
	}

	return buildBatchedStatements(canonicalNodeModuleUpsertCypher, rows, w.batchSize)
}

// --- Phase G: Structural edges ---

func (w *CanonicalNodeWriter) buildStructuralEdgeStatements(mat projector.CanonicalMaterialization) []Statement {
	var stmts []Statement

	// IMPORTS edges
	if len(mat.Imports) > 0 {
		rows := make([]map[string]any, len(mat.Imports))
		for i, imp := range mat.Imports {
			rows[i] = map[string]any{
				"file_path":     imp.FilePath,
				"module_name":   imp.ModuleName,
				"imported_name": imp.ImportedName,
				"alias":         imp.Alias,
				"line_number":   imp.LineNumber,
				"generation_id": mat.GenerationID,
			}
		}
		stmts = append(stmts, buildBatchedStatements(canonicalNodeImportEdgeCypher, rows, w.batchSize)...)
	}

	// HAS_PARAMETER edges
	if len(mat.Parameters) > 0 {
		rows := make([]map[string]any, len(mat.Parameters))
		for i, p := range mat.Parameters {
			rows[i] = map[string]any{
				"func_name":     p.FunctionName,
				"file_path":     p.FilePath,
				"func_line":     p.FunctionLine,
				"param_name":    p.ParamName,
				"generation_id": mat.GenerationID,
			}
		}
		stmts = append(stmts, buildBatchedStatements(canonicalNodeHasParameterEdgeCypher, rows, w.batchSize)...)
	}

	// Class CONTAINS Function edges
	if len(mat.ClassMembers) > 0 {
		rows := make([]map[string]any, len(mat.ClassMembers))
		for i, cm := range mat.ClassMembers {
			rows[i] = map[string]any{
				"class_name":    cm.ClassName,
				"func_name":     cm.FunctionName,
				"file_path":     cm.FilePath,
				"func_line":     cm.FunctionLine,
				"generation_id": mat.GenerationID,
			}
		}
		stmts = append(stmts, buildBatchedStatements(canonicalNodeClassContainsFuncEdgeCypher, rows, w.batchSize)...)
	}

	// Nested Function CONTAINS edges
	if len(mat.NestedFuncs) > 0 {
		rows := make([]map[string]any, len(mat.NestedFuncs))
		for i, nf := range mat.NestedFuncs {
			rows[i] = map[string]any{
				"outer_name":    nf.OuterName,
				"inner_name":    nf.InnerName,
				"file_path":     nf.FilePath,
				"inner_line":    nf.InnerLine,
				"generation_id": mat.GenerationID,
			}
		}
		stmts = append(stmts, buildBatchedStatements(canonicalNodeNestedFuncEdgeCypher, rows, w.batchSize)...)
	}

	return stmts
}

// --- Batch statement building ---

// buildBatchedStatements splits rows into batches and returns one Statement per batch.
func buildBatchedStatements(cypher string, rows []map[string]any, batchSize int) []Statement {
	if len(rows) == 0 {
		return nil
	}
	var stmts []Statement
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		stmts = append(stmts, Statement{
			Operation:  OperationCanonicalUpsert,
			Cypher:     cypher,
			Parameters: map[string]any{"rows": rows[start:end]},
		})
	}
	return stmts
}

func buildBatchedRetractStatements(cypher string, rows []map[string]any, batchSize int) []Statement {
	if len(rows) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = len(rows)
	}
	stmts := make([]Statement, 0, (len(rows)+batchSize-1)/batchSize)
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		stmts = append(stmts, Statement{
			Operation:  OperationCanonicalRetract,
			Cypher:     cypher,
			Parameters: map[string]any{"rows": rows[start:end]},
		})
	}
	return stmts
}

// --- Telemetry helpers ---

func (w *CanonicalNodeWriter) recordAtomicWrite(ctx context.Context, mode string, seconds float64, _ projector.CanonicalMaterialization) {
	if w.instruments == nil {
		return
	}
	attrs := metric.WithAttributes(telemetry.AttrWritePhase(mode))
	if w.instruments.CanonicalAtomicWrites != nil {
		w.instruments.CanonicalAtomicWrites.Add(ctx, 1, attrs)
	}
	if w.instruments.CanonicalWriteDuration != nil {
		w.instruments.CanonicalWriteDuration.Record(ctx, seconds, attrs)
	}
}

func (w *CanonicalNodeWriter) recordAtomicFallback(ctx context.Context) {
	if w.instruments == nil || w.instruments.CanonicalAtomicFallbacks == nil {
		return
	}
	w.instruments.CanonicalAtomicFallbacks.Add(ctx, 1)
}
