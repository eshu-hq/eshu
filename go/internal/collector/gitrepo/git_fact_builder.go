// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitrepo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gitrepo/gitcodeowners"
	"github.com/eshu-hq/eshu/go/internal/collector/gitrepo/gitdocs"
	"github.com/eshu-hq/eshu/go/internal/collector/gitrepo/gitmodel"
	"github.com/eshu-hq/eshu/go/internal/collector/gitrepo/gitobs"
	"github.com/eshu-hq/eshu/go/internal/collector/gitrepo/gitsubmodule"
	"github.com/eshu-hq/eshu/go/internal/collector/gitrepo/gitsvccatalog"
	"github.com/eshu-hq/eshu/go/internal/collector/gitrepo/gittfstate"
	"github.com/eshu-hq/eshu/go/internal/collector/gitrepo/workflowimage"

	"github.com/eshu-hq/eshu/go/internal/collector"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// factStreamBuffer is the channel buffer size for streaming fact production.
// Matches the Postgres batch INSERT size so the channel drains at the same
// rate the producer fills it.
const factStreamBuffer = 500

// buildStreamingGeneration computes scope/generation metadata from the full
// snapshot (needed for the freshness hint hash), then launches a background
// goroutine that streams facts through a channel. Snapshot entries are niled
// as facts are emitted so file body strings become GC-eligible immediately
// rather than after the entire generation commits.
func buildStreamingGeneration(
	repoPath string,
	repo repositoryidentity.Metadata,
	sourceRunID string,
	observedAt time.Time,
	snapshot RepositorySnapshot,
	isDependency bool,
	ref string,
) collector.CollectedGeneration {
	return buildStreamingGenerationWithContext(context.Background(), repoPath, repo, sourceRunID, observedAt, snapshot, isDependency, ref)
}

func buildStreamingGenerationWithContext(
	ctx context.Context,
	repoPath string,
	repo repositoryidentity.Metadata,
	sourceRunID string,
	observedAt time.Time,
	snapshot RepositorySnapshot,
	isDependency bool,
	ref string,
) collector.CollectedGeneration {
	if ctx == nil {
		ctx = context.Background()
	}
	scopeValue := buildScope(repo, ref)
	// A reconciliation snapshot carries an empty freshness hint so the
	// commit-time skip never elides it: the sweep must re-project the full
	// observation to retract drift even when the content hash is unchanged.
	freshnessHint := snapshotFreshnessHint(snapshot)
	if snapshot.Reconcile {
		freshnessHint = ""
	}
	generation := buildGeneration(
		scopeValue.ScopeID,
		sourceRunID,
		repoPath,
		observedAt,
		freshnessHint,
		snapshot.HeadCommitSHA,
		snapshot.Delta,
	)
	contentFileCount := len(snapshot.ContentFiles)
	if len(snapshot.ContentFileMetas) > 0 {
		contentFileCount = len(snapshot.ContentFileMetas)
	}
	followupFactCount := 13
	if snapshot.Delta {
		// EstimatedFactCount is a conservative metadata-only floor. Preserve
		// its existing delta baseline and account for the newly unconditional
		// rationale marker; the drained atomic remains the exact emitted count.
		followupFactCount = 2
	}
	dataflowScannedFactCount := 0
	if snapshot.DataflowScanned && !snapshot.Delta {
		dataflowScannedFactCount = 1
	}
	// factCount is a cheap pre-computed estimate from metadata counts only.
	// The body-re-reading count passes (serviceCatalogFactCount,
	// gitDocumentationFactCount, workflowImageEvidenceFactCount) have been
	// removed; the exact count is derived from the emitted stream via the
	// atomic counter populated by streamFacts.
	factCount := 1 + len(snapshot.FileData) + contentFileCount +
		len(snapshot.ContentEntities) + len(snapshot.TerraformStateCandidates) +
		len(snapshot.TaintEvidence) + len(snapshot.InterprocTaintEvidence) +
		len(snapshot.FunctionSummaries) + len(snapshot.FunctionSources) +
		len(snapshot.DataflowFunctions) +
		dataflowScannedFactCount +
		(2 * len(snapshot.DeletedRelativePaths)) +
		gitobs.ObservabilityFactCount(snapshot.FileData) +
		gittfstate.TerraformStateBackendExpressionWarningFactCount(repo.ID, snapshot.FileData) +
		followupFactCount

	factCountAtomic := new(atomic.Int64)
	factCh := make(chan facts.Envelope, factStreamBuffer)
	go streamFacts(
		ctx,
		factCh,
		repoPath,
		repo,
		sourceRunID,
		scopeValue.ScopeID,
		generation.GenerationID,
		observedAt,
		&snapshot,
		isDependency,
		factCountAtomic,
		ref,
	)

	return collector.CollectedGeneration{
		Scope:              scopeValue,
		Generation:         generation,
		Facts:              factCh,
		EstimatedFactCount: factCount,
		FactCountAtomic:    factCountAtomic,
		DiscoveryAdvisory:  snapshot.DiscoveryAdvisory,
	}
}

