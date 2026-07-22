// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

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
) CollectedGeneration {
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
) CollectedGeneration {
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
	followupFactCount := 11
	if snapshot.Delta {
		followupFactCount = 1
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
		observabilityFactCount(snapshot.FileData) +
		terraformStateBackendExpressionWarningFactCount(repo.ID, snapshot.FileData) +
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

	return CollectedGeneration{
		Scope:              scopeValue,
		Generation:         generation,
		Facts:              factCh,
		EstimatedFactCount: factCount,
		factCountAtomic:    factCountAtomic,
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

	w := factStreamWriter{ch: ch, count: count, ref: ref}

	// Repository fact
	w.send(repositoryFactEnvelope(
		repoPath, repo, sourceRunID, scopeID, generationID, observedAt,
		snapshot.FileCount, snapshot.ImportsMap, isDependency,
		snapshot.GitRefs,
		snapshot.Delta, snapshot.DeltaRelativePaths, snapshot.DeletedRelativePaths,
		snapshot.Reconcile,
	))

	// Terraform state candidate facts. These are metadata-only advisory facts;
	// raw state bytes are never read or persisted by the Git collector.
	for i, candidate := range snapshot.TerraformStateCandidates {
		w.send(terraformStateCandidateFactEnvelope(repo.ID, scopeID, generationID, observedAt, candidate))
		snapshot.TerraformStateCandidates[i] = TerraformStateCandidate{}
	}
	snapshot.TerraformStateCandidates = nil

	emitTerraformStateBackendExpressionWarnings(w, repo.ID, scopeID, generationID, observedAt, snapshot.FileData)

	// File metadata facts
	sourceRevisions := commitSHAByRelativePath(repoPath, snapshot)
	for i, fileData := range snapshot.FileData {
		w.send(fileFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, fileData, isDependency))
		relativePath := repositoryRelativePath(repoPath, payloadPath(fileData, "path"))
		emitObservabilityFactsForFile(
			w, repoPath, repo.ID, scopeID, generationID, observedAt, fileData, sourceRevisions[relativePath],
		)
		snapshot.FileData[i] = nil
	}
	snapshot.FileData = nil
	for _, relativePath := range snapshot.DeletedRelativePaths {
		w.send(fileTombstoneEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, relativePath, isDependency))
	}

	// Content file facts — two-phase re-read path or legacy path.
	gitDocumentationSourceEmitted := false
	documentationPaths := documentationMetaRelativePaths(snapshot.DocumentationFileMetas)
	// codeownersCandidates accumulates recognized CODEOWNERS bodies across both
	// content branches; GitHub honors one file per repo, so emission happens
	// once after both branches close (see noteCodeownersCandidate).
	codeownersCandidates := map[string]string{}
	if len(snapshot.ContentFileMetas) > 0 {
		for i, meta := range snapshot.ContentFileMetas {
			body, err := streamContentBodyReadFile(filepath.Join(repoPath, filepath.FromSlash(meta.RelativePath))) // #nosec G304 -- reads indexed repo content file at a path derived from the scan target, not user-supplied input
			if err != nil {
				// File disappeared between parse and emit — skip.
				snapshot.ContentFileMetas[i] = ContentFileMeta{}
				continue
			}
			bodyStr := string(body)
			noteCodeownersCandidate(codeownersCandidates, meta.RelativePath, bodyStr)

			w.send(contentFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, ContentFileSnapshot{
				RelativePath:    meta.RelativePath,
				Body:            bodyStr,
				Digest:          meta.Digest,
				Language:        meta.Language,
				ArtifactType:    meta.ArtifactType,
				TemplateDialect: meta.TemplateDialect,
				IACRelevant:     meta.IACRelevant,
				CommitSHA:       meta.CommitSHA,
			}))
			emitServiceCatalogFactsForContentFile(w, scopeID, generationID, observedAt, meta.RelativePath, bodyStr)
			emitWorkflowImageEvidenceFactsForContentFile(
				w,
				repo.ID,
				scopeID,
				generationID,
				observedAt,
				meta.RelativePath,
				bodyStr,
			)
			if !documentationPaths[meta.RelativePath] && emitGitDocumentationFactsForContentFile(
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
			snapshot.ContentFileMetas[i] = ContentFileMeta{}
		}
		snapshot.ContentFileMetas = nil
	} else {
		for i, fileSnapshot := range snapshot.ContentFiles {
			noteCodeownersCandidate(codeownersCandidates, fileSnapshot.RelativePath, fileSnapshot.Body)
			w.send(contentFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, fileSnapshot))
			emitServiceCatalogFactsForContentFile(
				w,
				scopeID,
				generationID,
				observedAt,
				fileSnapshot.RelativePath,
				fileSnapshot.Body,
			)
			emitWorkflowImageEvidenceFactsForContentFile(
				w,
				repo.ID,
				scopeID,
				generationID,
				observedAt,
				fileSnapshot.RelativePath,
				fileSnapshot.Body,
			)
			if !documentationPaths[fileSnapshot.RelativePath] && emitGitDocumentationFactsForContentFile(
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
			snapshot.ContentFiles[i] = ContentFileSnapshot{}
		}
		snapshot.ContentFiles = nil
	}
	emitCodeownersFactsForCandidates(w, repo.ID, scopeID, generationID, observedAt, codeownersCandidates)
	for i, meta := range snapshot.DocumentationFileMetas {
		body, ok := readDocumentationBody(repoPath, meta.RelativePath, nil)
		if !ok {
			snapshot.DocumentationFileMetas[i] = ContentFileMeta{}
			continue
		}
		if emitGitDocumentationFactsForContentFile(
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
		snapshot.DocumentationFileMetas[i] = ContentFileMeta{}
	}
	snapshot.DocumentationFileMetas = nil
	for _, relativePath := range snapshot.DeletedRelativePaths {
		w.send(contentTombstoneEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, relativePath))
	}

	// Content entity facts
	for i, entitySnapshot := range snapshot.ContentEntities {
		w.send(contentEntityFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, entitySnapshot))
		snapshot.ContentEntities[i] = ContentEntitySnapshot{}
	}
	snapshot.ContentEntities = nil

	// Value-flow taint evidence facts (opt-in via ESHU_EMIT_DATAFLOW; the slice is
	// empty otherwise so this loop is a no-op when the gate is off).
	for _, evidence := range snapshot.TaintEvidence {
		w.send(taintEvidenceFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, evidence))
	}
	snapshot.TaintEvidence = nil
	for _, evidence := range snapshot.InterprocTaintEvidence {
		w.send(interprocEvidenceFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, evidence))
	}
	snapshot.InterprocTaintEvidence = nil

	// Value-flow function summary facts (opt-in via ESHU_EMIT_DATAFLOW; the slice
	// is empty otherwise so this loop is a no-op when the gate is off). Emitted on
	// both delta and full generations: each summary upserts by its durable
	// FunctionID, so a delta that only re-summarizes changed files refreshes those
	// functions without disturbing the rest.
	for _, summary := range snapshot.FunctionSummaries {
		w.send(functionSummaryFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, summary))
	}
	snapshot.FunctionSummaries = nil

	// Value-flow param-level source facts (opt-in via ESHU_EMIT_DATAFLOW; empty
	// otherwise). Emitted on both delta and full generations: each upserts by its
	// (FunctionID, param index) so a delta refreshes only changed files.
	for _, fnSource := range snapshot.FunctionSources {
		w.send(functionSourceFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, fnSource))
	}
	snapshot.FunctionSources = nil
	for _, function := range snapshot.DataflowFunctions {
		w.send(dataflowFunctionFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, function))
	}
	snapshot.DataflowFunctions = nil

	// Reducer follow-up facts — trigger downstream materialization domains.
	if snapshot.Delta {
		w.send(shellExecMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
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
		w.send(dataflowScannedFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	}

	w.send(workloadIdentityFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.send(deployableUnitCorrelationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.send(workloadMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.send(codeCallMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.send(platformInfraMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.send(deploymentMappingFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.send(sqlRelationshipMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.send(shellExecMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.send(inheritanceMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	w.send(codeImportRepoEdgeFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
	// Unconditional (not gated on CODEOWNERS presence): runs the domain every
	// generation so delta-retract sweeps stale edges even when CODEOWNERS is removed.
	w.send(codeownersOwnershipFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt))
}

// streamContentBodyReadFile is the seam streamFacts uses to read each content
// file body once at emit time. It is a package var so a test can count physical
// body reads and prove the #4877 change reads each candidate body exactly once
// (emit only) rather than twice (the removed pre-stream count pass plus emit).
var streamContentBodyReadFile = os.ReadFile

// factStreamWriter wraps a fact channel with an atomic counter. Every send
// through this writer increments the counter atomically so the stream
// produces an exact post-drain count without pre-reading file bodies.
type factStreamWriter struct {
	ch    chan<- facts.Envelope
	count *atomic.Int64
	ref   string
}

func (w factStreamWriter) send(env facts.Envelope) {
	if w.ref != "" {
		if env.Payload == nil {
			env.Payload = map[string]any{}
		}
		env.Payload["ref"] = w.ref
	}
	w.count.Add(1)
	w.ch <- env
}

func repositoryFactEnvelope(
	repoPath string,
	repo repositoryidentity.Metadata,
	sourceRunID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	parsedFileCount int,
	importsMap map[string][]string,
	isDependency bool,
	gitRefs []GitRef,
	delta bool,
	deltaRelativePaths []string,
	deltaDeletedRelativePaths []string,
	reconcile bool,
) facts.Envelope {
	payload := map[string]any{
		"graph_id":          repo.ID,
		"graph_kind":        "repository",
		"name":              repo.Name,
		"repo_id":           repo.ID,
		"parsed_file_count": fmt.Sprintf("%d", parsedFileCount),
		"is_dependency":     isDependency,
	}
	if repo.RepoSlug != "" {
		payload["repo_slug"] = repo.RepoSlug
	}
	if repo.RemoteURL != "" {
		payload["remote_url"] = repo.RemoteURL
	}
	if repo.LocalPath != "" {
		payload["local_path"] = repo.LocalPath
	}
	if len(importsMap) > 0 {
		payload["imports_map"] = importsMap
	}
	if defaultBranch := repositoryDefaultBranch(gitRefs); defaultBranch != "" {
		payload["default_branch"] = defaultBranch
	}
	if refsPayload := repositoryFactGitRefsPayload(gitRefs); len(refsPayload) > 0 {
		payload["git_refs"] = refsPayload
	}
	if delta {
		payload["delta_generation"] = true
		payload["delta_relative_paths"] = append([]string(nil), deltaRelativePaths...)
		payload["delta_deleted_relative_paths"] = append([]string(nil), deltaDeletedRelativePaths...)
	}
	if reconcile {
		payload["reconciliation_generation"] = true
	}
	if strings.TrimSpace(sourceRunID) != "" {
		payload["source_run_id"] = sourceRunID
	}

	return factEnvelope("repository", scopeID, generationID, observedAt, "repository:"+repo.ID, payload, repoPath)
}

func fileFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	fileData map[string]any,
	isDependency bool,
) facts.Envelope {
	filePath := payloadPath(fileData, "path")
	relativePath := repositoryRelativePath(repoPath, filePath)
	payload := map[string]any{
		"graph_id":         repoID + ":" + relativePath,
		"graph_kind":       "file",
		"repo_id":          repoID,
		"relative_path":    relativePath,
		"parsed_file_data": fileData,
		"is_dependency":    isDependency,
	}
	if language := payloadString(fileData, "language", "lang"); language != "" {
		payload["language"] = language
	}

	return factEnvelope("file", scopeID, generationID, observedAt, "file:"+repoID+":"+relativePath, payload, filePath)
}
