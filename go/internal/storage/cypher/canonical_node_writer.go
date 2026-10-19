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

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/package/registry"
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
	packageRegistryLocks              *registry.IdentityLocks
	tfStateOwnershipResolver          TerraformStateOwnershipResolver
	tfStateConfigMatchResolver        TerraformStateConfigMatchResolver
	kustomizeOverlayResolver          KustomizeOverlayResolver
	schemaWriteFence                  func(context.Context) error
	// repositoryRetirementBare emits the bare path-conflict retirement
	// shape instead of the attributed one. It MUST be set on every writer
	// targeting NornicDB: the attributed shape's OPTIONAL MATCH + WITH
	// collect composition drops the id guard there and deletes the live
	// node in steady state (#8173). The default (false) keeps the
	// attributed shape, which is Neo4j-only.
	repositoryRetirementBare bool
	// retiredRepositoryObserver runs after a committed path-conflict
	// retirement with the retired Repository ids (#8316). Nil (the
	// default) observes nothing; production wiring injects the
	// deployable_unit_correlation fingerprint invalidation.
	retiredRepositoryObserver RetiredRepositoryObserver
	// searchKey decides whether infra-label writes append the #7245 infra
	// search key clause. NewCanonicalNodeWriter sets search.On; production
	// wiring passes the backend dialect to NewCanonicalNodeWriterWithDialect.
	searchKey search.Dialect
}

type canonicalWritePhase struct {
	name       string
	statements []Statement
}

// NewCanonicalNodeWriter constructs a writer backed by the given Executor.
// batchSize defaults to DefaultBatchSize (500) if <= 0. instruments may be nil.
//
// It emits the default Neo4j-keyed shape (search.On): infra-label writes carry
// the #7245 infra search key clause, so a writer built here can never leave a
// stale key on Neo4j. It is for tests and fixtures; production wiring must use
// NewCanonicalNodeWriterWithDialect with the dialect resolved from the
// configured graph backend (TestProductionWiringUsesDialectConstructors).
func NewCanonicalNodeWriter(executor Executor, batchSize int, instruments *telemetry.Instruments) *CanonicalNodeWriter {
	return newCanonicalNodeWriter(executor, batchSize, instruments, search.On)
}

// NewCanonicalNodeWriterWithDialect constructs a writer whose infra-label
// writes follow searchKey: search.On (Neo4j) appends the infra search key
// clause, search.Off (NornicDB) emits each statement byte for byte as it was
// before the key existed. It panics on an invalid dialect, because a wiring
// that cannot name its backend must fail before it writes.
func NewCanonicalNodeWriterWithDialect(
	executor Executor,
	batchSize int,
	instruments *telemetry.Instruments,
	searchKey search.Dialect,
) *CanonicalNodeWriter {
	search.MustDialect(searchKey, "NewCanonicalNodeWriterWithDialect")
	return newCanonicalNodeWriter(executor, batchSize, instruments, searchKey)
}

func newCanonicalNodeWriter(
	executor Executor,
	batchSize int,
	instruments *telemetry.Instruments,
	searchKey search.Dialect,
) *CanonicalNodeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &CanonicalNodeWriter{
		executor:             executor,
		batchSize:            batchSize,
		instruments:          instruments,
		packageRegistryLocks: registry.NewIdentityLocks(),
		searchKey:            searchKey,
	}
}

