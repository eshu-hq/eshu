// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/git/content"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/codeowners"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/docs"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/observability"
	runtimedeclaration "github.com/eshu-hq/eshu/go/internal/collector/repo/git/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/service/catalog"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/submodule"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/tfstate"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/workflow/image"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
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

// generationReconcileReason returns the reason a git generation records for a
// forced reconciliation snapshot (#7978): the decision reason, or
// GenerationReconcileReasonUnspecified when the snapshot carried none; empty
// for an ordinary snapshot.
func generationReconcileReason(snapshot RepositorySnapshot) string {
	if !snapshot.Reconcile {
		return ""
	}
	if snapshot.ReconcileReason != "" {
		return snapshot.ReconcileReason
	}
	return GenerationReconcileReasonUnspecified
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
	generation := buildGeneration(
		scopeValue.ScopeID,
		sourceRunID,
		repoPath,
		observedAt,
		snapshotFreshnessHint(snapshot),
		snapshot.HeadCommitSHA,
		snapshot.Delta,
	)
	generation.ReconcileReason = generationReconcileReason(snapshot)
	generation.DefaultBranchChanged = snapshot.DefaultBranchChanged
	if !snapshot.Delta {
		generation.PreparseIdentity = snapshot.PreparseIdentity
	}
	if snapshot.Delta {
		generation.DeltaBaselineCommitSHA, generation.DeltaUnionBaseCommitSHA = snapshot.DeltaBaselineCommitSHA, snapshot.DeltaUnionBaseCommitSHA
	}
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
		observability.ObservabilityFactCount(snapshot.FileData) +
		tfstate.TerraformStateBackendExpressionWarningFactCount(repo.ID, snapshot.FileData) +
		followupFactCount

	factCountAtomic := new(atomic.Int64)
	// dropped is set by the stream when it skips a file it could not read
	// (review F-7). The stream runs after this function returns, so the store
	// reads it through PreparseIdentityDropped after the drain. It is wired
	// whether or not the generation has an identity (review F7-5): a drop on an
	// identity-less generation must still mark it stream_dropped.
	dropped := new(atomic.Bool)
	generation.PreparseIdentityDropped = dropped
	// droppedPaths accumulates the skipped files' paths (#8185) for the
	// stream_dropped mark, so the next equal snapshot can verify the drop.
	droppedPaths := new(scope.StreamDroppedPaths)
	generation.PreparseIdentityDroppedPaths = droppedPaths
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
		dropped,
		droppedPaths,
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
	dropped *atomic.Bool,
	droppedPaths *scope.StreamDroppedPaths,
) {
	defer close(ch)

	w := model.NewFactStreamWriter(ch, count, ref)

	// Repository fact (empty_scope declared only for walked-empty snapshots).
	w.Send(gitcontent.RepositoryFactEnvelope(
		repoPath, repo, sourceRunID, scopeID, generationID, observedAt,
		snapshot.FileCount, snapshot.ImportsMap, isDependency,
		repositoryDefaultBranch(snapshot.GitRefs),
		repositoryFactGitRefsPayload(snapshot.GitRefs),
		snapshot.Delta, snapshot.DeltaRelativePaths, snapshot.DeletedRelativePaths,
		snapshot.Reconcile,
		snapshotDeclaresEmptyScope(snapshot),
	))

	// Terraform state candidate facts. These are metadata-only advisory facts;
	// raw state bytes are never read or persisted by the Git collector.
	for i, candidate := range snapshot.TerraformStateCandidates {
		w.Send(terraformStateCandidateFactEnvelope(repo.ID, scopeID, generationID, observedAt, candidate))
		snapshot.TerraformStateCandidates[i] = TerraformStateCandidate{}
	}
	snapshot.TerraformStateCandidates = nil

	tfstate.EmitTerraformStateBackendExpressionWarnings(w, repo.ID, scopeID, generationID, observedAt, snapshot.FileData)

	// File metadata facts
	sourceRevisions := commitSHAByRelativePath(repoPath, snapshot)
	for i, fileData := range snapshot.FileData {
		w.Send(gitcontent.FileFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, fileData, isDependency))
		relativePath := model.RepositoryRelativePath(repoPath, model.PayloadPath(fileData, "path"))
		observability.EmitObservabilityFactsForFile(w, repoPath, repo.ID, scopeID, generationID, observedAt, fileData, sourceRevisions[relativePath])
		snapshot.FileData[i] = nil
	}
	snapshot.FileData = nil
	for _, relativePath := range snapshot.DeletedRelativePaths {
		w.Send(fileTombstoneEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, relativePath, isDependency))
	}

	// Content file facts — two-phase re-read path or legacy path.
	gitDocumentationSourceEmitted := false
	documentationPaths := docs.DocumentationMetaRelativePaths(snapshot.DocumentationFileMetas)
	// Candidate bodies accumulate across both content branches and emit once
	// after both close: .gitmodules (one recognized location, see
	// submodule.IsGitmodulesPath), CODEOWNERS (GitHub honors one file per
	// repo) and the version-pin files at any depth, each independent.
	var tally contentStoreTally // content-store policy outcomes (#8812)
	gitmodulesCandidates, codeownersCandidates, runtimeDeclarationCandidates := map[string]string{}, map[string]string{}, map[string]runtimedeclaration.Candidate{}
	if len(snapshot.ContentFileMetas) > 0 {
		for i, meta := range snapshot.ContentFileMetas {
			body, err := streamContentBodyReadFile(filepath.Join(repoPath, filepath.FromSlash(meta.RelativePath))) // #nosec G304 -- reads indexed repo content file at a path derived from the scan target, not user-supplied input
			if err != nil {
				// The file disappeared or became unreadable between parse and
				// emit: skip it and record the drop (review F-7, #8185).
				dropped.Store(true)
				droppedPaths.Note(meta.RelativePath)
				recordStreamDrop(ctx, identityDrop{
					Reason: telemetry.PreparseDropContentReadFailed, RepositoryID: repo.ID, ScopeID: scopeID,
					GenerationID: generationID, RelativePath: meta.RelativePath, Err: err,
				})
				snapshot.ContentFileMetas[i] = model.ContentFileMeta{}
				continue
			}
			body = tally.storedBytes(snapshot.ContentStores, meta.RelativePath, body) // every emitter below sees the stored body
			bodyStr := string(body)
			submodule.NoteSubmoduleCandidate(gitmodulesCandidates, meta.RelativePath, bodyStr)
			codeowners.NoteCodeownersCandidate(codeownersCandidates, meta.RelativePath, bodyStr)
			runtimedeclaration.NoteRuntimeDeclarationCandidate(runtimeDeclarationCandidates, meta.RelativePath, bodyStr)

			w.Send(gitcontent.ContentFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, model.ContentFileSnapshot{
				RelativePath:    meta.RelativePath,
				Body:            bodyStr,
				Digest:          meta.Digest,
				Language:        meta.Language,
				ArtifactType:    meta.ArtifactType,
				TemplateDialect: meta.TemplateDialect,
				IACRelevant:     meta.IACRelevant,
				CommitSHA:       meta.CommitSHA,
			}))
			catalog.EmitServiceCatalogFactsForContentFile(w, scopeID, generationID, observedAt, meta.RelativePath, bodyStr)
			image.EmitWorkflowImageEvidenceFactsForContentFile(
				w,
				repo.ID,
				scopeID,
				generationID,
				observedAt,
				meta.RelativePath,
				meta.CommitSHA,
				bodyStr,
			)
			if !documentationPaths[meta.RelativePath] && docs.EmitGitDocumentationFactsForContentFile(
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
			snapshot.ContentFileMetas[i] = model.ContentFileMeta{}
		}
		snapshot.ContentFileMetas = nil
	} else {
		for i, fileSnapshot := range snapshot.ContentFiles {
			fileSnapshot.Body = tally.storedBody(snapshot.ContentStores, fileSnapshot.RelativePath, fileSnapshot.Body)
			submodule.NoteSubmoduleCandidate(gitmodulesCandidates, fileSnapshot.RelativePath, fileSnapshot.Body)
			codeowners.NoteCodeownersCandidate(codeownersCandidates, fileSnapshot.RelativePath, fileSnapshot.Body)
			runtimedeclaration.NoteRuntimeDeclarationCandidate(runtimeDeclarationCandidates, fileSnapshot.RelativePath, fileSnapshot.Body)
			w.Send(gitcontent.ContentFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, fileSnapshot))
			catalog.EmitServiceCatalogFactsForContentFile(w,
				scopeID,
				generationID,
				observedAt,
				fileSnapshot.RelativePath,
				fileSnapshot.Body,
			)
			image.EmitWorkflowImageEvidenceFactsForContentFile(
				w,
				repo.ID,
				scopeID,
				generationID,
				observedAt,
				fileSnapshot.RelativePath,
				fileSnapshot.CommitSHA,
				fileSnapshot.Body,
			)
			if !documentationPaths[fileSnapshot.RelativePath] && docs.EmitGitDocumentationFactsForContentFile(
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
			snapshot.ContentFiles[i] = model.ContentFileSnapshot{}
		}
		snapshot.ContentFiles = nil
	}
	for i, meta := range snapshot.WorkflowImageFileMetas {
		body, err := streamContentBodyReadFile(filepath.Join(repoPath, filepath.FromSlash(meta.RelativePath))) // #nosec G304 -- reads an admitted workflow path from repository discovery
		if err != nil {
			dropped.Store(true)
			droppedPaths.Note(meta.RelativePath)
			recordStreamDrop(ctx, identityDrop{
				Reason: telemetry.PreparseDropWorkflowReadFailed, RepositoryID: repo.ID, ScopeID: scopeID,
				GenerationID: generationID, RelativePath: meta.RelativePath, Err: err,
			})
			snapshot.WorkflowImageFileMetas[i] = model.ContentFileMeta{}
			continue
		}
		image.EmitWorkflowImageEvidenceFactsForContentFile(
			w,
			repo.ID,
			scopeID,
			generationID,
			observedAt,
			meta.RelativePath,
			meta.CommitSHA,
			string(tally.storedBytes(snapshot.ContentStores, meta.RelativePath, body)),
		)
		snapshot.WorkflowImageFileMetas[i] = model.ContentFileMeta{}
	}
	snapshot.WorkflowImageFileMetas = nil
	gitTreePath := strings.TrimSpace(snapshot.GitTreePath)
	if gitTreePath == "" {
		gitTreePath = repoPath
	}
	submodule.EmitSubmoduleFactsForCandidates(ctx, w, repo.ID, gitTreePath, snapshot.HeadCommitSHA, scopeID, generationID, observedAt, gitmodulesCandidates)
	codeowners.EmitCodeownersFactsForCandidates(w, repo.ID, scopeID, generationID, observedAt, codeownersCandidates)
	runtimedeclaration.EmitRuntimeDeclarationFactsForCandidates(w, repo.ID, scopeID, generationID, observedAt, runtimeDeclarationCandidates, pinSkipRecorder(ctx, repo.ID, scopeID, generationID))
	for i, meta := range snapshot.DocumentationFileMetas {
		body, ok, readErr := docs.ReadDocumentationBodyWithError(repoPath, meta.RelativePath, nil)
		if !ok {
			if readErr != nil { // an unsupported format is deterministic and drops nothing
				dropped.Store(true)
				droppedPaths.Note(meta.RelativePath)
				recordStreamDrop(ctx, identityDrop{
					Reason: telemetry.PreparseDropDocsBodyReadFailed, RepositoryID: repo.ID, ScopeID: scopeID,
					GenerationID: generationID, RelativePath: meta.RelativePath, Err: readErr,
				})
			}
			snapshot.DocumentationFileMetas[i] = model.ContentFileMeta{}
			continue
		}
		if docs.EmitGitDocumentationFactsForContentFile(
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
			tally.storedBytes(snapshot.ContentStores, meta.RelativePath, body),
			!gitDocumentationSourceEmitted,
		) {
			gitDocumentationSourceEmitted = true
		}
		snapshot.DocumentationFileMetas[i] = model.ContentFileMeta{}
	}
	snapshot.DocumentationFileMetas = nil
	recordContentStoreTally(ctx, &tally, repo.ID, scopeID, generationID)
	for _, relativePath := range snapshot.DeletedRelativePaths {
		w.Send(contentTombstoneEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, relativePath))
	}

	// Content entity facts
	for i, entitySnapshot := range snapshot.ContentEntities {
		w.Send(gitcontent.ContentEntityFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, entitySnapshot))
		snapshot.ContentEntities[i] = model.ContentEntitySnapshot{}
	}
	snapshot.ContentEntities = nil

	emitValueFlowFacts(&w, repoPath, repo.ID, scopeID, generationID, observedAt, snapshot)

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
// The read is capped (#8986): a file that grew past shared.SourceReadCap after
// discovery is refused, not read whole, and the stream drops it as a read
// failure.
var streamContentBodyReadFile = shared.ReadFileCapped
