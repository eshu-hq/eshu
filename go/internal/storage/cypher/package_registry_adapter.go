// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/package/registry"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// This file adapts the package/registry leaf to the CanonicalNodeWriter. The
// leaf owns the Cypher templates, row builders, phase names, and identity
// locks as statement-free parts; the methods here convert each part to
// batched Statements and hold the writer-scoped lock set. Statement assembly
// and phase dispatch stay in this package so the leaf never imports it back.

func (w *CanonicalNodeWriter) packageRegistryStatements(part registry.Part, mat canonical.CanonicalMaterialization) []Statement {
	return packageRegistryBatchedStatements(part.Cypher, part.Rows, w.batchSize, part.Label, part.Phase, mat)
}

func (w *CanonicalNodeWriter) buildPackageRegistryStatements(mat canonical.CanonicalMaterialization) []Statement {
	var statements []Statement
	statements = append(statements, w.buildPackageRegistryPackageStatements(mat)...)
	statements = append(statements, w.buildPackageRegistryVersionStatements(mat)...)
	statements = append(statements, w.buildPackageRegistryDependencyPackageStatements(mat)...)
	statements = append(statements, w.buildPackageRegistryDependencyStatements(mat)...)
	statements = append(statements, w.buildPackageRegistryArtifactStatements(mat)...)
	return statements
}

func (w *CanonicalNodeWriter) buildPackageRegistryPackageStatements(mat canonical.CanonicalMaterialization) []Statement {
	return w.packageRegistryStatements(registry.BuildPackagePart(mat), mat)
}

func (w *CanonicalNodeWriter) buildPackageRegistryVersionStatements(mat canonical.CanonicalMaterialization) []Statement {
	return w.packageRegistryStatements(registry.BuildVersionPart(mat), mat)
}

func (w *CanonicalNodeWriter) buildPackageRegistryDependencyStatements(mat canonical.CanonicalMaterialization) []Statement {
	return w.packageRegistryStatements(registry.BuildDependencyPart(mat), mat)
}

func (w *CanonicalNodeWriter) buildPackageRegistryDependencyPackageStatements(
	mat canonical.CanonicalMaterialization,
) []Statement {
	return w.packageRegistryStatements(registry.BuildDependencyTargetPart(mat), mat)
}

func (w *CanonicalNodeWriter) buildPackageRegistryArtifactStatements(
	mat canonical.CanonicalMaterialization,
) []Statement {
	return w.packageRegistryStatements(registry.BuildArtifactPart(mat), mat)
}

func (w *CanonicalNodeWriter) buildPackageRegistryArtifactEdgeStatements(
	mat canonical.CanonicalMaterialization,
) []Statement {
	return w.packageRegistryStatements(registry.BuildArtifactEdgePart(mat), mat)
}

func (w *CanonicalNodeWriter) buildPackageRegistryVersionEdgeStatements(
	mat canonical.CanonicalMaterialization,
) []Statement {
	return w.packageRegistryStatements(registry.BuildVersionEdgePart(mat), mat)
}

func (w *CanonicalNodeWriter) buildPackageRegistryDependencyEdgeStatements(
	mat canonical.CanonicalMaterialization,
) []Statement {
	return w.packageRegistryStatements(registry.BuildDependencyEdgePart(mat), mat)
}

func (w *CanonicalNodeWriter) buildPackageRegistryEventStatements(
	mat canonical.CanonicalMaterialization,
) []Statement {
	return w.packageRegistryStatements(registry.BuildEventPart(mat), mat)
}

func (w *CanonicalNodeWriter) buildPackageRegistryEventEdgeStatements(
	mat canonical.CanonicalMaterialization,
) []Statement {
	return w.packageRegistryStatements(registry.BuildEventEdgePart(mat), mat)
}

func packageRegistryBatchedStatements(
	cypher string,
	rows []map[string]any,
	batchSize int,
	label string,
	phase string,
	mat canonical.CanonicalMaterialization,
) []Statement {
	statements := BuildBatchedStatements(cypher, rows, batchSize)
	for index := range statements {
		batchRows := statements[index].Parameters["rows"].([]map[string]any)
		statements[index].Parameters[StatementMetadataPhaseKey] = phase
		statements[index].Parameters[StatementMetadataEntityLabelKey] = label
		statements[index].Parameters[StatementMetadataScopeIDKey] = mat.ScopeID
		statements[index].Parameters[StatementMetadataGenerationIDKey] = mat.GenerationID
		statements[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"label=%s rows=%d",
			label,
			len(batchRows),
		)
	}
	return statements
}

func (w *CanonicalNodeWriter) lockPackageRegistryIdentities(
	mat canonical.CanonicalMaterialization,
) registry.Lease {
	if w == nil || w.packageRegistryLocks == nil {
		return registry.Lease{}
	}
	return w.packageRegistryLocks.Lock(registry.LockKeys(mat))
}

func recordPackageRegistryIdentityLock(
	ctx context.Context,
	span trace.Span,
	mat canonical.CanonicalMaterialization,
	lease registry.Lease,
) {
	if lease.KeyCount == 0 {
		return
	}
	span.SetAttributes(
		attribute.Int("package_registry_identity_lock_key_count", lease.KeyCount),
		attribute.Float64("package_registry_identity_lock_wait_seconds", lease.Wait.Seconds()),
	)
	if !lease.SlowLockWait() {
		return
	}
	slog.InfoContext(
		ctx, "canonical package registry identity lock acquired",
		"scope_id", mat.ScopeID,
		"repo_id", mat.RepoID,
		"generation_id", mat.GenerationID,
		"package_uid_min", lease.MinKey,
		"package_uid_count", lease.KeyCount,
		"wait_s", lease.Wait.Seconds(),
	)
}
