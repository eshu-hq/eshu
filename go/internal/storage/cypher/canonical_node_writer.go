// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// CanonicalNodeWriter writes canonical graph nodes from a CanonicalMaterialization
// in strict phase order. Each phase creates nodes that subsequent phases MATCH.
type CanonicalNodeWriter struct {
	executor                          Executor
	batchSize                         int
	fileBatchSize                     int
	entityBatchSize                   int
	entityLabelBatchSizes             map[string]int
	entityContainmentInEntityUpsert   bool
	entityContainmentBatchAcrossFiles bool
	tracer                            trace.Tracer
	instruments                       *telemetry.Instruments
	packageRegistryLocks              *packageRegistryIdentityLocks
	tfStateOwnershipResolver          TerraformStateOwnershipResolver
	tfStateConfigMatchResolver        TerraformStateConfigMatchResolver
}

type canonicalWritePhase struct {
	name       string
	statements []Statement
}

// NewCanonicalNodeWriter constructs a writer backed by the given Executor.
// batchSize defaults to DefaultBatchSize (500) if <= 0. instruments may be nil.
func NewCanonicalNodeWriter(executor Executor, batchSize int, instruments *telemetry.Instruments) *CanonicalNodeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &CanonicalNodeWriter{
		executor:             executor,
		batchSize:            batchSize,
		instruments:          instruments,
		packageRegistryLocks: newPackageRegistryIdentityLocks(),
	}
}

// WithTracer records canonical graph-write spans when tracing is configured.
func (w *CanonicalNodeWriter) WithTracer(tracer trace.Tracer) *CanonicalNodeWriter {
	if w == nil {
		return nil
	}
	w.tracer = tracer
	return w
}

// WithEntityBatchSize overrides the per-statement row batch size used only for
// canonical entity upserts. Other canonical phases keep the writer's default
// batch size.
func (w *CanonicalNodeWriter) WithEntityBatchSize(batchSize int) *CanonicalNodeWriter {
	if w == nil {
		return nil
	}
	if batchSize > 0 {
		w.entityBatchSize = batchSize
	}
	return w
}

// WithTerraformStateOwnershipResolver injects the port used to scope the
// #5443 MATCHES_STATE edge to the config repository that owns a Terraform
// state resource's backend. Optional: a nil resolver (the default) means no
// MATCHES_STATE edges are written and every TerraformStateResource node's
// config_repo_id property stays null, which is a safe, honest "ownership not
// resolved" state rather than a wrong match. See tfstate_state_match_edge.go.
func (w *CanonicalNodeWriter) WithTerraformStateOwnershipResolver(resolver TerraformStateOwnershipResolver) *CanonicalNodeWriter {
	if w == nil {
		return nil
	}
	w.tfStateOwnershipResolver = resolver
	return w
}

// WithTerraformStateConfigMatchResolver injects the port used to detect
// whether a #5443 MATCHES_STATE edge candidate is ambiguous: (repo_id, name)
// carries no uniqueness constraint (tf_resource_unique is (name, path,
// line_number)), so two Terraform roots in one monorepo can both declare the
// same address. Optional: a nil resolver (the default) leaves
// ConfigMatchAmbiguous at its zero value for every row, matching every unit
// test in this package that constructs rows directly without exercising
// resolver wiring. Production wiring (cmd/projector) always wires a real
// resolver, so this default only affects hand-built test fixtures, never the
// production write path. See tfstate_state_match_edge.go.
func (w *CanonicalNodeWriter) WithTerraformStateConfigMatchResolver(resolver TerraformStateConfigMatchResolver) *CanonicalNodeWriter {
	if w == nil {
		return nil
	}
	w.tfStateConfigMatchResolver = resolver
	return w
}

// WithFileBatchSize overrides the per-statement row batch size used only for
// canonical file upserts. Other canonical phases keep the writer's default
// batch size.
func (w *CanonicalNodeWriter) WithFileBatchSize(batchSize int) *CanonicalNodeWriter {
	if w == nil {
		return nil
	}
	if batchSize > 0 {
		w.fileBatchSize = batchSize
	}
	return w
}

