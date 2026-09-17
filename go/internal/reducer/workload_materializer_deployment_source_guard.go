// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// GraphExistenceProber is the narrow read-only capability a deployment-source
// guard needs from the graph backend: run one anchored existence query and
// report whether every target it names is present. It is intentionally
// separate from CypherExecutor — widening that hot interface would force
// every write-only fake and adapter to implement a read primitive it has no
// use for (the same split go/cmd/reducer keeps between cypherRunner and
// cypherProber for #5998). A nil prober keeps the legacy unconditional write.
type GraphExistenceProber interface {
	ProbeGraphExists(ctx context.Context, cypher string, params map[string]any) (bool, error)
}

// deploymentSourceTargetMissingError fails a materialization pass whose
// deployment-source targets are absent from the graph. Retryable() keeps the
// intent queued: the deployment Repository node is committed by another
// scope's materialization with no happens-before against this batch, so a
// later pass binds it and the re-run writes the edge. It must never be
// terminalized — an absent target here is a timing state, not a payload
// defect. DeploymentSourcesWritten stays zero on this path: the deferred
// batch counts nothing as written.
type deploymentSourceTargetMissingError struct {
	batchRows      int
	sampleInstance string
	sampleRepoID   string
}

func (e *deploymentSourceTargetMissingError) Error() string {
	return fmt.Sprintf(
		"deployment-source batch of %d row(s) has no graph target (sample instance %q repo %q); deferring the pass until materialization commits the target",
		e.batchRows, e.sampleInstance, e.sampleRepoID,
	)
}

// Retryable opts the miss into bounded queue retries.
func (e *deploymentSourceTargetMissingError) Retryable() bool { return true }

// buildDeploymentSourceTargetProbe returns the existence probe for one
// deployment-source batch: one inline-anchored MATCH per distinct
// WorkloadInstance and deployment Repository, no WITH, no OPTIONAL MATCH,
// no subquery — the same shape discipline as the shared-edge target guard.
// The probe returns a row if and only if every distinct target of the batch
// exists.
func buildDeploymentSourceTargetProbe(rows []DeploymentSourceRow) (string, map[string]any) {
	params := make(map[string]any, 2*len(rows))
	var sb strings.Builder
	clause := 0
	seen := make(map[string]struct{}, 2*len(rows))
	emit := func(key string, match string, kv ...any) {
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		sb.WriteString("MATCH " + match + "\n")
		for i := 0; i < len(kv); i += 2 {
			params[kv[i].(string)] = kv[i+1]
		}
		clause++
	}
	for _, row := range rows {
		if row.InstanceID != "" {
			key := "i\x00" + row.InstanceID
			emit(key, fmt.Sprintf("(i%d:WorkloadInstance {id: $i%d})", clause, clause),
				fmt.Sprintf("i%d", clause), row.InstanceID)
		}
		if row.DeploymentRepoID != "" {
			key := "r\x00" + row.DeploymentRepoID
			emit(key, fmt.Sprintf("(:Repository {id: $r%d})", clause),
				fmt.Sprintf("r%d", clause), row.DeploymentRepoID)
		}
	}
	sb.WriteString("RETURN 1 LIMIT 1")
	return sb.String(), params
}

// checkDeploymentSourceTargets probes one deployment-source batch for
// write-target completeness before its MERGE runs. It returns nil when no
// prober is wired, the probe itself fails (fail open on infrastructure
// faults: a backend fault must not stall the partition on work that may be
// perfectly writable), or every target is present. A detected miss returns a
// retryable error; the caller must run no deployment-source statement for
// the batch afterwards and must count nothing as written.
func checkDeploymentSourceTargets(
	ctx context.Context,
	prober GraphExistenceProber,
	logger *slog.Logger,
	instruments *telemetry.Instruments,
	rows []DeploymentSourceRow,
) error {
	if prober == nil || len(rows) == 0 {
		return nil
	}
	cypher, params := buildDeploymentSourceTargetProbe(rows)
	allPresent, err := prober.ProbeGraphExists(ctx, cypher, params)
	if err != nil {
		if logger != nil {
			logger.Warn(
				"deployment-source target probe failed, writing without verification",
				"batch_rows", len(rows),
				"error", err,
			)
		}
		return nil
	}
	if allPresent {
		return nil
	}
	if instruments != nil && instruments.SharedEdgeTargetMiss != nil {
		instruments.SharedEdgeTargetMiss.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrDomain(string(DomainWorkloadMaterialization)),
		))
	}
	if logger != nil {
		logger.Warn(
			"deployment-source batch target absent, deferring pass",
			"batch_rows", len(rows),
			"sample_instance_id", rows[0].InstanceID,
			"sample_deployment_repo_id", rows[0].DeploymentRepoID,
		)
	}
	return &deploymentSourceTargetMissingError{
		batchRows:      len(rows),
		sampleInstance: rows[0].InstanceID,
		sampleRepoID:   rows[0].DeploymentRepoID,
	}
}
