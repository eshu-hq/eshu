package main

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	defaultNornicDBCanonicalWriteTimeout            = 30 * time.Second
	defaultNornicDBPhaseGroupStatements             = 500
	defaultNornicDBFilePhaseStatements              = 5
	defaultNornicDBFileBatchSize                    = 100
	defaultNornicDBEntityPhaseStatements            = 25
	defaultNornicDBEntityBatchSize                  = 100
	defaultNornicDBFunctionEntityBatchSize          = 15
	defaultNornicDBStructEntityBatchSize            = 50
	defaultNornicDBVariableEntityBatchSize          = 100
	defaultNornicDBK8sResourceEntityBatchSize       = 1
	defaultNornicDBFunctionEntityPhaseStatements    = 5
	defaultNornicDBStructEntityPhaseStatements      = 15
	defaultNornicDBVariableEntityPhaseStatements    = 5
	defaultNornicDBK8sResourceEntityPhaseStatements = 1
	canonicalWriteTimeoutEnv                        = "ESHU_CANONICAL_WRITE_TIMEOUT"
	nornicDBCanonicalGroupedWritesEnv               = "ESHU_NORNICDB_CANONICAL_GROUPED_WRITES"
	nornicDBPhaseGroupStatementsEnv                 = "ESHU_NORNICDB_PHASE_GROUP_STATEMENTS"
	nornicDBFilePhaseGroupStatementsEnv             = "ESHU_NORNICDB_FILE_PHASE_GROUP_STATEMENTS"
	nornicDBFileBatchSizeEnv                        = "ESHU_NORNICDB_FILE_BATCH_SIZE"
	nornicDBEntityPhaseStatementsEnv                = "ESHU_NORNICDB_ENTITY_PHASE_GROUP_STATEMENTS"
	nornicDBEntityBatchSizeEnv                      = "ESHU_NORNICDB_ENTITY_BATCH_SIZE"
	nornicDBEntityLabelBatchSizesEnv                = "ESHU_NORNICDB_ENTITY_LABEL_BATCH_SIZES"
	nornicDBEntityLabelPhaseGroupStatementsEnv      = "ESHU_NORNICDB_ENTITY_LABEL_PHASE_GROUP_STATEMENTS"
)

// bootstrapCanonicalExecutorForGraphBackend mirrors ingester's NornicDB
// canonical-write safety path so bootstrap-index cannot send a whole
// source-local materialization as one oversized grouped transaction.
func bootstrapCanonicalExecutorForGraphBackend(
	rawExecutor sourcecypher.Executor,
	graphBackend runtimecfg.GraphBackend,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) (sourcecypher.Executor, error) {
	instrumented := &sourcecypher.InstrumentedExecutor{
		Inner: &sourcecypher.RetryingExecutor{
			Inner:       rawExecutor,
			MaxRetries:  3,
			Instruments: instruments,
		},
		Tracer:      tracer,
		Instruments: instruments,
	}
	if graphBackend != runtimecfg.GraphBackendNornicDB {
		return instrumented, nil
	}

	groupedWrites, err := nornicDBCanonicalGroupedWrites(getenv)
	if err != nil {
		return nil, err
	}
	phaseGroupStatements, err := nornicDBPositiveIntEnv(getenv, nornicDBPhaseGroupStatementsEnv, defaultNornicDBPhaseGroupStatements)
	if err != nil {
		return nil, err
	}
	filePhaseStatements, err := nornicDBPositiveIntEnv(getenv, nornicDBFilePhaseGroupStatementsEnv, defaultNornicDBFilePhaseStatements)
	if err != nil {
		return nil, err
	}
	entityPhaseStatements, err := nornicDBPositiveIntEnv(getenv, nornicDBEntityPhaseStatementsEnv, defaultNornicDBEntityPhaseStatements)
	if err != nil {
		return nil, err
	}
	entityLabelPhaseStatements, err := nornicDBEntityLabelPhaseGroupStatements(getenv, entityPhaseStatements)
	if err != nil {
		return nil, err
	}

	bounded := sourcecypher.TimeoutExecutor{
		Inner:       instrumented,
		Timeout:     nornicDBCanonicalWriteTimeout(getenv),
		TimeoutHint: canonicalWriteTimeoutEnv,
	}
	if groupedWrites {
		slog.Warn("NornicDB bootstrap canonical grouped writes enabled for conformance",
			"graph_backend", string(graphBackend),
			"grouped_writes", true,
			"env_var", nornicDBCanonicalGroupedWritesEnv)
		return bounded, nil
	}
	return bootstrapNornicDBPhaseGroupExecutor{
		inner:                    bounded,
		maxStatements:            phaseGroupStatements,
		fileMaxStatements:        filePhaseStatements,
		entityMaxStatements:      entityPhaseStatements,
		entityLabelMaxStatements: entityLabelPhaseStatements,
	}, nil
}