// streamFacts emits fact envelopes through the channel and progressively
// releases snapshot data as it goes.
//
// Two-phase path (ContentFileMetas populated): re-reads each file body from
// disk when building content facts. Memory stays O(single_file) because the
// body is read, sent to the channel, and released before the next file.
//
// Legacy path (ContentFiles populated): bodies are already in memory from
// SnapshotRepository. Each entry is zeroed after sending.
//
// The count parameter is an atomic counter incremented on every send so the
// caller can read the exact emitted count after the channel drains.
func streamFacts(
	ctx context.Context,
	ch chan<- facts.Envelope,
	repoPath string,
	repo repositoryidentity.Metadata,
	sourceRunID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	snapshot *RepositorySnapshot,
	isDependency bool,
	count *atomic.Int64,
	ref string,
) {
	defer close(ch)

	w := gitmodel.NewFactStreamWriter(ch, count, ref)

	// Repository fact
	w.Send(repositoryFactEnvelope(
		repoPath, repo, sourceRunID, scopeID, generationID, observedAt,
		snapshot.FileCount, snapshot.ImportsMap, isDependency,
		snapshot.GitRefs,
		snapshot.Delta, snapshot.DeltaRelativePaths, snapshot.DeletedRelativePaths,
		snapshot.Reconcile,
	))

	// Terraform state candidate facts. These are metadata-only advisory facts;
	// raw state bytes are never read or persisted by the Git collector.
	for i, candidate := range snapshot.TerraformStateCandidates {
		w.Send(terraformStateCandidateFactEnvelope(repo.ID, scopeID, generationID, observedAt, candidate))
		snapshot.TerraformStateCandidates[i] = TerraformStateCandidate{}
	}
	snapshot.TerraformStateCandidates = nil

	gittfstate.EmitTerraformStateBackendExpressionWarnings(w, repo.ID, scopeID, generationID, observedAt, snapshot.FileData)

	// File metadata facts
	sourceRevisions := commitSHAByRelativePath(repoPath, snapshot)
	for i, fileData := range snapshot.FileData {
		w.Send(fileFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, fileData, isDependency))
		relativePath := gitmodel.RepositoryRelativePath(repoPath, gitmodel.PayloadPath(fileData, "path"))
		gitobs.EmitObservabilityFactsForFile(
			w, repoPath, repo.ID, scopeID, generationID, observedAt, fileData, sourceRevisions[relativePath],
		)
		snapshot.FileData[i] = nil
	}
	snapshot.FileData = nil
	for _, relativePath := range snapshot.DeletedRelativePaths {
		w.Send(fileTombstoneEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, relativePath, isDependency))
	}

	// Content file facts — two-phase re-read path or legacy path.
	gitDocumentationSourceEmitted := false
	documentationPaths := gitdocs.DocumentationMetaRelativePaths(snapshot.DocumentationFileMetas)
	// gitmodulesCandidates accumulates the .gitmodules body if present across
	// both content branches; there is exactly one recognized location (see
	// submodule.IsGitmodulesPath), so emission happens once after both
	// branches close (see noteSubmoduleCandidate).
	gitmodulesCandidates := map[string]string{}
	// codeownersCandidates accumulates recognized CODEOWNERS bodies across both
	// content branches; GitHub honors one file per repo, so emission happens
	// once after both branches close (see noteCodeownersCandidate).
	codeownersCandidates := map[string]string{}
	if len(snapshot.ContentFileMetas) > 0 {
		for i, meta := range snapshot.ContentFileMetas {
			body, err := streamContentBodyReadFile(filepath.Join(repoPath, filepath.FromSlash(meta.RelativePath))) // #nosec G304 -- reads indexed repo content file at a path derived from the scan target, not user-supplied input
			if err != nil {
				// File disappeared between parse and emit — skip.
				snapshot.ContentFileMetas[i] = gitmodel.ContentFileMeta{}
				continue
			}
			bodyStr := string(body)
			gitsubmodule.NoteSubmoduleCandidate(gitmodulesCandidates, meta.RelativePath, bodyStr)
			gitcodeowners.NoteCodeownersCandidate(codeownersCandidates, meta.RelativePath, bodyStr)

			w.Send(contentFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, gitmodel.ContentFileSnapshot{
				RelativePath:    meta.RelativePath,
				Body:            bodyStr,
				Digest:          meta.Digest,
				Language:        meta.Language,
				ArtifactType:    meta.ArtifactType,
				TemplateDialect: meta.TemplateDialect,
				IACRelevant:     meta.IACRelevant,
				CommitSHA:       meta.CommitSHA,
			}))
			gitsvccatalog.EmitServiceCatalogFactsForContentFile(w, scopeID, generationID, observedAt, meta.RelativePath, bodyStr)
			workflowimage.EmitWorkflowImageEvidenceFactsForContentFile(
				w,
				repo.ID,
				scopeID,
				generationID,
				observedAt,
				meta.RelativePath,
				meta.CommitSHA,
				bodyStr,
			)
			if !documentationPaths[meta.RelativePath] && gitdocs.EmitGitDocumentationFactsForContentFile(
				ctx,
				w,
				repoPath,
				repo,
				scopeID,
				generationID,
				observedAt,
				meta.RelativePath,
				meta.Digest,
				meta.CommitSHA,
				body,
				!gitDocumentationSourceEmitted,
			) {
				gitDocumentationSourceEmitted = true
			}
			snapshot.ContentFileMetas[i] = gitmodel.ContentFileMeta{}
		}
		snapshot.ContentFileMetas = nil
	} else {
		for i, fileSnapshot := range snapshot.ContentFiles {
			gitsubmodule.NoteSubmoduleCandidate(gitmodulesCandidates, fileSnapshot.RelativePath, fileSnapshot.Body)
			gitcodeowners.NoteCodeownersCandidate(codeownersCandidates, fileSnapshot.RelativePath, fileSnapshot.Body)
			w.Send(contentFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, fileSnapshot))
			gitsvccatalog.EmitServiceCatalogFactsForContentFile(
				w,
				scopeID,
				generationID,
				observedAt,
				fileSnapshot.RelativePath,
				fileSnapshot.Body,
			)
			workflowimage.EmitWorkflowImageEvidenceFactsForContentFile(
				w,
				repo.ID,
				scopeID,
				generationID,
				observedAt,
				fileSnapshot.RelativePath,
				fileSnapshot.CommitSHA,
				fileSnapshot.Body,
			)
			if !documentationPaths[fileSnapshot.RelativePath] && gitdocs.EmitGitDocumentationFactsForContentFile(
				ctx,
				w,
				repoPath,
				repo,
				scopeID,
				generationID,
				observedAt,
				fileSnapshot.RelativePath,
				fileSnapshot.Digest,
				fileSnapshot.CommitSHA,
				[]byte(fileSnapshot.Body),
				!gitDocumentationSourceEmitted,
			) {
				gitDocumentationSourceEmitted = true
			}
			snapshot.ContentFiles[i] = gitmodel.ContentFileSnapshot{}
		}
		snapshot.ContentFiles = nil
	}
	for i, meta := range snapshot.WorkflowImageFileMetas {
		body, err := streamContentBodyReadFile(filepath.Join(repoPath, filepath.FromSlash(meta.RelativePath))) // #nosec G304 -- reads an admitted workflow path from repository discovery
		if err != nil {
			snapshot.WorkflowImageFileMetas[i] = gitmodel.ContentFileMeta{}
			continue
		}
		workflowimage.EmitWorkflowImageEvidenceFactsForContentFile(
			w,
			repo.ID,
			scopeID,
			generationID,
			observedAt,
			meta.RelativePath,
			meta.CommitSHA,
			string(body),
		)
		snapshot.WorkflowImageFileMetas[i] = gitmodel.ContentFileMeta{}
	}
	snapshot.WorkflowImageFileMetas = nil
	gitTreePath := strings.TrimSpace(snapshot.GitTreePath)
	if gitTreePath == "" {
		gitTreePath = repoPath
	}
	gitsubmodule.EmitSubmoduleFactsForCandidates(ctx, w, repo.ID, gitTreePath, snapshot.HeadCommitSHA, scopeID, generationID, observedAt, gitmodulesCandidates)
	gitcodeowners.EmitCodeownersFactsForCandidates(w, repo.ID, scopeID, generationID, observedAt, codeownersCandidates)
	for i, meta := range snapshot.DocumentationFileMetas {
		body, ok := gitdocs.ReadDocumentationBody(repoPath, meta.RelativePath, nil)
		if !ok {
			snapshot.DocumentationFileMetas[i] = gitmodel.ContentFileMeta{}
			continue
		}
		if gitdocs.EmitGitDocumentationFactsForContentFile(
			ctx,
			w,
			repoPath,
			repo,
			scopeID,
			generationID,
			observedAt,
			meta.RelativePath,
			meta.Digest,
			meta.CommitSHA,
			body,
			!gitDocumentationSourceEmitted,
		) {
			gitDocumentationSourceEmitted = true
		}
		snapshot.DocumentationFileMetas[i] = gitmodel.ContentFileMeta{}
	}
	snapshot.DocumentationFileMetas = nil
	for _, relativePath := range snapshot.DeletedRelativePaths {
		w.Send(contentTombstoneEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, relativePath))
	}

	// Content entity facts
	for i, entitySnapshot := range snapshot.ContentEntities {
		w.Send(contentEntityFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, entitySnapshot))
		snapshot.ContentEntities[i] = gitmodel.ContentEntitySnapshot{}
	}
	snapshot.ContentEntities = nil

	// Value-flow taint evidence facts (opt-in via ESHU_EMIT_DATAFLOW; the slice is
	// empty otherwise so this loop is a no-op when the gate is off).
	for _, evidence := range snapshot.TaintEvidence {
		w.Send(taintEvidenceFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, evidence))
	}
	snapshot.TaintEvidence = nil
	for _, evidence := range snapshot.InterprocTaintEvidence {
		w.Send(interprocEvidenceFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, evidence))
	}
	snapshot.InterprocTaintEvidence = nil

	// Value-flow function summary facts (opt-in via ESHU_EMIT_DATAFLOW; the slice
	// is empty otherwise so this loop is a no-op when the gate is off). Emitted on
	// both delta and full generations: each summary upserts by its durable
	// FunctionID, so a delta that only re-summarizes changed files refreshes those
	// functions without disturbing the rest.
	for _, summary := range snapshot.FunctionSummaries {
		w.Send(functionSummaryFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, summary))
	}
	snapshot.FunctionSummaries = nil

	// Value-flow param-level source facts (opt-in via ESHU_EMIT_DATAFLOW; empty
	// otherwise). Emitted on both delta and full generations: each upserts by its
	// (FunctionID, param index) so a delta refreshes only changed files.
	for _, fnSource := range snapshot.FunctionSources {
		w.Send(functionSourceFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, fnSource))
	}
	snapshot.FunctionSources = nil
	for _, function := range snapshot.DataflowFunctions {
		w.Send(dataflowFunctionFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, function))
	}
	snapshot.DataflowFunctions = nil

	// Reducer follow-up facts — trigger downstream materialization domains.
	// codeowners_ownership and submodule_pin re-resolve the whole-repo candidate
	// set every generation and their reducer domains carry dedicated delta-scope
	// retract logic that is dead unless the marker fires on delta. Emit BEFORE the
	// delta early-return below so a delta that changes or removes CODEOWNERS/.gitmodules
	// re-projects (and sweeps stale edges). The data facts they consume are emitted
	// above (emitSubmoduleFactsForCandidates/emitCodeownersFactsForCandidates, which
	// re-read current disk state on both delta and full generations).
	w.Send(rationaleMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.Send(codeownersOwnershipFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.Send(submodulePinFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	if snapshot.Delta {
		w.Send(shellExecMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
		return
	}

	// Value-flow reconciliation marker — emitted only on full (non-delta)
	// generations whenever the gate ran, even with zero findings above. It must
	// NOT fire on deltas: a delta carries only changed-file findings, while the
	// evidence reducers retract the whole scope then write what they load, so a
	// marker-triggered delta would wipe evidence for unchanged files. On a full
	// generation the loaded finding set is complete, so retract-then-write is
	// correct and stale edges/nodes are cleared when the finding set goes empty
	// (#2919).
	if snapshot.DataflowScanned {
		w.Send(dataflowScannedFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	}

	w.Send(workloadIdentityFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.Send(deployableUnitCorrelationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.Send(workloadMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.Send(codeCallMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.Send(platformInfraMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.Send(deploymentMappingFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.Send(sqlRelationshipMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.Send(shellExecMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.Send(inheritanceMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.Send(codeImportRepoEdgeFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
}

// streamContentBodyReadFile is the seam streamFacts uses to read each content
// file body once at emit time. It is a package var so a test can count physical
// body reads and prove the #4877 change reads each candidate body exactly once
// (emit only) rather than twice (the removed pre-stream count pass plus emit).
var streamContentBodyReadFile = os.ReadFile
