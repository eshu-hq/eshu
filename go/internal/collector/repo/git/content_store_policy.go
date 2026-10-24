// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"log/slog"
	"path/filepath"
	"sort"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	gitcontent "github.com/eshu-hq/eshu/go/internal/collector/git/content"
	"github.com/eshu-hq/eshu/go/internal/content/shape"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// applyContentStoreToShapeFiles applies each definition's content-store policy
// to the parsed files before shape.Materialize (#8812), so entity source_cache
// is sliced from the stored body and never from the source. It returns the
// sparse path -> policy map streamFacts needs to apply the same policy to the
// body it re-reads from disk. The map is nil, and no file is looked up, when no
// definition binds a non-store policy, which is every definition today.
//
// The digest on each shape.File was computed on the source bytes and is left
// alone, so an unchanged file keeps its freshness hint under either policy.
// The parser already ran on the source: its structured fields are the intended
// output. What the policy governs is every copy of the raw text: the body, the
// entity source snippets, and the whole-file parsed_file_data.source some
// parsers echo. Structured values a parser extracts (a Terraform variable
// default, for example) are parser output, not a body copy, and stay.
func applyContentStoreToShapeFiles(
	registry parser.Registry,
	repoPath string,
	shapeFiles []shape.File,
	parsedFiles []map[string]any,
) map[string]parser.ContentStore {
	if !registry.HasContentStorePolicy() {
		return nil
	}
	stores := map[string]parser.ContentStore{}
	storedBodies := map[string]string{}
	for i := range shapeFiles {
		store := registry.ContentStoreForPath(shapeFiles[i].Path)
		if store.Policy == parser.ContentStoreStore || store.Policy == "" {
			continue
		}
		stores[shapeFiles[i].Path] = store
		shapeFiles[i].Body = gitcontent.ApplyContentStore(store, shapeFiles[i].Body).Body
		storedBodies[shapeFiles[i].Path] = shapeFiles[i].Body
		for bucket, entities := range shapeFiles[i].EntityBuckets {
			for j := range entities {
				entities[j].Source = ""
			}
			shapeFiles[i].EntityBuckets[bucket] = entities
		}
	}
	if len(stores) == 0 {
		return stores
	}
	for _, parsedFile := range parsedFiles {
		relativePath, err := filepath.Rel(repoPath, snapshotPayloadString(parsedFile, "path"))
		if err != nil {
			continue
		}
		stored, governed := storedBodies[filepath.ToSlash(filepath.Clean(relativePath))]
		if !governed {
			continue
		}
		// Some parsers (yaml, hcl) echo the whole file as parsed_file_data.source.
		// That key is a second copy of the body, so it carries the stored body.
		if _, hasSource := parsedFile["source"]; hasSource {
			parsedFile["source"] = stored
		}
		for _, mapping := range snapshotEntityBuckets {
			items, _ := parsedFile[mapping.bucket].([]map[string]any)
			for _, item := range items {
				delete(item, "source")
			}
		}
	}
	return stores
}

// contentStoreTally counts the policy outcomes of one fact stream so the
// operator signal is one counter add per (policy, outcome) and one log line per
// collection, not one per file.
type contentStoreTally struct {
	counts map[contentStoreKey]int64
	// replaced counts the credential values a redact policy replaced.
	replaced int64
}

type contentStoreKey struct {
	policy  parser.ContentStorePolicy
	outcome gitcontent.StoreOutcome
}

// storedBody applies the path's policy to a source body read at emit time and
// tallies the outcome. A path with no non-store policy returns body unchanged.
func (t *contentStoreTally) storedBody(stores map[string]parser.ContentStore, path, body string) string {
	store, governed := stores[path]
	if !governed {
		return body
	}
	result := gitcontent.ApplyContentStore(store, body)
	if t.counts == nil {
		t.counts = map[contentStoreKey]int64{}
	}
	t.counts[contentStoreKey{policy: store.Policy, outcome: result.Outcome}]++
	t.replaced += int64(result.Replaced)
	return result.Body
}

// storedBytes is storedBody for a body read from disk. It returns body itself,
// with no copy, for a path that stores verbatim.
func (t *contentStoreTally) storedBytes(stores map[string]parser.ContentStore, path string, body []byte) []byte {
	if _, governed := stores[path]; !governed {
		return body
	}
	return []byte(t.storedBody(stores, path, string(body)))
}

// recordContentStoreTally reports one stream's policy outcomes through the
// GitSource the context carries. A context without a recorder is a no-op; the
// stream applies the policy either way.
func recordContentStoreTally(ctx context.Context, tally *contentStoreTally, repositoryID, scopeID, generationID string) {
	if len(tally.counts) == 0 {
		return
	}
	if s, _ := ctx.Value(streamDropRecorderKey{}).(*GitSource); s != nil {
		s.recordContentStore(ctx, tally, repositoryID, scopeID, generationID)
	}
}

// recordContentStore adds eshu_dp_collector_content_store_files_total once per
// (policy, outcome) and logs the same counts, so an operator can see which
// files fell back to a metadata row (redactor_failed) without a debugger.
func (s *GitSource) recordContentStore(ctx context.Context, tally *contentStoreTally, repositoryID, scopeID, generationID string) {
	keys := make([]contentStoreKey, 0, len(tally.counts))
	for key := range tally.counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].policy != keys[j].policy {
			return keys[i].policy < keys[j].policy
		}
		return keys[i].outcome < keys[j].outcome
	})
	attrs := []any{
		slog.String("repository_id", repositoryID),
		slog.String(telemetry.LogKeyScopeID, scopeID),
		slog.String(telemetry.LogKeyGenerationID, generationID),
		slog.Int64("values_replaced", tally.replaced),
	}
	var failed int64
	for _, key := range keys {
		count := tally.counts[key]
		if s.Instruments != nil && s.Instruments.ContentStorePolicyFiles != nil {
			s.Instruments.ContentStorePolicyFiles.Add(ctx, count, metric.WithAttributes(
				attribute.String("policy", string(key.policy)),
				attribute.String("outcome", string(key.outcome)),
			))
		}
		attrs = append(attrs, slog.Int64(string(key.policy)+"_"+string(key.outcome), count))
		if key.outcome == gitcontent.StoreOutcomeRedactorFailed {
			failed += count
		}
	}
	if s.Logger == nil {
		return
	}
	if failed > 0 {
		s.Logger.WarnContext(ctx, "git content store policy fell back to metadata rows", attrs...)
		return
	}
	s.Logger.InfoContext(ctx, "git content store policy applied", attrs...)
}
