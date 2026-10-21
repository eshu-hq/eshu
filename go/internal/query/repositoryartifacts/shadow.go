// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"errors"
	"fmt"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
)

// StoredRepository is one related repository's stored answer for the shadow
// compare and the stored read: its trusted rows, or Trusted false when the
// stored read has nothing to serve or compare for it. The production API
// wiring adapts the storage read-model port to this shape; tests use a fake.
type StoredRepository struct {
	RepoID string
	// Trusted reports whether Rows are exact for the current content and
	// extractor. Only a trusted repository is compared; any other one
	// counts stored_missing, never a mismatch.
	Trusted bool
	// Reason is why the repository is served live when Trusted is false:
	// one of the storage read-model reasons (missing, stale_version,
	// stale_extractor, failed) or empty when the read dropped the id. The
	// stored path counts it on eshu_dp_config_artifact_reads_total; the
	// shadow compare ignores it.
	Reason string
	// ListTruncated carries the trusted state row's list_truncated: the
	// stored 5,000-window reason input. It is meaningful only when Trusted
	// is true; an untrusted repository's live fallback load owns the window
	// reason, so its bit stays false here.
	ListTruncated bool
	Rows          []contentartifacts.StoredRow
}

// StoredReader is the leaf's port to the stored config-artifact read model:
// one call answers the related repository ids the caller passes. The caller
// passes only ids the access filter kept (#5167 W3 P0), in walk order, and
// never the anchor repository, which stays live.
type StoredReader interface {
	ReadStored(ctx context.Context, repoIDs []string) ([]StoredRepository, error)
}

// ShadowRepositoryResult is one related repository's shadow compare outcome.
type ShadowRepositoryResult struct {
	RepoID string
	// Outcome is one of contentartifacts.ShadowOutcomes().
	Outcome string
	// LiveRows and StoredRows are the compared row counts. They are zero
	// for stored_missing, where there was nothing to compare.
	LiveRows   int
	StoredRows int
}

// ShadowReport is one shadowed request's compare outcome: one result per
// related repository in walk order, or a read error with no results. A report
// with a read error still served the live answer.
type ShadowReport struct {
	Repositories []ShadowRepositoryResult
	ReadErr      error
}

// Count returns the number of results with the given outcome.
func (r ShadowReport) Count(outcome string) int {
	count := 0
	for _, result := range r.Repositories {
		if result.Outcome == outcome {
			count++
		}
	}
	return count
}

// ShadowConfig carries the shadow compare of the deployment-artifact load
// (#8155 PR3c). The zero value disables it, so every existing caller keeps
// today's read with no stored statement.
type ShadowConfig struct {
	// Enabled runs the shadow compare after the live load. Disabled keeps
	// the live answer untouched and issues no stored statement.
	Enabled bool
	// Reader answers the stored rows of the related repositories. It must
	// be set when Enabled is set; an enabled shadow with no reader reports
	// a read error instead of failing the request.
	Reader StoredReader
	// Reporter receives one report per shadowed request. Nil disables
	// the compare: with no receiver the stored read is skipped, so a
	// misconfigured shadow costs no statement. It runs inline on the
	// request path, so it must stay cheap (a counter and a log) and must
	// not fail: the wiring slice owns it. It takes the request context so
	// the production reporter emits under the request span.
	Reporter func(context.Context, ShadowReport)
}

// maybeShadow runs the shadow compare when the loader's shadow config
// enables it, and reports it. It never fails the request: a stored-read
// failure lands in the report while the caller keeps serving the live
// answer it already computed. It never runs on a stored load: the stored
// answer has no live rows to compare against.
func (l configArtifactLoader) maybeShadow(ctx context.Context, res configArtifactLoadResult, stats *ConfigArtifactReadStats) {
	if !l.shadow.Enabled || l.shadow.Reporter == nil || l.stored.Enabled {
		return
	}
	report := ShadowReport{}
	if l.shadow.Reader == nil {
		report.ReadErr = errors.New("config artifact shadow: enabled with no stored reader")
	} else {
		stats.StoredStatements++
		report = compareShadow(ctx, l.shadow.Reader, res)
	}
	l.shadow.Reporter(ctx, report)
}

// compareShadow reads the stored rows of the related repositories and
// compares each against its live rows. The anchor (sources[0]) stays out of
// the comparison: it is always served live. A repository the stored read
// cannot vouch for counts stored_missing.
func compareShadow(ctx context.Context, reader StoredReader, res configArtifactLoadResult) ShadowReport {
	report := ShadowReport{}
	if len(res.sources) < 2 {
		return report
	}
	related := res.sources[1:]
	ids := make([]string, 0, len(related))
	for _, source := range related {
		ids = append(ids, source.RepoID)
	}
	stored, err := reader.ReadStored(ctx, ids)
	if err != nil {
		report.ReadErr = err
		return report
	}
	byID := make(map[string]StoredRepository, len(stored))
	for _, repo := range stored {
		byID[repo.RepoID] = repo
	}
	for i, source := range related {
		live := namedLiveRows(res.rows[i+1])
		result := ShadowRepositoryResult{RepoID: source.RepoID, LiveRows: len(live)}
		repo, ok := byID[source.RepoID]
		if !ok || !repo.Trusted {
			result.Outcome = contentartifacts.ShadowOutcomeStoredMissing
			report.Repositories = append(report.Repositories, result)
			continue
		}
		named := contentartifacts.ApplyStoredNameStep(source.RepoName, repo.Rows)
		result.StoredRows = len(named)
		if contentartifacts.EqualNamedRows(live, named) {
			result.Outcome = contentartifacts.ShadowOutcomeMatch
		} else {
			result.Outcome = contentartifacts.ShadowOutcomeMismatch
		}
		report.Repositories = append(report.Repositories, result)
	}
	return report
}

// namedLiveRows reads one repository's live rows into the named-row identity
// the shadow compare uses, keeping each row's own source_repo label. A
// missing or nil column is empty and a non-string value formats with %v, the
// same reading the neutral package's row order and dedup use.
func namedLiveRows(rows []map[string]any) []contentartifacts.NamedRow {
	out := make([]contentartifacts.NamedRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, contentartifacts.NamedRow{
			Path:         shadowRowString(row, "path"),
			SourceRepo:   shadowRowString(row, "source_repo"),
			RelativePath: shadowRowString(row, "relative_path"),
			EvidenceKind: shadowRowString(row, "evidence_kind"),
		})
	}
	return out
}

// shadowRowString reads a string column of a built row.
func shadowRowString(row map[string]any, key string) string {
	switch value := row[key].(type) {
	case nil:
		return ""
	case string:
		return value
	default:
		return fmt.Sprintf("%v", value)
	}
}
