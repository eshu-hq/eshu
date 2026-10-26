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
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/content/shape"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// applyContentStoreToShapeFiles applies each definition's content-store policy
// to the parsed files before shape.Materialize (#8812), so entity source_cache
// is sliced from the stored body and never from the source. It returns the
// sparse path -> policy map streamFacts needs to apply the same policy to the
// body it re-reads from disk. The map is nil, and no file is looked up, when no
// definition binds a non-store policy. The default registry binds .npmrc to
// metadata (#8978), so a default-registry repository pays one registry lookup
// per file and gets an empty map unless it holds an .npmrc.
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

// addContentStoresForMetas adds the policy for every path a body-reading
// emitter in streamFacts will re-read, so a definition that binds a name the
// parser never sees (CODEOWNERS and .gitmodules are diverted before parsing, and
// documentation and workflow-image files are re-read separately) is still
// enforced. It is a no-op for a registry that binds no non-store policy.
func addContentStoresForMetas(registry parser.Registry, snapshot *RepositorySnapshot) {
	if !registry.HasContentStorePolicy() {
		return
	}
	for _, metas := range [][]model.ContentFileMeta{
		snapshot.ContentFileMetas, snapshot.DocumentationFileMetas, snapshot.WorkflowImageFileMetas,
	} {
		for _, meta := range metas {
			store := registry.ContentStoreForPath(meta.RelativePath)
			if store.Policy == parser.ContentStoreStore || store.Policy == "" {
				continue
			}
			if snapshot.ContentStores == nil {
				snapshot.ContentStores = map[string]parser.ContentStore{}
			}
			snapshot.ContentStores[meta.RelativePath] = store
		}
	}
}

// contentStoreTally counts the policy outcomes of one fact stream so the
// operator signal is one counter add per (policy, redactor, outcome) and one log line per
// collection, not one per file.
type contentStoreTally struct {
	counts map[contentStoreKey]int64
	// lookup re-checks the snapshot registry when the ContentStores map misses
	// a path. It is nil when the snapshot carries no registry, and the map is
	// then the whole contract.
	lookup func(path string) (parser.ContentStore, bool)
	// replaced counts the credential values a redact policy replaced.
	replaced int64
	// failedPaths holds the first maxLoggedFailedPaths repo-relative paths whose
	// redactor failed, and failedReasons counts every failure by bounded reason
	// class, so the WARN log says which file and why without error text.
	failedPaths   []string
	failedReasons map[gitcontent.FailureReason]int64
}

// maxLoggedFailedPaths bounds the failed paths one log line carries.
const maxLoggedFailedPaths = 10

type contentStoreKey struct {
	policy parser.ContentStorePolicy
	// redactor is the redact policy's mode (marker or skeleton), or
	// redactorNone for a policy without one, so an operator can tell which
	// redactor failed.
	redactor parser.RedactorMode
	outcome  gitcontent.StoreOutcome
}

// redactorNone is the redactor label of a policy that names no redactor mode.
const redactorNone parser.RedactorMode = "none"

func redactorLabel(store parser.ContentStore) parser.RedactorMode {
	if store.Redactor == "" {
		return redactorNone
	}
	return store.Redactor
}

// logKey is the per-key count field of the stream log: policy_outcome for a
// policy without a redactor, policy_redactor_outcome otherwise.
func (k contentStoreKey) logKey() string {
	if k.redactor == redactorNone {
		return string(k.policy) + "_" + string(k.outcome)
	}
	return string(k.policy) + "_" + string(k.redactor) + "_" + string(k.outcome)
}

// contentStoreFallback returns the emit-time re-check for a snapshot built
// with registry: the definition's non-store policy for path, or ungoverned
// when the definition stores verbatim. It returns nil for a registry that
// binds no non-store policy, including the zero registry a hand-built
// snapshot carries when it names none.
func contentStoreFallback(registry parser.Registry) func(string) (parser.ContentStore, bool) {
	if !registry.HasContentStorePolicy() {
		return nil
	}
	return func(path string) (parser.ContentStore, bool) {
		store := registry.ContentStoreForPath(path)
		if store.Policy == parser.ContentStoreStore || store.Policy == "" {
			return parser.ContentStore{}, false
		}
		return store, true
	}
}