// WithEntityLabelBatchSize overrides the per-statement row batch size for one
// canonical entity label while leaving other entity labels on the default
// entity batch size.
func (w *CanonicalNodeWriter) WithEntityLabelBatchSize(label string, batchSize int) *CanonicalNodeWriter {
	if w == nil {
		return nil
	}
	if label == "" || batchSize <= 0 {
		return w
	}
	if w.entityLabelBatchSizes == nil {
		w.entityLabelBatchSizes = make(map[string]int)
	}
	w.entityLabelBatchSizes[label] = batchSize
	return w
}

// WithEntityContainmentInEntityUpsert keeps entity node and file containment
// writes in the same statement. Use only for backends whose batch MERGE support
// requires the file MATCH context to preserve row-bound entity identity.
func (w *CanonicalNodeWriter) WithEntityContainmentInEntityUpsert() *CanonicalNodeWriter {
	if w == nil {
		return nil
	}
	w.entityContainmentInEntityUpsert = true
	w.entityContainmentBatchAcrossFiles = false
	return w
}

// WithBatchedEntityContainmentInEntityUpsert keeps entity node and containment
// writes in one MERGE-first batch whose rows carry file_path. Use only with
// backends that have proven row-safe `SET += row.props` support in the
// generalized UNWIND/MERGE hot path.
func (w *CanonicalNodeWriter) WithBatchedEntityContainmentInEntityUpsert() *CanonicalNodeWriter {
	if w == nil {
		return nil
	}
	w.entityContainmentInEntityUpsert = true
	w.entityContainmentBatchAcrossFiles = true
	return w
}

