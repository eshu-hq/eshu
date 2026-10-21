// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/content"
	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Live-read reason labels of the stored path. mode_live and stored_error are
// owned here; every other reason arrives as the stored reader's Reason, which
// the production wiring maps 1:1 from the storage read-model reasons
// (missing, stale_version, stale_extractor, failed), and feeds
// eshu_dp_config_artifact_reads_total{source,reason} with the stage
// live_reasons field.
const (
	// StoredReadReasonModeLive counts related repositories served live
	// because the mode serves everything live (live and shadow mode).
	StoredReadReasonModeLive = "mode_live"
	// StoredReadReasonStoredError counts related repositories served live
	// because the whole stored read failed (or no reader is configured),
	// so every repository fell back to the live load.
	StoredReadReasonStoredError = "stored_error"
	// StoredReadReasonMissing counts a related repository the stored read
	// dropped, or an untrusted answer with an empty reason. The storage
	// reader never sends an empty reason for an untrusted repository, so
	// this is the adapter contract, not a second missing shape.
	StoredReadReasonMissing = "missing"
)

// StoredConfig carries the stored read of the deployment-artifact load (#8155
// PR3c stored cutover). The zero value disables it: every repository loads
// live, as today.
type StoredConfig struct {
	// Enabled serves the stored rows of trusted related repositories and
	// the live extractor for every other repository. The anchor repository
	// always loads live.
	Enabled bool
	// Reader answers the stored rows of the related repositories. It must
	// be set when Enabled is set; an enabled stored load with no reader
	// serves every repository live and counts stored_error instead of
	// failing the request.
	Reader StoredReader
}

// readMode reports the mode name the stage logs and the read_mode field
// carries: the ESHU_CONFIG_ARTIFACT_READ_MODE value this loader behaves as.
func (l configArtifactLoader) readMode() string {
	switch {
	case l.stored.Enabled:
		return string(content.ReadModeStored)
	case l.shadow.Enabled:
		return string(content.ReadModeShadow)
	default:
		return string(content.ReadModeLive)
	}
}

// countLiveRead counts n related repositories served live under reason.
func (s *ConfigArtifactReadStats) countLiveRead(reason string, n int) {
	if n <= 0 {
		return
	}
	s.LiveRepositoryCount += n
	if s.LiveReasons == nil {
		s.LiveReasons = map[string]int{}
	}
	s.LiveReasons[reason] += n
}

// LiveReasonsSummary renders the stage live_reasons field: the live-read
// reasons sorted by reason as reason=count pairs. It is empty when no
// related repository was served live.
func (s ConfigArtifactReadStats) LiveReasonsSummary() string {
	pairs := make([]string, 0, len(s.LiveReasons))
	for _, reason := range slices.Sorted(maps.Keys(s.LiveReasons)) {
		pairs = append(pairs, fmt.Sprintf("%s=%d", reason, s.LiveReasons[reason]))
	}
	return strings.Join(pairs, ",")
}

