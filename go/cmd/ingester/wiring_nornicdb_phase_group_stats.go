package main

import (
	"log/slog"
	"sort"
	"strings"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

type entityPhaseLabelStats struct {
	phase               string
	label               string
	scopeID             string
	generationID        string
	rows                int
	statements          int
	executions          int
	groupedChunks       int
	singletonStatements int
	totalDuration       time.Duration
	maxDuration         time.Duration
	maxStatementRows    int
	maxExecutionRows    int
	loggedExecutions    int
	completeLogged      bool
}

func ensureEntityPhaseLabelStats(
	stats map[string]*entityPhaseLabelStats,
	phase string,
	label string,
	stmt sourcecypher.Statement,
) *entityPhaseLabelStats {
	phase = strings.TrimSpace(phase)
	if phase == "" {
		phase = "unknown"
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = "unknown"
	}
	if existing := stats[label]; existing != nil {
		existing.captureStatementContext(stmt)
		return existing
	}
	created := &entityPhaseLabelStats{phase: phase, label: label}
	created.captureStatementContext(stmt)
	stats[label] = created
	return created
}

func (s *entityPhaseLabelStats) captureStatementContext(stmt sourcecypher.Statement) {
	if s == nil || len(stmt.Parameters) == 0 {
		return
	}
	if s.scopeID == "" {
		s.scopeID, _ = stmt.Parameters[sourcecypher.StatementMetadataScopeIDKey].(string)
	}
	if s.generationID == "" {
		s.generationID, _ = stmt.Parameters[sourcecypher.StatementMetadataGenerationIDKey].(string)
	}
}

func (s *entityPhaseLabelStats) recordChunk(chunk []sourcecypher.Statement, duration time.Duration) {
	if s == nil || len(chunk) == 0 {
		return
	}
	s.executions++
	s.groupedChunks++
	s.totalDuration += duration
	if duration > s.maxDuration {
		s.maxDuration = duration
	}
	executionRows := 0
	for _, stmt := range chunk {
		s.captureStatementContext(stmt)
		rows := entityStatementRowCount(stmt)
		s.rows += rows
		s.statements++
		executionRows += rows
		if rows > s.maxStatementRows {
			s.maxStatementRows = rows
		}
	}
	if executionRows > s.maxExecutionRows {
		s.maxExecutionRows = executionRows
	}
}

func (s *entityPhaseLabelStats) recordSingleton(stmt sourcecypher.Statement, duration time.Duration) {
	if s == nil {
		return
	}
	s.captureStatementContext(stmt)
	rows := entityStatementRowCount(stmt)
	s.rows += rows
	s.statements++
	s.executions++
	s.singletonStatements++
	s.totalDuration += duration
	if duration > s.maxDuration {
		s.maxDuration = duration
	}
	if rows > s.maxStatementRows {
		s.maxStatementRows = rows
	}
	if rows > s.maxExecutionRows {
		s.maxExecutionRows = rows
	}
}

func logEntityPhaseLabelSummaryIfDue(summary *entityPhaseLabelStats, complete bool) {
	if summary == nil {
		return
	}
	if !complete && summary.executions-summary.loggedExecutions < defaultNornicDBEntityLabelSummaryExecutions {
		return
	}
	logEntityPhaseLabelSummary(summary, complete)
}

func logEntityPhaseLabelSummaries(stats map[string]*entityPhaseLabelStats, complete bool) {
	if len(stats) == 0 {
		return
	}
	labels := make([]string, 0, len(stats))
	for label := range stats {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		summary := stats[label]
		if summary == nil {
			continue
		}
		logEntityPhaseLabelSummary(summary, complete)
	}
}

func logEntityPhaseLabelSummary(summary *entityPhaseLabelStats, complete bool) {
	if summary == nil {
		return
	}
	if complete && summary.completeLogged {
		return
	}
	avgRowsPerStatement := 0.0
	if summary.statements > 0 {
		avgRowsPerStatement = float64(summary.rows) / float64(summary.statements)
	}
	avgExecutionDuration := 0.0
	if summary.executions > 0 {
		avgExecutionDuration = summary.totalDuration.Seconds() / float64(summary.executions)
	}
	attrs := []any{
		"phase", summary.phase,
		"label", summary.label,
		"complete", complete,
		"rows", summary.rows,
		"statements", summary.statements,
		"executions", summary.executions,
		"grouped_chunks", summary.groupedChunks,
		"singleton_statements", summary.singletonStatements,
		"total_duration_s", summary.totalDuration.Seconds(),
		"avg_execution_duration_s", avgExecutionDuration,
		"max_execution_duration_s", summary.maxDuration.Seconds(),
		"avg_rows_per_statement", avgRowsPerStatement,
		"max_statement_rows", summary.maxStatementRows,
		"max_execution_rows", summary.maxExecutionRows,
	}
	if summary.scopeID != "" {
		attrs = append(attrs, "scope_id", summary.scopeID)
	}
	if summary.generationID != "" {
		attrs = append(attrs, "generation_id", summary.generationID)
	}
	slog.Info("nornicdb entity label summary", attrs...)
	summary.loggedExecutions = summary.executions
	if complete {
		summary.completeLogged = true
	}
}

func entityStatementRowCount(stmt sourcecypher.Statement) int {
	if rows, ok := stmt.Parameters["rows"].([]map[string]any); ok {
		return len(rows)
	}
	if rows, ok := stmt.Parameters["rows"].([]any); ok {
		return len(rows)
	}
	if _, ok := stmt.Parameters["entity_id"]; ok {
		return 1
	}
	return 0
}

func (e nornicDBPhaseGroupExecutor) phaseGroupStatementLimit(stmts []sourcecypher.Statement) int {
	phase := statementPhase(stmts)
	// Phase-specific limits are intentionally evidence-driven. Keep the broad
	// phase-group default until a measured repo-scale hotspot proves a narrower
	// phase budget is safer for NornicDB without penalizing unrelated phases.
	if phase == sourcecypher.CanonicalPhaseFiles {
		if e.fileMaxStatements > 0 {
			return e.fileMaxStatements
		}
		return defaultNornicDBFilePhaseStatements
	}
	if phase == sourcecypher.CanonicalPhaseDirectories {
		if e.directoryMaxStatements > 0 {
			return e.directoryMaxStatements
		}
		return defaultNornicDBDirectoryPhaseStatements
	}
	if statementPhaseUsesEntityLabelStats(phase) {
		if label := entityStatementLabel(stmts[0]); label != "" && e.entityLabelMaxStatements != nil {
			if limit := e.entityLabelMaxStatements[label]; limit > 0 {
				return limit
			}
		}
		if e.entityMaxStatements > 0 {
			return e.entityMaxStatements
		}
		return defaultNornicDBEntityPhaseStatements
	}
	if e.maxStatements > 0 {
		return e.maxStatements
	}
	return defaultNornicDBPhaseGroupStatements
}

func statementPhaseUsesEntityLabelStats(phase string) bool {
	switch strings.TrimSpace(phase) {
	case sourcecypher.CanonicalPhaseEntities, sourcecypher.CanonicalPhaseEntityContainment:
		return true
	default:
		return false
	}
}

func statementPhase(stmts []sourcecypher.Statement) string {
	if len(stmts) == 0 {
		return ""
	}
	phase, _ := stmts[0].Parameters[sourcecypher.StatementMetadataPhaseKey].(string)
	return strings.TrimSpace(phase)
}

func statementPhaseGroupMode(stmt sourcecypher.Statement) string {
	mode, _ := stmt.Parameters[sourcecypher.StatementMetadataPhaseGroupModeKey].(string)
	return strings.TrimSpace(mode)
}

func entityStatementLabel(stmt sourcecypher.Statement) string {
	label, _ := stmt.Parameters[sourcecypher.StatementMetadataEntityLabelKey].(string)
	return strings.TrimSpace(label)
}

func allStatementsUseOperation(stmts []sourcecypher.Statement, operation sourcecypher.Operation) bool {
	if len(stmts) == 0 {
		return false
	}
	for _, stmt := range stmts {
		if stmt.Operation != operation {
			return false
		}
	}
	return true
}

func summarizePhaseGroupChunk(stmts []sourcecypher.Statement) string {
	if len(stmts) == 0 {
		return ""
	}
	if summary, ok := stmts[0].Parameters[sourcecypher.StatementMetadataSummaryKey].(string); ok && strings.TrimSpace(summary) != "" {
		return summary
	}
	return summarizePhaseGroupStatement(stmts[0].Cypher)
}

func sanitizedPhaseGroupChunk(stmts []sourcecypher.Statement) []sourcecypher.Statement {
	sanitized := make([]sourcecypher.Statement, len(stmts))
	for i, stmt := range stmts {
		sanitized[i] = sanitizedStatement(stmt)
	}
	return sanitized
}

func sanitizedStatement(stmt sourcecypher.Statement) sourcecypher.Statement {
	stmt.Parameters = sanitizedStatementParameters(stmt.Parameters)
	return stmt
}

func sanitizedStatementParameters(params map[string]any) map[string]any {
	if len(params) == 0 {
		return params
	}

	hasDiagnostics := false
	for key := range params {
		if strings.HasPrefix(key, "_") {
			hasDiagnostics = true
			break
		}
	}
	if !hasDiagnostics {
		return params
	}

	sanitized := make(map[string]any, len(params))
	for key, value := range params {
		if strings.HasPrefix(key, "_") {
			continue
		}
		sanitized[key] = value
	}
	return sanitized
}

func summarizePhaseGroupStatement(cypher string) string {
	trimmed := strings.TrimSpace(cypher)
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) > 2 {
		lines = lines[:2]
	}
	trimmed = strings.Join(lines, " | ")
	if len(trimmed) > 120 {
		return trimmed[:120]
	}
	return trimmed
}