// Write executes all canonical writes in strict phase order:
//
//	A: retract stale nodes
//	B: repository_cleanup (skipped for first-generation scopes)
//	C: repository
//	D: directory nodes (MERGE by path, no parent MATCH)
//	D2: directory edges (parent CONTAINS, after directory nodes commit)
//	E: files
//	F: entities (per-label)
//	G: entity_retract
//	H: entity_containment
//	I: terraform_state
//	J: oci_registry
//	K: package_registry
//	L: modules
//	M: structural edges
//
// When the executor implements GroupExecutor, all statements are dispatched as
// a single atomic transaction. Otherwise, statements execute sequentially.
//
// Every write-path error is routed through WrapRetryableNeo4jError before it
// returns, so transient NornicDB failures (driver retry-budget exhaustion,
// connectivity loss, and the codes in retryableNeo4jCodes) reach the projector
// queue as projector.RetryableError and requeue with backpressure instead of
// dead-lettering. This mirrors every other canonical graph writer; the
// classification is not loosened here: genuinely terminal errors such as schema
// constraint violations are returned unchanged and stay terminal.
func (w *CanonicalNodeWriter) Write(ctx context.Context, mat projector.CanonicalMaterialization) error {
	if mat.IsEmpty() {
		return nil
	}

	mat.TerraformStateResources = w.resolveTerraformStateOwnership(ctx, mat.TerraformStateResources)
	mat.TerraformStateResources = w.resolveTerraformStateConfigMatchAmbiguity(ctx, mat.TerraformStateResources)

	phases := annotateCanonicalWritePhases(w.buildPhases(mat))
	if mat.ReconciliationProjection {
		phases = annotateReconciliationDriftWritePhases(phases)
	}
	if mat.FirstGeneration {
		slog.Info(
			"canonical retract skipped for first generation",
			"scope_id", mat.ScopeID,
			"repo_id", mat.RepoID,
			"generation_id", mat.GenerationID,
		)
	}
	allStatements := flattenCanonicalWritePhases(phases)
	if len(allStatements) == 0 {
		return nil
	}
	ctx, writeSpan := w.startWriteSpan(ctx, mat, len(allStatements))
	defer writeSpan.End()
	packageRegistryLock := w.lockPackageRegistryIdentities(mat)
	defer packageRegistryLock.unlock()
	recordPackageRegistryIdentityLock(ctx, writeSpan, mat, packageRegistryLock)

	// Atomic path: single transaction for all node phases, then a deferred
	// second transaction for the package_registry edge phases. The deferred
	// edges MATCH multi-label nodes (Package, PackageVersion, PackageDependency)
	// that are MERGE'd in the main group; NornicDB does not make those
	// multi-label nodes visible to a same-transaction UNWIND-driven MATCH, so
	// the edges must run after the node group commits. Single-label edges (e.g.
	// File -> Directory and Directory -> Directory CONTAINS) do NOT need
	// deferral: NornicDB provides cross-statement read-your-writes for
	// single-label nodes within one atomic group (see the RequireAtomicGroup
	// "file entity containment" conformance case), so they stay inline.
	if ge, ok := w.executor.(GroupExecutor); ok {
		mainStatements, edgeStatements := partitionDeferredPackageRegistryEdgePhases(phases)
		start := time.Now()
		if err := ge.ExecuteGroup(ctx, mainStatements); err != nil {
			writeSpan.RecordError(err)
			writeSpan.SetStatus(codes.Error, err.Error())
			return WrapRetryableNeo4jError(fmt.Errorf("canonical atomic write: %w", err))
		}
		if len(edgeStatements) > 0 {
			if err := ge.ExecuteGroup(ctx, edgeStatements); err != nil {
				writeSpan.RecordError(err)
				writeSpan.SetStatus(codes.Error, err.Error())
				return WrapRetryableNeo4jError(fmt.Errorf("canonical atomic edge write: %w", err))
			}
		}
		dur := time.Since(start).Seconds()
		slog.Info("canonical atomic write completed",
			"scope_id", mat.ScopeID,
			"statements", len(mainStatements),
			"edge_statements", len(edgeStatements),
			"duration_s", dur)
		w.recordAtomicWrite(ctx, "atomic_group", dur, mat)
		return nil
	}

	// Phase-group path: preserve phase ordering, but use bounded grouped
	// execution within each phase when the executor provides a narrower
	// non-atomic grouping surface.
	if pge, ok := w.executor.(PhaseGroupExecutor); ok {
		w.recordAtomicFallback(ctx)
		start := time.Now()
		for _, phase := range phases {
			if len(phase.statements) == 0 {
				continue
			}
			phaseStart := time.Now()
			phaseCtx, phaseSpan := w.startPhaseSpan(ctx, phase, mat)
			if err := pge.ExecutePhaseGroup(phaseCtx, phase.statements); err != nil {
				phaseSpan.RecordError(err)
				phaseSpan.SetStatus(codes.Error, err.Error())
				phaseSpan.End()
				writeSpan.RecordError(err)
				writeSpan.SetStatus(codes.Error, err.Error())
				w.logCanonicalPhaseFailure(phaseCtx, mat, phase, time.Since(phaseStart), err, "phase_group")
				return WrapRetryableNeo4jError(fmt.Errorf("canonical phase-group write (%s): %w", phase.name, err))
			}
			phaseSpan.End()
			phaseSeconds := time.Since(phaseStart).Seconds()
			slog.Info(
				"canonical phase group completed",
				"scope_id", mat.ScopeID,
				"phase", phase.name,
				"statements", len(phase.statements),
				"duration_s", phaseSeconds,
			)
			w.recordAtomicWrite(ctx, "phase_group_"+phase.name, phaseSeconds, mat)
		}
		dur := time.Since(start).Seconds()
		slog.Info("canonical phase-group write completed",
			"scope_id", mat.ScopeID, "statements", len(allStatements), "duration_s", dur)
		w.recordAtomicWrite(ctx, "phase_group", dur, mat)
		return nil
	}

	// Fallback: sequential execution (existing behavior).
	w.recordAtomicFallback(ctx)
	start := time.Now()
	for _, phase := range phases {
		if len(phase.statements) == 0 {
			continue
		}
		phaseStart := time.Now()
		phaseCtx, phaseSpan := w.startPhaseSpan(ctx, phase, mat)
		for _, stmt := range phase.statements {
			if err := w.executor.Execute(phaseCtx, stmt); err != nil {
				phaseSpan.RecordError(err)
				phaseSpan.SetStatus(codes.Error, err.Error())
				phaseSpan.End()
				writeSpan.RecordError(err)
				writeSpan.SetStatus(codes.Error, err.Error())
				w.logCanonicalPhaseFailure(phaseCtx, mat, phase, time.Since(phaseStart), err, "sequential")
				return WrapRetryableNeo4jError(fmt.Errorf("canonical sequential write (%s): %w", phase.name, err))
			}
		}
		phaseSpan.End()
		phaseSeconds := time.Since(phaseStart).Seconds()
		slog.Info(
			"canonical phase completed",
			"scope_id", mat.ScopeID,
			"phase", phase.name,
			"statements", len(phase.statements),
			"duration_s", phaseSeconds,
		)
		w.recordAtomicWrite(ctx, "phase_"+phase.name, phaseSeconds, mat)
	}
	dur := time.Since(start).Seconds()
	slog.Info("canonical sequential write completed",
		"scope_id", mat.ScopeID, "statements", len(allStatements), "duration_s", dur)
	w.recordAtomicWrite(ctx, "sequential_group", dur, mat)
	return nil
}

