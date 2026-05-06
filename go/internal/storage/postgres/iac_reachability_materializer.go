package postgres

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/iacreachability"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const listActiveIaCContentFilesSQL = `
SELECT repo_id, relative_path, content
FROM content_files
WHERE iac_relevant IS TRUE
   OR lower(relative_path) LIKE '%.tf'
   OR lower(relative_path) LIKE '%.hcl'
   OR lower(relative_path) LIKE '%.yaml'
   OR lower(relative_path) LIKE '%.yml'
   OR lower(relative_path) LIKE '%jenkinsfile%'
ORDER BY repo_id, relative_path
`

// MaterializeIaCReachability writes corpus-wide IaC usage rows for the active
// repository generations after source-local content projection has drained.
func (s IngestionStore) MaterializeIaCReachability(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) error {
	if s.db == nil {
		return fmt.Errorf("ingestion store db is required")
	}

	start := time.Now()
	if tracer != nil {
		var span trace.Span
		ctx, span = tracer.Start(ctx, telemetry.SpanIaCReachabilityMaterialization)
		defer span.End()
	}

	activeGenerations, err := loadActiveRepositoryGenerations(ctx, s.db)
	if err != nil {
		return fmt.Errorf("load active repository generations for IaC reachability: %w", err)
	}
	filesByRepo, err := loadActiveIaCContentFiles(ctx, s.db, activeGenerations)
	if err != nil {
		return fmt.Errorf("load active IaC content files: %w", err)
	}

	analyzedRows := iacreachability.Analyze(filesByRepo, iacreachability.Options{IncludeAmbiguous: true})
	materializedRows := iacReachabilityRowsForActiveGenerations(analyzedRows, activeGenerations, s.now())
	if err := NewIaCReachabilityStore(s.db).Upsert(ctx, materializedRows); err != nil {
		return err
	}

	duration := time.Since(start).Seconds()
	if instruments != nil {
		instruments.IaCReachabilityMaterializationDuration.Record(ctx, duration)
		recordIaCReachabilityRows(ctx, instruments, materializedRows)
	}
	s.logIaCReachabilityMaterialized(ctx, duration, len(filesByRepo), len(materializedRows))
	return nil
}

func loadActiveIaCContentFiles(
	ctx context.Context,
	queryer Queryer,
	activeGenerations map[string]repositoryGenerationIdentity,
) (map[string][]iacreachability.File, error) {
	if queryer == nil || len(activeGenerations) == 0 {
		return nil, nil
	}

	rows, err := queryer.QueryContext(ctx, listActiveIaCContentFilesSQL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	filesByRepo := make(map[string][]iacreachability.File)
	for rows.Next() {
		var repoID, relativePath, content string
		if err := rows.Scan(&repoID, &relativePath, &content); err != nil {
			return nil, err
		}
		if _, ok := activeGenerations[repoID]; !ok {
			continue
		}
		if !iacreachability.RelevantFile(relativePath) {
			continue
		}
		filesByRepo[repoID] = append(filesByRepo[repoID], iacreachability.File{
			RepoID:       repoID,
			RelativePath: relativePath,
			Content:      content,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return filesByRepo, nil
}

func iacReachabilityRowsForActiveGenerations(
	rows []iacreachability.Row,
	activeGenerations map[string]repositoryGenerationIdentity,
	now time.Time,
) []IaCReachabilityRow {
	result := make([]IaCReachabilityRow, 0, len(rows))
	for _, row := range rows {
		identity, ok := activeGenerations[row.RepoID]
		if !ok {
			continue
		}
		result = append(result, IaCReachabilityRow{
			ScopeID:      identity.ScopeID,
			GenerationID: identity.GenerationID,
			RepoID:       row.RepoID,
			Family:       row.Family,
			ArtifactPath: row.ArtifactPath,
			ArtifactName: row.ArtifactName,
			Reachability: IaCReachability(row.Reachability),
			Finding:      IaCFinding(row.Finding),
			Confidence:   row.Confidence,
			Evidence:     append([]string(nil), row.Evidence...),
			Limitations:  append([]string(nil), row.Limitations...),
			ObservedAt:   now,
			UpdatedAt:    now,
		})
	}
	return result
}

func recordIaCReachabilityRows(
	ctx context.Context,
	instruments *telemetry.Instruments,
	rows []IaCReachabilityRow,
) {
	counts := map[IaCReachability]int64{}
	for _, row := range rows {
		counts[row.Reachability]++
	}
	for reachability, count := range counts {
		instruments.IaCReachabilityRows.Add(ctx, count, metric.WithAttributes(
			telemetry.AttrOutcome(string(reachability)),
		))
	}
}

func (s IngestionStore) logIaCReachabilityMaterialized(
	ctx context.Context,
	duration float64,
	repositoryCount int,
	rowCount int,
) {
	attrs := []any{
		slog.Int("repository_count", repositoryCount),
		slog.Int("row_count", rowCount),
		slog.Float64("duration_seconds", duration),
		telemetry.PhaseAttr(telemetry.PhaseProjection),
	}
	if s.Logger != nil {
		s.Logger.InfoContext(ctx, "iac reachability materialized", attrs...)
		return
	}
	log.Printf(
		"iac_reachability_materialized repository_count=%d row_count=%d duration_s=%.2f",
		repositoryCount,
		rowCount,
		duration,
	)
}