type bootstrapNornicDBPhaseGroupExecutor struct {
	inner                    sourcecypher.Executor
	maxStatements            int
	fileMaxStatements        int
	entityMaxStatements      int
	entityLabelMaxStatements map[string]int
}

func (e bootstrapNornicDBPhaseGroupExecutor) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	if e.inner == nil {
		return nil
	}
	return e.inner.Execute(ctx, bootstrapSanitizedStatement(stmt))
}

func (e bootstrapNornicDBPhaseGroupExecutor) ExecutePhaseGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	if len(stmts) == 0 || e.inner == nil {
		return nil
	}
	if allBootstrapStatementsUseOperation(stmts, sourcecypher.OperationCanonicalRetract) {
		return e.executeSequentialRetractPhase(ctx, stmts)
	}
	ge, ok := e.inner.(sourcecypher.GroupExecutor)
	if !ok {
		for _, stmt := range stmts {
			if err := e.inner.Execute(ctx, bootstrapSanitizedStatement(stmt)); err != nil {
				return err
			}
		}
		return nil
	}
	return e.executeGroupedByLabel(ctx, ge, stmts)
}

func (e bootstrapNornicDBPhaseGroupExecutor) executeSequentialRetractPhase(ctx context.Context, stmts []sourcecypher.Statement) error {
	for i, stmt := range stmts {
		startedAt := time.Now()
		if err := e.inner.Execute(ctx, bootstrapSanitizedStatement(stmt)); err != nil {
			return fmt.Errorf(
				"phase-group retract statement %d/%d (duration=%s, first_statement=%q): %w",
				i+1,
				len(stmts),
				time.Since(startedAt),
				bootstrapStatementSummary(stmt),
				err,
			)
		}
	}
	return nil
}

