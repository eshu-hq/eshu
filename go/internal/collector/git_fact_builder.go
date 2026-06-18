package collector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
) CollectedGeneration {
	return buildStreamingGenerationWithContext(context.Background(), repoPath, repo, sourceRunID, observedAt, snapshot, isDependency)
}

func buildStreamingGenerationWithContext(
	ctx context.Context,
	repoPath string,
	repo repositoryidentity.Metadata,
	sourceRunID string,
	observedAt time.Time,
	snapshot RepositorySnapshot,
	isDependency bool,
) CollectedGeneration {
	if ctx == nil {
		ctx = context.Background()
	}
	scopeValue := buildScope(repo)
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
	followupFactCount := 8
	if snapshot.Delta {
		followupFactCount = 1
	}
	dataflowScannedFactCount := 0
	if snapshot.DataflowScanned && !snapshot.Delta {
		dataflowScannedFactCount = 1
	}
	factCount := 1 + len(snapshot.FileData) + contentFileCount +
		len(snapshot.ContentEntities) + len(snapshot.TerraformStateCandidates) +
		len(snapshot.TaintEvidence) + len(snapshot.InterprocTaintEvidence) +
		len(snapshot.FunctionSummaries) + len(snapshot.FunctionSources) +
		dataflowScannedFactCount +
		(2 * len(snapshot.DeletedRelativePaths)) +
		observabilityFactCount(snapshot.FileData) +
		terraformStateBackendExpressionWarningFactCount(repo.ID, snapshot.FileData) +
		serviceCatalogFactCount(repoPath, scopeValue.ScopeID, generation.GenerationID, observedAt, snapshot) +
		gitDocumentationFactCount(ctx, repoPath, repo, scopeValue.ScopeID, generation.GenerationID, observedAt, snapshot) +
		workflowImageEvidenceFactCount(repoPath, snapshot) +
		followupFactCount

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
	)

	return CollectedGeneration{
		Scope:             scopeValue,
		Generation:        generation,
		Facts:             factCh,
		FactCount:         factCount,
		DiscoveryAdvisory: snapshot.DiscoveryAdvisory,
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
) {
	defer close(ch)

	// Repository fact
	ch <- repositoryFactEnvelope(
		repoPath, repo, sourceRunID, scopeID, generationID, observedAt,
		snapshot.FileCount, snapshot.ImportsMap, isDependency,
		snapshot.GitRefs,
		snapshot.Delta, snapshot.DeltaRelativePaths, snapshot.DeletedRelativePaths,
		snapshot.Reconcile,
	)

	// Terraform state candidate facts. These are metadata-only advisory facts;
	// raw state bytes are never read or persisted by the Git collector.
	for i, candidate := range snapshot.TerraformStateCandidates {
		ch <- terraformStateCandidateFactEnvelope(repo.ID, scopeID, generationID, observedAt, candidate)
		snapshot.TerraformStateCandidates[i] = TerraformStateCandidate{}
	}
	snapshot.TerraformStateCandidates = nil

	emitTerraformStateBackendExpressionWarnings(ch, repo.ID, scopeID, generationID, observedAt, snapshot.FileData)

	// File metadata facts
	sourceRevisions := commitSHAByRelativePath(repoPath, snapshot)
	for i, fileData := range snapshot.FileData {
		ch <- fileFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, fileData, isDependency)
		relativePath := repositoryRelativePath(repoPath, payloadPath(fileData, "path"))
		emitObservabilityFactsForFile(
			ch, repoPath, repo.ID, scopeID, generationID, observedAt, fileData, sourceRevisions[relativePath],
		)
		snapshot.FileData[i] = nil
	}
	snapshot.FileData = nil
	for _, relativePath := range snapshot.DeletedRelativePaths {
		ch <- fileTombstoneEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, relativePath, isDependency)
	}

	// Content file facts — two-phase re-read path or legacy path.
	gitDocumentationSourceEmitted := false
	documentationPaths := documentationMetaRelativePaths(snapshot.DocumentationFileMetas)
	if len(snapshot.ContentFileMetas) > 0 {
		for i, meta := range snapshot.ContentFileMetas {
			body, err := os.ReadFile(filepath.Join(repoPath, filepath.FromSlash(meta.RelativePath)))
			if err != nil {
				// File disappeared between parse and emit — skip.
				snapshot.ContentFileMetas[i] = ContentFileMeta{}
				continue
			}
			bodyStr := string(body)

			ch <- contentFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, ContentFileSnapshot{
				RelativePath:    meta.RelativePath,
				Body:            bodyStr,
				Digest:          meta.Digest,
				Language:        meta.Language,
				ArtifactType:    meta.ArtifactType,
				TemplateDialect: meta.TemplateDialect,
				IACRelevant:     meta.IACRelevant,
				CommitSHA:       meta.CommitSHA,
			})
			emitServiceCatalogFactsForContentFile(ch, scopeID, generationID, observedAt, meta.RelativePath, bodyStr)
			emitWorkflowImageEvidenceFactsForContentFile(
				ch,
				repo.ID,
				scopeID,
				generationID,
				observedAt,
				meta.RelativePath,
				bodyStr,
			)
			if !documentationPaths[meta.RelativePath] && emitGitDocumentationFactsForContentFile(
				ctx,
				ch,
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
			ch <- contentFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, fileSnapshot)
			emitServiceCatalogFactsForContentFile(
				ch,
				scopeID,
				generationID,
				observedAt,
				fileSnapshot.RelativePath,
				fileSnapshot.Body,
			)
			emitWorkflowImageEvidenceFactsForContentFile(
				ch,
				repo.ID,
				scopeID,
				generationID,
				observedAt,
				fileSnapshot.RelativePath,
				fileSnapshot.Body,
			)
			if !documentationPaths[fileSnapshot.RelativePath] && emitGitDocumentationFactsForContentFile(
				ctx,
				ch,
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
	for i, meta := range snapshot.DocumentationFileMetas {
		body, ok := readDocumentationBody(repoPath, meta.RelativePath, nil)
		if !ok {
			snapshot.DocumentationFileMetas[i] = ContentFileMeta{}
			continue
		}
		if emitGitDocumentationFactsForContentFile(
			ctx,
			ch,
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
		ch <- contentTombstoneEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, relativePath)
	}

	// Content entity facts
	for i, entitySnapshot := range snapshot.ContentEntities {
		ch <- contentEntityFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, entitySnapshot)
		snapshot.ContentEntities[i] = ContentEntitySnapshot{}
	}
	snapshot.ContentEntities = nil

	// Value-flow taint evidence facts (opt-in via ESHU_EMIT_DATAFLOW; the slice is
	// empty otherwise so this loop is a no-op when the gate is off).
	for _, evidence := range snapshot.TaintEvidence {
		ch <- taintEvidenceFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, evidence)
	}
	snapshot.TaintEvidence = nil
	for _, evidence := range snapshot.InterprocTaintEvidence {
		ch <- interprocEvidenceFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, evidence)
	}
	snapshot.InterprocTaintEvidence = nil

	// Value-flow function summary facts (opt-in via ESHU_EMIT_DATAFLOW; the slice
	// is empty otherwise so this loop is a no-op when the gate is off). Emitted on
	// both delta and full generations: each summary upserts by its durable
	// FunctionID, so a delta that only re-summarizes changed files refreshes those
	// functions without disturbing the rest.
	for _, summary := range snapshot.FunctionSummaries {
		ch <- functionSummaryFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, summary)
	}
	snapshot.FunctionSummaries = nil

	// Value-flow param-level source facts (opt-in via ESHU_EMIT_DATAFLOW; empty
	// otherwise). Emitted on both delta and full generations: each upserts by its
	// (FunctionID, param index) so a delta refreshes only changed files.
	for _, fnSource := range snapshot.FunctionSources {
		ch <- functionSourceFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, fnSource)
	}
	snapshot.FunctionSources = nil

	// Reducer follow-up facts — trigger downstream materialization domains.
	if snapshot.Delta {
		ch <- shellExecMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt)
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
		ch <- dataflowScannedFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt)
	}

	ch <- workloadIdentityFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt)
	ch <- deployableUnitCorrelationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt)
	ch <- workloadMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt)
	ch <- codeCallMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt)
	ch <- deploymentMappingFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt)
	ch <- sqlRelationshipMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt)
	ch <- shellExecMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt)
	ch <- inheritanceMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt)
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