func (w *CanonicalNodeWriter) startWriteSpan(
	ctx context.Context,
	mat projector.CanonicalMaterialization,
	statementCount int,
) (context.Context, trace.Span) {
	if w.tracer == nil {
		return ctx, trace.SpanFromContext(context.Background())
	}
	return w.tracer.Start(ctx, telemetry.SpanCanonicalWrite, trace.WithAttributes(
		telemetry.AttrScopeID(mat.ScopeID),
		telemetry.AttrGenerationID(mat.GenerationID),
		attribute.String("repo_id", mat.RepoID),
		attribute.Int("statement_count", statementCount),
		attribute.Int("file_count", len(mat.Files)),
		attribute.Int("entity_count", len(mat.Entities)),
	))
}

func (w *CanonicalNodeWriter) startPhaseSpan(
	ctx context.Context,
	phase canonicalWritePhase,
	mat projector.CanonicalMaterialization,
) (context.Context, trace.Span) {
	if w.tracer == nil || phase.name != "retract" {
		return ctx, trace.SpanFromContext(context.Background())
	}
	return w.tracer.Start(ctx, telemetry.SpanCanonicalRetract, trace.WithAttributes(
		telemetry.AttrScopeID(mat.ScopeID),
		telemetry.AttrGenerationID(mat.GenerationID),
		attribute.String("repo_id", mat.RepoID),
		attribute.Int("statement_count", len(phase.statements)),
	))
}

func (w *CanonicalNodeWriter) logCanonicalPhaseFailure(
	ctx context.Context,
	mat projector.CanonicalMaterialization,
	phase canonicalWritePhase,
	duration time.Duration,
	err error,
	mode string,
) {
	slog.WarnContext(
		ctx, "canonical phase failed",
		"scope_id", mat.ScopeID,
		"repo_id", mat.RepoID,
		"generation_id", mat.GenerationID,
		"phase", phase.name,
		"mode", mode,
		"statements", len(phase.statements),
		"duration_s", duration.Seconds(),
		"error", err.Error(),
	)
}