func (e bootstrapNornicDBPhaseGroupExecutor) executeGroupedByLabel(
	ctx context.Context,
	ge sourcecypher.GroupExecutor,
	stmts []sourcecypher.Statement,
) error {
	grouped := make([]sourcecypher.Statement, 0, len(stmts))
	groupedLabel := ""
	flush := func() error {
		if len(grouped) == 0 {
			return nil
		}
		err := e.executeGroupedChunks(ctx, ge, grouped, e.phaseGroupStatementLimit(grouped))
		grouped = grouped[:0]
		groupedLabel = ""
		return err
	}

	for _, stmt := range stmts {
		if bootstrapStatementPhaseGroupMode(stmt) == sourcecypher.PhaseGroupModeExecuteOnly {
			if err := flush(); err != nil {
				return err
			}
			startedAt := time.Now()
			if err := e.inner.Execute(ctx, bootstrapSanitizedStatement(stmt)); err != nil {
				return fmt.Errorf(
					"phase-group singleton statement (phase=%s, duration=%s, first_statement=%q): %w",
					bootstrapStatementPhase([]sourcecypher.Statement{stmt}),
					time.Since(startedAt),
					bootstrapStatementSummary(stmt),
					err,
				)
			}
			continue
		}
		label := bootstrapEntityStatementLabel(stmt)
		if len(grouped) > 0 && groupedLabel != label {
			if err := flush(); err != nil {
				return err
			}
		}
		grouped = append(grouped, stmt)
		if groupedLabel == "" {
			groupedLabel = label
		}
		if len(grouped) >= e.phaseGroupStatementLimit(grouped) {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

func (e bootstrapNornicDBPhaseGroupExecutor) executeGroupedChunks(
	ctx context.Context,
	ge sourcecypher.GroupExecutor,
	stmts []sourcecypher.Statement,
	maxStatements int,
) error {
	totalChunks := (len(stmts) + maxStatements - 1) / maxStatements
	for start := 0; start < len(stmts); start += maxStatements {
		end := start + maxStatements
		if end > len(stmts) {
			end = len(stmts)
		}
		chunkStartedAt := time.Now()
		chunk := stmts[start:end]
		if err := ge.ExecuteGroup(ctx, bootstrapSanitizedPhaseGroupChunk(chunk)); err != nil {
			return fmt.Errorf(
				"phase-group chunk %d/%d (statements %d-%d of %d, size=%d, duration=%s, first_statement=%q): %w",
				(start/maxStatements)+1,
				totalChunks,
				start+1,
				end,
				len(stmts),
				end-start,
				time.Since(chunkStartedAt),
				bootstrapStatementSummary(chunk[0]),
				err,
			)
		}
	}
	return nil
}

func (e bootstrapNornicDBPhaseGroupExecutor) phaseGroupStatementLimit(stmts []sourcecypher.Statement) int {
	switch bootstrapStatementPhase(stmts) {
	case sourcecypher.CanonicalPhaseFiles:
		return positiveOrDefault(e.fileMaxStatements, defaultNornicDBFilePhaseStatements)
	case sourcecypher.CanonicalPhaseEntities, sourcecypher.CanonicalPhaseEntityContainment:
		if label := bootstrapEntityStatementLabel(stmts[0]); label != "" && e.entityLabelMaxStatements != nil {
			if limit := e.entityLabelMaxStatements[label]; limit > 0 {
				return limit
			}
		}
		return positiveOrDefault(e.entityMaxStatements, defaultNornicDBEntityPhaseStatements)
	default:
		return positiveOrDefault(e.maxStatements, defaultNornicDBPhaseGroupStatements)
	}
}

func bootstrapStatementPhase(stmts []sourcecypher.Statement) string {
	if len(stmts) == 0 {
		return ""
	}
	phase, _ := stmts[0].Parameters[sourcecypher.StatementMetadataPhaseKey].(string)
	return strings.TrimSpace(phase)
}

func bootstrapEntityStatementLabel(stmt sourcecypher.Statement) string {
	label, _ := stmt.Parameters[sourcecypher.StatementMetadataEntityLabelKey].(string)
	return strings.TrimSpace(label)
}

func bootstrapStatementPhaseGroupMode(stmt sourcecypher.Statement) string {
	mode, _ := stmt.Parameters[sourcecypher.StatementMetadataPhaseGroupModeKey].(string)
	return strings.TrimSpace(mode)
}

func allBootstrapStatementsUseOperation(stmts []sourcecypher.Statement, operation sourcecypher.Operation) bool {
	for _, stmt := range stmts {
		if stmt.Operation != operation {
			return false
		}
	}
	return len(stmts) > 0
}

func bootstrapSanitizedPhaseGroupChunk(stmts []sourcecypher.Statement) []sourcecypher.Statement {
	sanitized := make([]sourcecypher.Statement, len(stmts))
	for i, stmt := range stmts {
		sanitized[i] = bootstrapSanitizedStatement(stmt)
	}
	return sanitized
}

func bootstrapSanitizedStatement(stmt sourcecypher.Statement) sourcecypher.Statement {
	stmt.Parameters = bootstrapSanitizedParameters(stmt.Parameters)
	return stmt
}

func bootstrapSanitizedParameters(params map[string]any) map[string]any {
	if len(params) == 0 {
		return params
	}
	hasDiagnosticKeys := false
	for key := range params {
		if strings.HasPrefix(key, "_") {
			hasDiagnosticKeys = true
			break
		}
	}
	if !hasDiagnosticKeys {
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

func bootstrapStatementSummary(stmt sourcecypher.Statement) string {
	if summary, ok := stmt.Parameters[sourcecypher.StatementMetadataSummaryKey].(string); ok && strings.TrimSpace(summary) != "" {
		return summary
	}
	trimmed := strings.TrimSpace(stmt.Cypher)
	lines := strings.Split(trimmed, "\n")
	if len(lines) > 2 {
		lines = lines[:2]
	}
	summary := strings.Join(lines, " | ")
	if len(summary) > 120 {
		return summary[:120]
	}
	return summary
}

func nornicDBCanonicalWriteTimeout(getenv func(string) string) time.Duration {
	raw := strings.TrimSpace(getenv(canonicalWriteTimeoutEnv))
	if raw == "" {
		return defaultNornicDBCanonicalWriteTimeout
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return defaultNornicDBCanonicalWriteTimeout
	}
	return parsed
}

func nornicDBCanonicalGroupedWrites(getenv func(string) string) (bool, error) {
	raw := strings.TrimSpace(getenv(nornicDBCanonicalGroupedWritesEnv))
	if raw == "" {
		return false, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("parse %s=%q: %w", nornicDBCanonicalGroupedWritesEnv, raw, err)
	}
	return enabled, nil
}

func nornicDBPositiveIntEnv(getenv func(string) string, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("parse %s=%q: must be a positive integer", key, raw)
	}
	return n, nil
}

func nornicDBEntityLabelBatchSizes(getenv func(string) string, entityBatchSize int) (map[string]int, error) {
	return nornicDBLabelSizeMap(
		getenv,
		nornicDBEntityLabelBatchSizesEnv,
		defaultNornicDBEntityLabelBatchSizes(entityBatchSize),
		entityBatchSize,
	)
}

func nornicDBEntityLabelPhaseGroupStatements(getenv func(string) string, entityPhaseStatements int) (map[string]int, error) {
	return nornicDBLabelSizeMap(
		getenv,
		nornicDBEntityLabelPhaseGroupStatementsEnv,
		defaultNornicDBEntityLabelPhaseGroupStatements(entityPhaseStatements),
		entityPhaseStatements,
	)
}

func defaultNornicDBEntityLabelBatchSizes(entityBatchSize int) map[string]int {
	return map[string]int{
		"Function":    capOptionalBatchSize(entityBatchSize, defaultNornicDBFunctionEntityBatchSize),
		"K8sResource": capOptionalBatchSize(entityBatchSize, defaultNornicDBK8sResourceEntityBatchSize),
		"Struct":      capOptionalBatchSize(entityBatchSize, defaultNornicDBStructEntityBatchSize),
		"Variable":    capOptionalBatchSize(entityBatchSize, defaultNornicDBVariableEntityBatchSize),
	}
}

func defaultNornicDBEntityLabelPhaseGroupStatements(entityPhaseStatements int) map[string]int {
	return map[string]int{
		"Function":    capOptionalBatchSize(entityPhaseStatements, defaultNornicDBFunctionEntityPhaseStatements),
		"K8sResource": capOptionalBatchSize(entityPhaseStatements, defaultNornicDBK8sResourceEntityPhaseStatements),
		"Struct":      capOptionalBatchSize(entityPhaseStatements, defaultNornicDBStructEntityPhaseStatements),
		"Variable":    capOptionalBatchSize(entityPhaseStatements, defaultNornicDBVariableEntityPhaseStatements),
	}
}

func capOptionalBatchSize(configured int, limit int) int {
	if configured <= 0 || configured > limit {
		return limit
	}
	return configured
}

func nornicDBLabelSizeMap(
	getenv func(string) string,
	key string,
	defaults map[string]int,
	ceiling int,
) (map[string]int, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return defaults, nil
	}
	for _, entry := range strings.Split(raw, ",") {
		parts := strings.SplitN(strings.TrimSpace(entry), "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("parse %s=%q: entries must be Label=size", key, raw)
		}
		label := strings.TrimSpace(parts[0])
		if label == "" {
			return nil, fmt.Errorf("parse %s=%q: label must be non-empty", key, raw)
		}
		size, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || size <= 0 {
			return nil, fmt.Errorf("parse %s=%q: label %q must have a positive integer size", key, raw, label)
		}
		defaults[label] = capOptionalBatchSize(ceiling, size)
	}
	return defaults, nil
}

func positiveOrDefault(value int, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}