// loadForSourcesStored runs the stored load over sources: the anchor loads
// live, every trusted related repository serves its stored rows, and every
// other related repository falls back to the live load. The merged answer
// keeps the live contract: repositories in source order, one dedup map and
// one total sort (MergeConfigPaths), so two related repositories that share a
// graph name, or an anchor that also appears in the related list, collapse
// exactly as they do live. Stored repositories spend no request budget and
// never emit the budget reason; their window reason comes from the state's
// list_truncated instead. A failed whole stored read serves every repository
// live and counts stored_error, never failing the request.
func (l configArtifactLoader) loadForSourcesStored(
	ctx context.Context,
	reader querycontract.ContentStore,
	sources []repositoryArtifactSource,
	stats *ConfigArtifactReadStats,
) (configArtifactLoadResult, error) {
	ordered := filterOrderedSources(sources)
	if reader == nil || len(ordered) == 0 {
		return configArtifactLoadResult{}, nil
	}
	related := ordered[1:]
	stored := l.readStoredRelated(ctx, related, stats)
	if stored.err != nil {
		stats.countLiveRead(StoredReadReasonStoredError, len(related))
		return l.loadForSources(ctx, reader, ordered, stats)
	}
	liveSources := make([]repositoryArtifactSource, 0, len(ordered))
	liveSources = append(liveSources, ordered[0])
	storedRows := make(map[int][]map[string]any, len(related))
	for i, source := range related {
		repo, ok := stored.byID[source.RepoID]
		if !ok || !repo.Trusted {
			stats.countLiveRead(storedFallbackReason(repo, ok), 1)
			liveSources = append(liveSources, source)
			continue
		}
		named := contentartifacts.ApplyStoredNameStep(source.RepoName, repo.Rows)
		storedRows[i+1] = namedRowsToMaps(named)
		stats.StoredRepositoryCount++
		stats.StoredRowCount += len(repo.Rows)
		if repo.ListTruncated {
			stats.StoredRepositoriesListTruncated++
		}
	}
	live, err := l.loadForSources(ctx, reader, liveSources, stats)
	if err != nil {
		return configArtifactLoadResult{}, err
	}
	full := make([][]map[string]any, 0, len(ordered))
	at := 0
	for pos := range ordered {
		rows, ok := storedRows[pos]
		if !ok {
			rows = live.rows[at]
			at++
		}
		full = append(full, rows)
	}
	res := configArtifactLoadResult{rows: full, sources: ordered}
	merged := contentartifacts.MergeConfigPaths(full...)
	if len(merged) == 0 {
		return res, nil
	}
	res.merged = map[string]any{contentartifacts.ConfigPathsKey: merged}
	return res, nil
}

// storedRelatedRead is one stored read of the related repositories: the
// answers by repository id, or err when the whole read failed.
type storedRelatedRead struct {
	byID map[string]StoredRepository
	err  error
}

// readStoredRelated reads the stored rows of the related repositories in one
// statement. An empty related set sends no statement. A nil reader and a
// failed read both fail the whole read; the caller then serves every
// repository live.
func (l configArtifactLoader) readStoredRelated(ctx context.Context, related []repositoryArtifactSource, stats *ConfigArtifactReadStats) storedRelatedRead {
	if len(related) == 0 {
		return storedRelatedRead{byID: map[string]StoredRepository{}}
	}
	if l.stored.Reader == nil {
		return storedRelatedRead{err: errors.New("config artifact stored read: enabled with no stored reader")}
	}
	ids := make([]string, 0, len(related))
	for _, source := range related {
		ids = append(ids, source.RepoID)
	}
	stats.StoredStatements++
	repos, err := l.stored.Reader.ReadStored(ctx, ids)
	if err != nil {
		return storedRelatedRead{err: err}
	}
	byID := make(map[string]StoredRepository, len(repos))
	for _, repo := range repos {
		byID[repo.RepoID] = repo
	}
	return storedRelatedRead{byID: byID}
}

// storedFallbackReason names the live-read reason of one related repository
// the stored read did not vouch for: its answer reason, or missing when the
// read dropped the id or left the reason empty.
func storedFallbackReason(repo StoredRepository, found bool) string {
	if !found || repo.Reason == "" {
		return StoredReadReasonMissing
	}
	return repo.Reason
}

// filterOrderedSources drops the sources the load cannot serve: ones with no
// id or no name. loadForSources and the stored path share it, so both walks
// see the same order.
func filterOrderedSources(sources []repositoryArtifactSource) []repositoryArtifactSource {
	ordered := make([]repositoryArtifactSource, 0, len(sources))
	for _, source := range sources {
		if source.RepoID == "" || source.RepoName == "" {
			continue
		}
		ordered = append(ordered, source)
	}
	return ordered
}

// ConfigureProductionShadowReads enables the shadow compare on the loader
// every route uses, with the production stored reader and reporter. Call
// once at API start before serving, never on a serving process.
func ConfigureProductionShadowReads(reader StoredReader, reporter func(context.Context, ShadowReport)) {
	productionConfigArtifactLoader.shadow = ShadowConfig{Enabled: true, Reader: reader, Reporter: reporter}
}

// ConfigureProductionStoredReads enables the stored load on the loader every
// route uses, with the production stored reader. Call once at API start
// before serving, never on a serving process.
func ConfigureProductionStoredReads(reader StoredReader) {
	productionConfigArtifactLoader.stored = StoredConfig{Enabled: true, Reader: reader}
}