func (w *CanonicalNodeWriter) buildPhases(mat projector.CanonicalMaterialization) []canonicalWritePhase {
	return []canonicalWritePhase{
		{name: "retract", statements: w.buildRetractStatements(mat)},
		{name: "repository_cleanup", statements: w.buildRepositoryCleanupStatements(mat)},
		{name: "repository", statements: w.buildRepositoryStatements(mat)},
		{name: CanonicalPhaseDirectories, statements: w.buildDirectoryNodeStatements(mat)},
		{name: CanonicalPhaseDirectoryEdges, statements: w.buildDirectoryEdgeStatements(mat)},
		{name: CanonicalPhaseFiles, statements: w.buildFileStatements(mat)},
		{name: "entities", statements: w.buildEntityStatements(mat)},
		{name: "entity_retract", statements: w.buildEntityRetractStatements(mat)},
		{name: "entity_containment", statements: w.buildEntityContainmentStatements(mat)},
		{name: canonicalPhaseTerraformState, statements: w.buildTerraformStateStatements(mat)},
		{name: canonicalPhaseOCIRegistry, statements: w.buildOCIRegistryStatements(mat)},
		{name: canonicalPhasePackageRegistryPackages, statements: w.buildPackageRegistryPackageStatements(mat)},
		{name: canonicalPhasePackageRegistryVersions, statements: w.buildPackageRegistryVersionStatements(mat)},
		{name: canonicalPhasePackageRegistryDependencyTargets, statements: w.buildPackageRegistryDependencyPackageStatements(mat)},
		{name: canonicalPhasePackageRegistryDependencies, statements: w.buildPackageRegistryDependencyStatements(mat)},
		{name: "modules", statements: w.buildModuleStatements(mat)},
		{name: "structural_edges", statements: w.buildStructuralEdgeStatements(mat)},
		// Deferred package_registry edge phases. These MUST run after the
		// package_registry node phases (packages, versions, dependency targets,
		// dependencies) commit, because NornicDB does not make a multi-label
		// node visible to a later same-transaction UNWIND-driven MATCH. In the
		// atomic GroupExecutor path, Write dispatches these as a second
		// ExecuteGroup after the node group commits; in the per-phase paths they
		// run last, after every node phase has committed.
		{name: canonicalPhasePackageRegistryVersionEdges, statements: w.buildPackageRegistryVersionEdgeStatements(mat)},
		{name: canonicalPhasePackageRegistryDependencyEdges, statements: w.buildPackageRegistryDependencyEdgeStatements(mat)},
	}
}

// isDeferredPackageRegistryEdgePhase reports whether a phase name belongs to the
// deferred package_registry edge phases that must execute in a second atomic
// write group, after the node phases they MATCH have committed. Only the
// package_registry edges need this: they MATCH MULTI-LABEL nodes
// (Package/PackageVersion/PackageDependency) that NornicDB does not surface to a
// same-transaction UNWIND-driven MATCH. Single-label edge phases (directory_edges,
// the inline File edges) get cross-statement read-your-writes within one atomic
// group and stay in the main group.
func isDeferredPackageRegistryEdgePhase(name string) bool {
	switch name {
	case canonicalPhasePackageRegistryVersionEdges, canonicalPhasePackageRegistryDependencyEdges:
		return true
	default:
		return false
	}
}

// partitionDeferredPackageRegistryEdgePhases splits flattened phase statements
// into the main atomic group and the deferred package_registry edge group,
// preserving order within each group.
func partitionDeferredPackageRegistryEdgePhases(
	phases []canonicalWritePhase,
) (mainStatements []Statement, edgeStatements []Statement) {
	for _, phase := range phases {
		if isDeferredPackageRegistryEdgePhase(phase.name) {
			edgeStatements = append(edgeStatements, phase.statements...)
			continue
		}
		mainStatements = append(mainStatements, phase.statements...)
	}
	return mainStatements, edgeStatements
}

func flattenCanonicalWritePhases(phases []canonicalWritePhase) []Statement {
	var allStatements []Statement
	for _, phase := range phases {
		allStatements = append(allStatements, phase.statements...)
	}
	return allStatements
}

// annotateCanonicalWritePhases tags statements with their owning phase before
// execution so grouped backends can report phase-level diagnostics without
// parsing Cypher text or changing transaction shape.
func annotateCanonicalWritePhases(phases []canonicalWritePhase) []canonicalWritePhase {
	for phaseIndex := range phases {
		phase := &phases[phaseIndex]
		for statementIndex := range phase.statements {
			params := phase.statements[statementIndex].Parameters
			if params == nil {
				params = make(map[string]any)
				phase.statements[statementIndex].Parameters = params
			}
			if _, ok := params[StatementMetadataPhaseKey]; !ok {
				params[StatementMetadataPhaseKey] = phase.name
			}
		}
	}
	return phases
}
