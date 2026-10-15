// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// listRepositoryCatalogQuery loads, newest first, one projected object per
// repository fact for the catalog loader below. The object carries only the
// keys relationships.RepositoryCatalogPayloadKeys declares: reading six keys
// from 28,000 payloads of 158 KB on average used to ship 4.4 GB and decode it
// in Go (#8082). jsonb_to_record detoasts each payload once; six
// payload->'key' calls would detoast it six times. Typed jsonb columns keep a
// non-string value a non-string, and an absent key becomes JSON null, which
// the Go reader skips exactly as it skips an absent key. The object guard
// drops the rows the old whole-payload decode into a map skipped, because
// jsonb_to_record raises on a non-object.
var listRepositoryCatalogQuery = renderRepositoryCatalogQuery(relationships.RepositoryCatalogPayloadKeys())

// renderRepositoryCatalogQuery renders the projecting catalog statement for
// keys. Every key becomes a jsonb_build_object argument and a jsonb_to_record
// column, so the statement cannot name a key the list lacks. Keys are spliced
// into SQL text, double-quoted so a reserved word such as "order" stays valid,
// and a key that is not a lowercase identifier panics: the list is a
// compile-time constant of the relationships package, never input.
func renderRepositoryCatalogQuery(keys []string) string {
	var objectArgs, columns strings.Builder
	for i, key := range keys {
		if !catalogKeyPattern.MatchString(key) {
			panic(fmt.Sprintf("backfill: repository catalog key %q is not a lowercase identifier", key))
		}
		if i > 0 {
			objectArgs.WriteString(", ")
			columns.WriteString(", ")
		}
		quoted := `"` + key + `"`
		objectArgs.WriteString("'" + key + "', x." + quoted)
		columns.WriteString(quoted + " jsonb")
	}
	return "\nSELECT jsonb_build_object(" + objectArgs.String() + ") AS payload, f.observed_at\n" +
		"FROM fact_records AS f,\n" +
		"     LATERAL jsonb_to_record(f.payload) AS x(" + columns.String() + ")\n" +
		"WHERE f.fact_kind = 'repository'\n" +
		"  AND jsonb_typeof(f.payload) = 'object'\n" +
		"ORDER BY f.observed_at DESC, f.fact_id DESC\n"
}

// catalogKeyPattern admits the payload keys that are safe to render as a
// double-quoted SQL identifier and a single-quoted jsonb key.
var catalogKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// catalogLoadStats counts what one LoadRepositoryCatalog call did, so the
// deferred pass's catalog_load block can report rows scanned and decode time.
// The loader returns only the kept entries, so the scanned row count and the
// JSON decode time travel through the context instead of the CatalogLoader
// signature.
type catalogLoadStats struct {
	rows   int64
	decode time.Duration
}

type catalogLoadStatsKey struct{}

// withCatalogLoadStats returns ctx carrying stats for LoadRepositoryCatalog to
// fill. Without it the loader does no extra work.
func withCatalogLoadStats(ctx context.Context, stats *catalogLoadStats) context.Context {
	return context.WithValue(ctx, catalogLoadStatsKey{}, stats)
}

// LoadRepositoryCatalog loads the deduplicated repository identity catalog
// (newest row per repository wins via the query ORDER BY) plus each kept
// entry's observed_at, which the shared cache uses as the freshness key so a
// replayed older generation cannot regress a newer cached identity (#5134
// review).
func LoadRepositoryCatalog(
	ctx context.Context,
	queryer db.Queryer,
) ([]relationships.CatalogEntry, map[string]time.Time, error) {
	if queryer == nil {
		return nil, nil, nil
	}

	rows, err := queryer.QueryContext(ctx, listRepositoryCatalogQuery)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	stats, _ := ctx.Value(catalogLoadStatsKey{}).(*catalogLoadStats)
	seen := make(map[string]struct{})
	catalog := make([]relationships.CatalogEntry, 0)
	observedAt := make(map[string]time.Time)
	for rows.Next() {
		var rawPayload []byte
		var rowObservedAt time.Time
		if err := rows.Scan(&rawPayload, &rowObservedAt); err != nil {
			return nil, nil, err
		}
		var decodeStart time.Time
		if stats != nil {
			stats.rows++
			decodeStart = time.Now()
		}
		entry, ok := repositoryCatalogEntryFromPayload(rawPayload)
		if stats != nil {
			stats.decode += time.Since(decodeStart)
		}
		if !ok {
			continue
		}
		if _, exists := seen[entry.RepoID]; exists {
			continue
		}
		seen[entry.RepoID] = struct{}{}
		catalog = append(catalog, entry)
		observedAt[entry.RepoID] = rowObservedAt
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	return catalog, observedAt, nil
}

func repositoryCatalogEntryFromPayload(rawPayload []byte) (relationships.CatalogEntry, bool) {
	if len(rawPayload) == 0 {
		return relationships.CatalogEntry{}, false
	}

	var payload map[string]any
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return relationships.CatalogEntry{}, false
	}

	return RepositoryCatalogEntryFromMap(payload)
}

// RepositoryCatalogEntryFromMap derives a repository CatalogEntry (RepoID plus
// matching aliases) from a decoded repository fact payload. It delegates to
// relationships.RepositoryCatalogEntry (#4394 T2) so the streaming commit path,
// the JSON catalog loader, and Ifá's derived-catalog seam
// (go/internal/ifa) all compute a generation's committed repository identity
// identically; otherwise alias-drift detection (issue #3521) would compare
// inconsistently shaped aliases.
func RepositoryCatalogEntryFromMap(payload map[string]any) (relationships.CatalogEntry, bool) {
	return relationships.RepositoryCatalogEntry(payload)
}
