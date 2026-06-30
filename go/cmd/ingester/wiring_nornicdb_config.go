// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func capOptionalBatchSize(configured int, limit int) int {
	if configured <= 0 {
		return limit
	}
	if limit <= 0 || configured <= limit {
		return configured
	}
	return limit
}

func orderedEntityBatchLabels(labelBatchSizes map[string]int) []string {
	labels := make([]string, 0, len(labelBatchSizes))
	for label := range labelBatchSizes {
		labels = append(labels, label)
	}
	slices.Sort(labels)
	return labels
}

func defaultNornicDBEntityLabelBatchSizes(entityBatchSize int) map[string]int {
	return map[string]int{
		"Function": capOptionalBatchSize(entityBatchSize, defaultNornicDBFunctionEntityBatchSize),
		// Struct payloads have been slower than the broad entity default, but
		// still materially lighter than Function rows on the self-repo dogfood
		// lane, so they keep a looser cap than Function.
		"Struct": capOptionalBatchSize(entityBatchSize, defaultNornicDBStructEntityBatchSize),
		// Variable rows are numerous but proved faster at the broader 100-row
		// cap once file-scoped batching removed the earlier wide-row hazard.
		"Variable": capOptionalBatchSize(entityBatchSize, defaultNornicDBVariableEntityBatchSize),
		// K8sResource rows need a per-statement row cap because file-scoped
		// inline containment can otherwise put enough same-file resources into
		// one NornicDB statement to exceed the bounded write budget.
		"K8sResource": capOptionalBatchSize(entityBatchSize, defaultNornicDBK8sResourceEntityBatchSize),
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

func nornicDBEntityLabelBatchSizes(getenv func(string) string, entityBatchSize int) (map[string]int, error) {
	labelBatchSizes := defaultNornicDBEntityLabelBatchSizes(entityBatchSize)
	raw := strings.TrimSpace(getenv(nornicDBEntityLabelBatchSizesEnv))
	if raw == "" {
		return labelBatchSizes, nil
	}

	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		label, value, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("parse %s=%q: entries must be Label=size", nornicDBEntityLabelBatchSizesEnv, raw)
		}
		label = strings.TrimSpace(label)
		value = strings.TrimSpace(value)
		if label == "" {
			return nil, fmt.Errorf("parse %s=%q: label must be non-empty", nornicDBEntityLabelBatchSizesEnv, raw)
		}
		batchSize, err := strconv.Atoi(value)
		if err != nil || batchSize <= 0 {
			return nil, fmt.Errorf("parse %s=%q: label %q must have a positive integer size", nornicDBEntityLabelBatchSizesEnv, raw, label)
		}
		labelBatchSizes[label] = capOptionalBatchSize(entityBatchSize, batchSize)
	}
	return labelBatchSizes, nil
}

func nornicDBEntityLabelPhaseGroupStatements(getenv func(string) string, entityPhaseStatements int) (map[string]int, error) {
	labelStatementLimits := defaultNornicDBEntityLabelPhaseGroupStatements(entityPhaseStatements)
	raw := strings.TrimSpace(getenv(nornicDBEntityLabelPhaseGroupStatementsEnv))
	if raw == "" {
		return labelStatementLimits, nil
	}

	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		label, value, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("parse %s=%q: entries must be Label=size", nornicDBEntityLabelPhaseGroupStatementsEnv, raw)
		}
		label = strings.TrimSpace(label)
		value = strings.TrimSpace(value)
		if label == "" {
			return nil, fmt.Errorf("parse %s=%q: label must be non-empty", nornicDBEntityLabelPhaseGroupStatementsEnv, raw)
		}
		statementCount, err := strconv.Atoi(value)
		if err != nil || statementCount <= 0 {
			return nil, fmt.Errorf("parse %s=%q: label %q must have a positive integer size", nornicDBEntityLabelPhaseGroupStatementsEnv, raw, label)
		}
		labelStatementLimits[label] = capOptionalBatchSize(entityPhaseStatements, statementCount)
	}
	return labelStatementLimits, nil
}

func canonicalExecutorForGraphBackend(
	rawExecutor sourcecypher.Executor,
	graphBackend runtimecfg.GraphBackend,
	nornicDBTimeout time.Duration,
	nornicDBGroupedWrites bool,
	nornicDBPhaseGroupStatements int,
	nornicDBFilePhaseStatements int,
	nornicDBEntityPhaseStatements int,
	nornicDBEntityLabelPhaseStatements map[string]int,
	nornicDBEntityPhaseConcurrency int,
	nornicDBRetractBatchSize int,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) sourcecypher.Executor {
	instrumented := &sourcecypher.InstrumentedExecutor{
		Inner: &sourcecypher.RetryingExecutor{
			Inner:       rawExecutor,
			MaxRetries:  3,
			Instruments: instruments,
		},
		Tracer:      tracer,
		Instruments: instruments,
	}
	if graphBackend == runtimecfg.GraphBackendNornicDB {
		bounded := sourcecypher.TimeoutExecutor{
			Inner:       instrumented,
			Timeout:     nornicDBTimeout,
			TimeoutHint: canonicalWriteTimeoutEnv,
		}
		// NOTE (#4027): nornicDBGroupedWrites is intentionally NOT routed to the
		// bare grouped GroupExecutor here. Whole-materialization atomic canonical
		// writes are unsupported on NornicDB — an UNWIND-driven MATCH does not see a
		// node MERGE'd earlier in the same transaction, so the
		// directory-edge/file/entity/containment phases (which MATCH the nodes the
		// node phases MERGE) resolve nothing and the whole tree is silently dropped
		// (observed as 0 File nodes corpus-wide). Both toggle states use the
		// per-dependency-phase executor below so each MATCH sees the prior committed
		// phase; the toggle's intent is logged at the routing decision just below.
		// Whole-materialization atomic remains valid only for a same-transaction
		// read-your-writes backend (Neo4j), via the canonical writer's GroupExecutor.
		if nornicDBGroupedWrites {
			slog.Warn("NornicDB canonical grouped writes requested; committing per dependency phase — whole-materialization atomic is unsupported on NornicDB (#4027)",
				"graph_backend", string(graphBackend),
				"env_var", nornicDBCanonicalGroupedWritesEnv)
		}
		// Wire the raw executor as drainReader so the bounded drain loop can
		// open its own write sessions independent of the TimeoutExecutor.
		// The drain loop bypasses the timeout wrapper intentionally: each
		// bounded iteration is small (retractBatchSize nodes) and completes
		// well within the per-statement budget; we don't want a 2m global
		// timeout to cancel a correctly-progressing multi-iteration drain.
		var dr retractDrainReader
		if rdr, ok := rawExecutor.(retractDrainReader); ok {
			dr = rdr
		}
		return nornicDBPhaseGroupExecutor{
			inner:                    bounded,
			maxStatements:            nornicDBPhaseGroupStatements,
			fileMaxStatements:        nornicDBFilePhaseStatements,
			entityMaxStatements:      nornicDBEntityPhaseStatements,
			entityLabelMaxStatements: nornicDBEntityLabelPhaseStatements,
			entityPhaseConcurrency:   nornicDBEntityPhaseConcurrency,
			drainReader:              dr,
			retractBatchSize:         nornicDBRetractBatchSize,
			instruments:              instruments,
		}
	}
	return instrumented
}