// resolve returns the path's non-store content-store policy: the snapshot map
// entry, or the registry fallback when the map misses a governed path, so the
// stream applies the definition's policy instead of emitting the raw body.
func (t *contentStoreTally) resolve(stores map[string]parser.ContentStore, path string) (parser.ContentStore, bool) {
	if store, governed := stores[path]; governed {
		return store, true
	}
	if t.lookup != nil {
		return t.lookup(path)
	}
	return parser.ContentStore{}, false
}

// storedBody applies the path's policy to a source body read at emit time and
// tallies the outcome. A path with no non-store policy returns body unchanged.
func (t *contentStoreTally) storedBody(stores map[string]parser.ContentStore, path, body string) string {
	// An ungoverned registry passes a nil map and no fallback; returning
	// before the lookup skips copying the zero ContentStore on every file.
	if len(stores) == 0 && t.lookup == nil {
		return body
	}
	store, governed := t.resolve(stores, path)
	if !governed {
		return body
	}
	result := gitcontent.ApplyContentStore(store, body)
	if t.counts == nil {
		t.counts = map[contentStoreKey]int64{}
	}
	t.counts[contentStoreKey{policy: store.Policy, redactor: redactorLabel(store), outcome: result.Outcome}]++
	t.replaced += int64(result.Replaced)
	if result.Outcome == gitcontent.StoreOutcomeRedactorFailed {
		if len(t.failedPaths) < maxLoggedFailedPaths {
			t.failedPaths = append(t.failedPaths, path)
		}
		if t.failedReasons == nil {
			t.failedReasons = map[gitcontent.FailureReason]int64{}
		}
		t.failedReasons[result.Reason]++
	}
	return result.Body
}

// storedBytes is storedBody for a body read from disk. It returns body itself,
// with no copy, for a path that stores verbatim, and copies nothing for a
// metadata path.
func (t *contentStoreTally) storedBytes(stores map[string]parser.ContentStore, path string, body []byte) []byte {
	// An ungoverned registry passes a nil map and no fallback; returning
	// before the lookup skips copying the zero ContentStore on every file.
	if len(stores) == 0 && t.lookup == nil {
		return body
	}
	store, governed := t.resolve(stores, path)
	if !governed {
		return body
	}
	if store.Policy == parser.ContentStoreMetadata {
		// The policy discards the body and ApplyContentStore ignores it for
		// metadata (it never fails, whatever the bytes), so skip the string
		// conversion of a body that may be megabytes. The tally is the same
		// metadata/metadata add, and the result is a non-nil empty slice because
		// docs.ReadDocumentationBodyWithError treats a nil body as "not supplied".
		t.storedBody(stores, path, "")
		return []byte{}
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
// (policy, redactor, outcome) and logs the same counts, so an operator can see which
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
		if keys[i].redactor != keys[j].redactor {
			return keys[i].redactor < keys[j].redactor
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
				attribute.String("redactor", string(key.redactor)),
				attribute.String("outcome", string(key.outcome)),
			))
		}
		attrs = append(attrs, slog.Int64(key.logKey(), count))
		if key.outcome == gitcontent.StoreOutcomeRedactorFailed {
			failed += count
		}
	}
	if s.Logger == nil {
		return
	}
	if failed > 0 {
		reasons := make([]string, 0, len(tally.failedReasons))
		for reason := range tally.failedReasons {
			reasons = append(reasons, string(reason))
		}
		sort.Strings(reasons)
		for _, reason := range reasons {
			attrs = append(attrs, slog.Int64("failed_reason_"+reason, tally.failedReasons[gitcontent.FailureReason(reason)]))
		}
		attrs = append(attrs, slog.Any("failed_paths", tally.failedPaths))
		s.Logger.WarnContext(ctx, "git content store policy fell back to metadata rows", attrs...)
		return
	}
	s.Logger.InfoContext(ctx, "git content store policy applied", attrs...)
}
