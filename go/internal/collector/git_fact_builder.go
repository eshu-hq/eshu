package collector

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	scopeValue := buildScope(repo)
	generation := buildGeneration(
		scopeValue.ScopeID,
		sourceRunID,
		repoPath,
		observedAt,
		snapshotFreshnessHint(snapshot),
	)
	contentFileCount := len(snapshot.ContentFiles)
	if len(snapshot.ContentFileMetas) > 0 {
		contentFileCount = len(snapshot.ContentFileMetas)
	}
	factCount := 1 + len(snapshot.FileData) + contentFileCount + len(snapshot.ContentEntities) + 2

	factCh := make(chan facts.Envelope, factStreamBuffer)
	go streamFacts(
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
	)

	// File metadata facts
	for i, fileData := range snapshot.FileData {
		ch <- fileFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, fileData, isDependency)
		snapshot.FileData[i] = nil
	}
	snapshot.FileData = nil

	// Content file facts — two-phase re-read path or legacy path.
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
			snapshot.ContentFileMetas[i] = ContentFileMeta{}
		}
		snapshot.ContentFileMetas = nil
	} else {
		for i, fileSnapshot := range snapshot.ContentFiles {
			ch <- contentFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, fileSnapshot)
			snapshot.ContentFiles[i] = ContentFileSnapshot{}
		}
		snapshot.ContentFiles = nil
	}

	// Content entity facts
	for i, entitySnapshot := range snapshot.ContentEntities {
		ch <- contentEntityFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt, entitySnapshot)
		snapshot.ContentEntities[i] = ContentEntitySnapshot{}
	}
	snapshot.ContentEntities = nil

	// Reducer follow-up facts — trigger downstream materialization domains.
	ch <- workloadIdentityFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt)
	ch <- deployableUnitCorrelationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt)
	ch <- workloadMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt)
	ch <- codeCallMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt)
	ch <- deploymentMappingFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt)
	ch <- sqlRelationshipMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt)
	ch <- inheritanceMaterializationFactEnvelope(repoPath, repo.ID, scopeID, generationID, observedAt)
}

// snapshotFreshnessHint computes a deterministic hash from file digests and
// entity metadata. This replaces the old approach that JSON-marshaled the
// entire snapshot (including all file bodies) which created a massive
// temporary allocation for large repos.
func snapshotFreshnessHint(snapshot RepositorySnapshot) string {
	h := sha256.New()
	writeFreshnessHashf(h, "v2:file_count=%d\n", snapshot.FileCount)

	// Hash file digests — already computed during materialization.
	// ContentFileMetas is the two-phase path; ContentFiles is the legacy path.
	if len(snapshot.ContentFileMetas) > 0 {
		for _, meta := range snapshot.ContentFileMetas {
			writeFreshnessHashf(h, "file:%s:%s\n", meta.RelativePath, meta.Digest)
		}
	} else {
		for _, cf := range snapshot.ContentFiles {
			writeFreshnessHashf(h, "file:%s:%s\n", cf.RelativePath, cf.Digest)
		}
	}

	// Hash entity count and identity (lightweight).
	writeFreshnessHashf(h, "entities=%d\n", len(snapshot.ContentEntities))
	for _, e := range snapshot.ContentEntities {
		writeFreshnessHashf(h, "entity:%s:%s:%d\n", e.RelativePath, e.EntityType, e.StartLine)
	}

	// Hash imports map keys (sorted for determinism).
	importKeys := make([]string, 0, len(snapshot.ImportsMap))
	for k := range snapshot.ImportsMap {
		importKeys = append(importKeys, k)
	}
	sort.Strings(importKeys)
	for _, k := range importKeys {
		writeFreshnessHashf(h, "import:%s:", k)
		targets := snapshot.ImportsMap[k]
		sorted := make([]string, len(targets))
		copy(sorted, targets)
		sort.Strings(sorted)
		for _, v := range sorted {
			writeFreshnessHashf(h, "%s,", v)
		}
		_, _ = h.Write([]byte("\n"))
	}

	return fmt.Sprintf("snapshot:%x", h.Sum(nil))
}

func writeFreshnessHashf(h interface{ Write([]byte) (int, error) }, format string, args ...any) {
	_, _ = fmt.Fprintf(h, format, args...)
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

func contentFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	fileSnapshot ContentFileSnapshot,
) facts.Envelope {
	payload := map[string]any{
		"content_path":   fileSnapshot.RelativePath,
		"content_body":   fileSnapshot.Body,
		"content_digest": fileSnapshot.Digest,
		"repo_id":        repoID,
	}
	if fileSnapshot.Language != "" {
		payload["language"] = fileSnapshot.Language
	}
	if fileSnapshot.CommitSHA != "" {
		payload["commit_sha"] = fileSnapshot.CommitSHA
	}
	if fileSnapshot.ArtifactType != "" {
		payload["artifact_type"] = fileSnapshot.ArtifactType
	}
	if fileSnapshot.TemplateDialect != "" {
		payload["template_dialect"] = fileSnapshot.TemplateDialect
	}
	if fileSnapshot.IACRelevant != nil {
		payload["iac_relevant"] = strings.ToLower(fmt.Sprintf("%t", *fileSnapshot.IACRelevant))
	}

	return factEnvelope(
		"content",
		scopeID,
		generationID,
		observedAt,
		"content:"+repoID+":"+fileSnapshot.RelativePath,
		payload,
		filepath.Join(repoPath, filepath.FromSlash(fileSnapshot.RelativePath)),
	)
}

func contentEntityFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	entitySnapshot ContentEntitySnapshot,
) facts.Envelope {
	payload := map[string]any{
		"graph_id":      entitySnapshot.EntityID,
		"graph_kind":    "content_entity",
		"entity_id":     entitySnapshot.EntityID,
		"repo_id":       repoID,
		"relative_path": entitySnapshot.RelativePath,
		"entity_type":   entitySnapshot.EntityType,
		"entity_name":   entitySnapshot.EntityName,
		"start_line":    entitySnapshot.StartLine,
		"end_line":      entitySnapshot.EndLine,
		"language":      entitySnapshot.Language,
		"source_cache":  entitySnapshot.SourceCache,
		"indexed_at":    entitySnapshot.IndexedAt.UTC().Format(time.RFC3339Nano),
	}
	if entitySnapshot.StartByte != nil {
		payload["start_byte"] = *entitySnapshot.StartByte
	}
	if entitySnapshot.EndByte != nil {
		payload["end_byte"] = *entitySnapshot.EndByte
	}
	if entitySnapshot.ArtifactType != "" {
		payload["artifact_type"] = entitySnapshot.ArtifactType
	}
	if entitySnapshot.TemplateDialect != "" {
		payload["template_dialect"] = entitySnapshot.TemplateDialect
	}
	if entitySnapshot.IACRelevant != nil {
		payload["iac_relevant"] = *entitySnapshot.IACRelevant
	}
	if len(entitySnapshot.Metadata) > 0 {
		payload["entity_metadata"] = cloneAnyMap(entitySnapshot.Metadata)
	}

	return factEnvelope(
		"content_entity",
		scopeID,
		generationID,
		observedAt,
		"content_entity:"+entitySnapshot.EntityID,
		payload,
		filepath.Join(repoPath, filepath.FromSlash(entitySnapshot.RelativePath)),
	)
}

func workloadIdentityFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "workload_identity",
		"entity_key":     "workload:" + filepath.Base(repoPath),
		"reason":         "repository snapshot emitted shared workload identity follow-up",
		"repo_id":        repoID,
	}

	return factEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":workload_identity",
		payload,
		repoPath,
	)
}

func deployableUnitCorrelationFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "deployable_unit_correlation",
		"entity_key":     "repo:" + filepath.Base(repoPath),
		"reason":         "repository snapshot emitted deployable-unit correlation follow-up",
		"repo_id":        repoID,
	}

	return factEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":deployable_unit_correlation",
		payload,
		repoPath,
	)
}

func codeCallMaterializationFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "code_call_materialization",
		"entity_key":     "repo:" + filepath.Base(repoPath),
		"reason":         "repository snapshot emitted code-call materialization follow-up",
		"repo_id":        repoID,
	}

	return factEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":code_call_materialization",
		payload,
		repoPath,
	)
}

func workloadMaterializationFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "workload_materialization",
		"entity_key":     "workload:" + filepath.Base(repoPath),
		"reason":         "repository snapshot emitted workload materialization follow-up",
		"repo_id":        repoID,
	}

	return factEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":workload_materialization",
		payload,
		repoPath,
	)
}

func deploymentMappingFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "deployment_mapping",
		"entity_key":     "deployment:" + filepath.Base(repoPath),
		"reason":         "repository snapshot emitted deployment mapping follow-up",
		"repo_id":        repoID,
	}

	return factEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":deployment_mapping",
		payload,
		repoPath,
	)
}

func sqlRelationshipMaterializationFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "sql_relationship_materialization",
		"entity_key":     "sql:" + filepath.Base(repoPath),
		"reason":         "repository snapshot emitted SQL relationship materialization follow-up",
		"repo_id":        repoID,
	}

	return factEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":sql_relationship_materialization",
		payload,
		repoPath,
	)
}

func inheritanceMaterializationFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "inheritance_materialization",
		"entity_key":     "inheritance:" + filepath.Base(repoPath),
		"reason":         "repository snapshot emitted inheritance materialization follow-up",
		"repo_id":        repoID,
	}

	return factEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":inheritance_materialization",
		payload,
		repoPath,
	)
}

func factEnvelope(
	factKind string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	factKey string,
	payload map[string]any,
	sourceURI string,
) facts.Envelope {
	return facts.Envelope{
		FactID: facts.StableID(
			"GoGitCollectorFact",
			map[string]any{
				"fact_key":      factKey,
				"fact_kind":     factKind,
				"generation_id": generationID,
				"scope_id":      scopeID,
			},
		),
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         factKind,
		StableFactKey:    factKey,
		CollectorKind:    "git",
		SourceConfidence: "observed",
		ObservedAt:       observedAt,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   "git",
			ScopeID:        scopeID,
			GenerationID:   generationID,
			FactKey:        factKey,
			SourceURI:      sourceURI,
			SourceRecordID: factKey,
		},
	}
}

func repositoryRelativePath(repoPath string, filePath string) string {
	relativePath, err := filepath.Rel(repoPath, filePath)
	if err != nil {
		return filepath.Base(filePath)
	}
	return filepath.ToSlash(relativePath)
}

func payloadString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		return text
	}
	return ""
}

func payloadPath(payload map[string]any, key string) string {
	value := payloadString(payload, key)
	if value == "" {
		return ""
	}
	resolved, err := filepath.Abs(value)
	if err != nil {
		return value
	}
	return resolved
}