// Write executes all canonical writes in strict phase order:
//
//	A: retract stale nodes
//	B: repository_cleanup (path-conflict retirement only; skipped for
//	   first-generation and delta scopes; never deletes the node C re-MERGEs;
//	   a retirement that deleted a node is reported on
//	   eshu_dp_canonical_repository_retirements_total and the
//	   `canonical repository retired` log, #7324)
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
//	M: structural edges (the #5445 EXTENDS_BASE edge set and base_refs node
//	   property, resolved separately with ctx via kustomizeExtendsBaseEdgeStatements
//	   and appended into this phase, since it is the only structural-edge
//	   builder needing a live resolver read rather than pure mat data)
//
// When the executor implements GroupExecutor, all statements are dispatched as
// a single atomic transaction. Otherwise, statements execute sequentially.
//
// Every write-path error is routed through WrapRetryableNeo4jError before it
// returns, so transient NornicDB failures (driver retry-budget exhaustion,
// connectivity loss, and the codes in retryableNeo4jCodes) reach the projector
// queue as failure.RetryableError and requeue with backpressure instead of
// dead-lettering. This mirrors every other canonical graph writer; the
// classification is not loosened here: genuinely terminal errors such as schema
// constraint violations are returned unchanged and stay terminal.
func (w *CanonicalNodeWriter) Write(ctx context.Context, mat canonical.CanonicalMaterialization) error {
	if mat.IsEmpty() {
		return nil
	}

	// The schema fence runs before any statement is built, so a writer the
	// applied graph schema has stopped admitting fails here rather than writing
	// nodes under an identity the current schema no longer uses. See
	// WithSchemaWriteFence.
	if w.schemaWriteFence != nil {
		if err := w.schemaWriteFence(ctx); err != nil {
			return &schemaFenceError{inner: err}
		}
	}

	// Drop rows whose indexed key the backend would reject before any
	// statement is built, so one oversized value skips one node instead of
	// failing the repository's atomic write (#7058).
	mat, oversizedSkipped := w.dropOversizedIndexKeys(ctx, mat)

	mat.TerraformStateResources = w.resolveTerraformStateOwnership(ctx, mat.TerraformStateResources)
	mat.TerraformStateResources = w.resolveTerraformStateConfigMatchAmbiguity(ctx, mat.TerraformStateResources)

	phases := w.buildPhases(mat)
	if kustomizeStmts := w.kustomizeExtendsBaseEdgeStatements(ctx, mat); len(kustomizeStmts) > 0 {
		for i := range phases {
			if phases[i].name == CanonicalPhaseStructuralEdges {
				phases[i].statements = append(phases[i].statements, kustomizeStmts...)
				break
			}
		}
	}
	phases = annotateCanonicalWritePhases(phases)
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
	if oversizedSkipped > 0 {
		writeSpan.SetAttributes(attribute.Int("oversized_index_keys_skipped", oversizedSkipped))
	}
	packageRegistryLock := w.lockPackageRegistryIdentities(mat)
	defer packageRegistryLock.Unlock()
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
		groupCtx, retirement := captureRepositoryRetirement(ctx, mainStatements)
		if err := ge.ExecuteGroup(groupCtx, mainStatements); err != nil {
			writeSpan.RecordError(err)
			writeSpan.SetStatus(codes.Error, err.Error())
			return WrapRetryableNeo4jError(fmt.Errorf("canonical atomic write: %w", err))
		}
		w.reportRepositoryRetirement(ctx, mat, retirement) // committed with the main group, before the edge group can fail
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
			phaseCtx, retirement := captureRepositoryRetirement(phaseCtx, phase.statements)
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
			w.reportRepositoryRetirement(ctx, mat, retirement)
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
		phaseCtx, retirement := captureRepositoryRetirement(phaseCtx, phase.statements)
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
		w.reportRepositoryRetirement(ctx, mat, retirement)
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
	mat canonical.CanonicalMaterialization,
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
	mat canonical.CanonicalMaterialization,
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
	mat canonical.CanonicalMaterialization,
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

func (w *CanonicalNodeWriter) buildPhases(mat canonical.CanonicalMaterialization) []canonicalWritePhase {
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
		{name: registry.PhasePackages, statements: w.buildPackageRegistryPackageStatements(mat)},
		{name: registry.PhaseVersions, statements: w.buildPackageRegistryVersionStatements(mat)},
		{name: registry.PhaseDependencyTargets, statements: w.buildPackageRegistryDependencyPackageStatements(mat)},
		{name: registry.PhaseDependencies, statements: w.buildPackageRegistryDependencyStatements(mat)},
		{name: registry.PhaseArtifacts, statements: w.buildPackageRegistryArtifactStatements(mat)},
		{name: registry.PhaseEvents, statements: w.buildPackageRegistryEventStatements(mat)},
		{name: "modules", statements: w.buildModuleStatements(mat)},
		{name: CanonicalPhaseStructuralEdges, statements: w.buildStructuralEdgeStatements(mat)},
		// Deferred package_registry edge phases. These MUST run after the
		// package_registry node phases (packages, versions, dependency targets,
		// dependencies, artifacts, events) commit, because NornicDB does not
		// make a multi-label node visible to a later same-transaction
		// UNWIND-driven MATCH. In the atomic GroupExecutor path, Write
		// dispatches these as a second ExecuteGroup after the node group
		// commits; in the per-phase paths they run last, after every node phase
		// has committed.
		{name: registry.PhaseVersionEdges, statements: w.buildPackageRegistryVersionEdgeStatements(mat)},
		{name: registry.PhaseDependencyEdges, statements: w.buildPackageRegistryDependencyEdgeStatements(mat)},
		{name: registry.PhaseArtifactEdges, statements: w.buildPackageRegistryArtifactEdgeStatements(mat)},
		{name: registry.PhaseEventEdges, statements: w.buildPackageRegistryEventEdgeStatements(mat)},
	}
}

// isDeferredPackageRegistryEdgePhase reports whether a phase name belongs to
// the deferred package_registry edge phases that must execute in a second
// atomic write group, after the node phases they MATCH have committed. They
// MATCH MULTI-LABEL nodes (Package/PackageVersion/PackageDependency/
// PackageArtifact/RegistryEvent) that NornicDB does not surface to a
// same-transaction UNWIND-driven MATCH. Single-label edge phases
// (directory_edges, the inline File edges) get cross-statement
// read-your-writes within one atomic group and stay in the main group.
func isDeferredPackageRegistryEdgePhase(name string) bool {
	switch name {
	case registry.PhaseVersionEdges,
		registry.PhaseDependencyEdges,
		registry.PhaseArtifactEdges,
		registry.PhaseEventEdges:
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
