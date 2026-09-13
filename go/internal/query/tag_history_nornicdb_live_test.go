// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/taghistory"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const tagHistoryKeysetLiveEnv = "ESHU_TAG_HISTORY_KEYSET_NORNICDB_LIVE"

// liveTagHistoryTimestamped is how many real-timestamp observations the live
// seed carries. It is deliberately above 2*taghistory.MaxLimit so a keyset walk
// at the maximum page size takes three or more pages, which is what makes the
// continuation contract observable rather than asserted on one page.
const liveTagHistoryTimestamped = 450

// TestTagHistoryKeysetNornicDBLive proves the three keyset statements against an
// isolated pinned NornicDB: FirstPageCypher, AfterKeyCypher and NullTailCypher
// (#6564 re-review finding 1).
//
// What it establishes that a double cannot:
//
//   - The three timestamp states the store really holds sort where the design
//     assumes: "" first, real millisecond strings ascending, and rows with NO
//     first_observed_at property LAST. page.go's key handling and the nt flag on
//     Cursor both depend on that null position, and nothing else cites a live
//     check of it.
//   - AfterKeyCypher's `OR t.first_observed_at IS NULL` disjunct does not
//     collapse the predicate. The empty-string-guarded OR form the pitfalls page
//     records DOES collapse on this backend, which is why the statements are
//     built by case in Go, and a reviewer needs to see that the disjunct that
//     survived is the safe one.
//   - Two rows sharing one millisecond are separated by the uid tiebreak, so a
//     page boundary landing between them neither duplicates nor skips.
//   - The whole route, not just the statements: RefillScopedPage against real
//     ContainerImage-[:BUILT_FROM]->Repository edges returns exactly the
//     granted rows across three or more pages.
//
// Run against an isolated NornicDB:
//
//	ESHU_TAG_HISTORY_KEYSET_NORNICDB_LIVE=1 \
//	ESHU_NEO4J_URI=bolt://127.0.0.1:17965 \
//	go test ./internal/query -run TestTagHistoryKeysetNornicDBLive -count=1 -v
func TestTagHistoryKeysetNornicDBLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv(tagHistoryKeysetLiveEnv)) == "" {
		t.Skip("set " + tagHistoryKeysetLiveEnv + "=1 to run the live NornicDB keyset proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open NornicDB driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()

	live := &liveTagHistoryGraph{t: t, ctx: ctx, driver: driver, reader: NewNeo4jReader(driver, "nornic")}
	seed := live.seed()
	defer live.cleanup(seed)

	t.Run("statements order the three timestamp states", func(t *testing.T) {
		got := live.uids(live.read(nil, len(seed.order)))
		if !equalStrings(got, seed.order) {
			t.Fatalf("live order mismatch\n got %v\nwant %v", firstN(got, 12), firstN(seed.order, 12))
		}
	})

	t.Run("the IS NULL disjunct does not collapse the predicate", func(t *testing.T) {
		// After the LAST timestamped key, AfterKeyCypher must return exactly
		// the null tail. A collapsed predicate returns zero rows here, and a
		// missing disjunct would too -- so this one assertion is sensitive to
		// both failure modes at once.
		after := seed.lastTimestampedKey
		got := live.uids(live.read(&after, taghistory.MaxLimit))
		if !equalStrings(got, seed.nullUIDs) {
			t.Fatalf("after the last timestamped key: got %v, want the null tail %v", got, seed.nullUIDs)
		}
	})

	t.Run("the null tail pages by uid alone", func(t *testing.T) {
		after := taghistory.Key{NullAt: true, UID: seed.nullUIDs[0]}
		got := live.uids(live.read(&after, taghistory.MaxLimit))
		if want := seed.nullUIDs[1:]; !equalStrings(got, want) {
			t.Fatalf("null-tail page after %q: got %v, want %v", after.UID, got, want)
		}
	})

	t.Run("a shared millisecond is separated by the uid tiebreak", func(t *testing.T) {
		// Resume at the FIRST of the two rows that share a millisecond. The
		// second must come back, and the first must not.
		after := seed.sharedMillisecondKey
		window := live.read(&after, 2)
		got := live.uids(window)
		if len(got) == 0 || got[0] != seed.sharedMillisecondSibling {
			t.Fatalf("after the shared-millisecond key: got %v, want %q first", got, seed.sharedMillisecondSibling)
		}
		for _, uid := range got {
			if uid == after.UID {
				t.Fatalf("the anchor row %q came back; the uid tiebreak did not apply: %v", after.UID, got)
			}
		}
	})

	t.Run("a keyset walk reaches every row exactly once across pages", func(t *testing.T) {
		var walked []string
		var after *taghistory.Key
		for page := 1; page <= 8; page++ {
			window, more, err := taghistory.ReadWindow(ctx, live.reader, seed.imageRef, after, taghistory.MaxLimit)
			if err != nil {
				t.Fatalf("page %d: %v", page, err)
			}
			if len(window) == 0 {
				break
			}
			walked = append(walked, live.uids(window)...)
			key := window[len(window)-1].Key
			after = &key
			if !more {
				break
			}
		}
		if !equalStrings(walked, seed.order) {
			t.Fatalf("keyset walk mismatch: %d rows walked, want %d", len(walked), len(seed.order))
		}
	})

	t.Run("RefillScopedPage returns exactly the granted rows", func(t *testing.T) {
		access := querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{seed.grantedRepositoryID}}
		var kept []string
		var after *taghistory.Key
		pages := 0
		for page := 1; page <= 12; page++ {
			scoped, err := taghistory.RefillScopedPage(ctx, live.reader, seed.imageRef, after, 100, access)
			if err != nil {
				t.Fatalf("page %d: %v", page, err)
			}
			pages++
			for _, row := range scoped.Rows {
				kept = append(kept, row.ResolvedDigest)
			}
			if !scoped.Truncated {
				break
			}
			if scoped.NextKey == nil {
				t.Fatalf("page %d is truncated with no continuation key", page)
			}
			after = scoped.NextKey
		}
		if pages < 3 {
			t.Fatalf("pages = %d, want at least 3 so the continuation contract is exercised", pages)
		}
		if !equalStrings(kept, seed.grantedDigests) {
			t.Fatalf("scoped rows = %d, want %d granted rows", len(kept), len(seed.grantedDigests))
		}
		t.Logf("scoped keyset paging: %d granted rows over %d pages of a %d-row history",
			len(kept), pages, len(seed.order))
	})
}

// liveTagHistoryGraph drives the seed, the reads and the cleanup for the live
// proof over one Bolt driver.
type liveTagHistoryGraph struct {
	t      *testing.T
	ctx    context.Context
	driver neo4jdriver.DriverWithContext
	reader GraphQuery
}

// liveTagHistorySeed is what the live graph was seeded with, in the order the
// statements must return it.
type liveTagHistorySeed struct {
	imageRef                 string
	grantedRepositoryID      string
	order                    []string
	nullUIDs                 []string
	grantedDigests           []string
	lastTimestampedKey       taghistory.Key
	sharedMillisecondKey     taghistory.Key
	sharedMillisecondSibling string
}

func (g *liveTagHistoryGraph) write(cypher string, params map[string]any) {
	g.t.Helper()
	session := g.driver.NewSession(g.ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: "nornic",
	})
	defer func() { _ = session.Close(g.ctx) }()
	result, err := session.Run(g.ctx, cypher, params)
	if err != nil {
		g.t.Fatalf("live NornicDB write: %v", err)
	}
	if _, err := result.Consume(g.ctx); err != nil {
		g.t.Fatalf("consume live NornicDB write: %v", err)
	}
}

func (g *liveTagHistoryGraph) read(after *taghistory.Key, limit int) []taghistory.WindowRow {
	g.t.Helper()
	window, _, err := taghistory.ReadWindow(g.ctx, g.reader, liveTagHistoryImageRef, after, limit)
	if err != nil {
		g.t.Fatalf("live ReadWindow(after=%#v): %v", after, err)
	}
	return window
}

func (g *liveTagHistoryGraph) uids(window []taghistory.WindowRow) []string {
	uids := make([]string, 0, len(window))
	for _, entry := range window {
		uids = append(uids, entry.Key.UID)
	}
	return uids
}

// liveTagHistoryImageRef is fixed rather than per-run so the isolation check
// below is meaningful: the seed refuses to run when any observation already
// exists on it, so a stale run's rows can never be read as this run's answer.
const liveTagHistoryImageRef = "live.invalid/eshu-6564-keyset:1.0.0"

// seed writes the three timestamp states, a shared-millisecond pair, and the
// BUILT_FROM edges a scoped page binds through. It returns the expected total
// order so every assertion above compares against the seed, not against
// whatever the backend happened to return.
func (g *liveTagHistoryGraph) seed() liveTagHistorySeed {
	g.t.Helper()

	const (
		grantedRepositoryID = "repository:live-6564-granted"
		otherRepositoryID   = "repository:live-6564-other"
	)
	countRow, err := g.reader.RunSingle(g.ctx,
		`MATCH (t:ContainerImageTagObservation {image_ref: $image_ref}) RETURN count(t) AS count`,
		map[string]any{"image_ref": liveTagHistoryImageRef})
	if err != nil {
		g.t.Fatalf("count existing observations: %v", err)
	}
	if got := IntVal(countRow, "count"); got != 0 {
		g.t.Fatalf("live proof requires an isolated graph with zero observations on %s, got %d",
			liveTagHistoryImageRef, got)
	}

	seed := liveTagHistorySeed{imageRef: liveTagHistoryImageRef, grantedRepositoryID: grantedRepositoryID}
	type observation struct {
		uid       string
		digest    string
		at        string
		hasAt     bool
		granted   bool
		sharedPos bool
	}
	var rows []observation

	// (1) Two rows with a stored EMPTY first_observed_at. ociTagObservedAtValue
	//     writes "" for a zero ObservedAt, and "" sorts FIRST as a string.
	for i := range 2 {
		rows = append(rows, observation{
			uid:     fmt.Sprintf("live-6564-empty-%02d", i),
			digest:  fmt.Sprintf("sha256:live6564empty%02d", i),
			at:      "",
			hasAt:   true,
			granted: i == 0,
		})
	}
	// (2) The timestamped body, fixed-width millisecond strings.
	for i := range liveTagHistoryTimestamped {
		rows = append(rows, observation{
			uid:     fmt.Sprintf("live-6564-ts-%05d", i),
			digest:  fmt.Sprintf("sha256:live6564ts%05d", i),
			at:      fmt.Sprintf("17600000%05d", i),
			hasAt:   true,
			granted: i%2 == 0,
		})
	}
	// (3) Two rows sharing ONE millisecond, both after the body, so a page
	//     boundary can land between them.
	sharedAt := fmt.Sprintf("17600000%05d", liveTagHistoryTimestamped)
	for i := range 2 {
		rows = append(rows, observation{
			uid:       fmt.Sprintf("live-6564-shared-%02d", i),
			digest:    fmt.Sprintf("sha256:live6564shared%02d", i),
			at:        sharedAt,
			hasAt:     true,
			granted:   true,
			sharedPos: true,
		})
	}
	// (4) Pre-#5459 rows: no first_observed_at property at all. They sort LAST.
	for i := range 3 {
		rows = append(rows, observation{
			uid:     fmt.Sprintf("live-6564-null-%02d", i),
			digest:  fmt.Sprintf("sha256:live6564null%02d", i),
			granted: i == 0,
		})
	}

	withAt := make([]map[string]any, 0, len(rows))
	withoutAt := make([]map[string]any, 0, 3)
	images := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		seed.order = append(seed.order, row.uid)
		if row.granted {
			seed.grantedDigests = append(seed.grantedDigests, row.digest)
		}
		if !row.hasAt {
			seed.nullUIDs = append(seed.nullUIDs, row.uid)
		}
		if row.sharedPos && seed.sharedMillisecondKey.UID == "" {
			seed.sharedMillisecondKey = taghistory.Key{At: row.at, UID: row.uid}
		} else if row.sharedPos {
			seed.sharedMillisecondSibling = row.uid
		}
		if row.hasAt {
			seed.lastTimestampedKey = taghistory.Key{At: row.at, UID: row.uid}
		}
		entry := map[string]any{"uid": row.uid, "digest": row.digest, "at": row.at}
		if row.hasAt {
			withAt = append(withAt, entry)
		} else {
			withoutAt = append(withoutAt, entry)
		}
		repositoryID := otherRepositoryID
		if row.granted {
			repositoryID = grantedRepositoryID
		}
		images = append(images, map[string]any{"digest": row.digest, "repository_id": repositoryID})
	}

	g.write(`CREATE (r:Repository {id: $granted, name: $granted})`, map[string]any{"granted": grantedRepositoryID})
	g.write(`CREATE (r:Repository {id: $other, name: $other})`, map[string]any{"other": otherRepositoryID})
	for _, batch := range chunkLiveRows(withAt, 150) {
		g.write(`
UNWIND $rows AS row
CREATE (t:ContainerImageTagObservation)
SET t.uid = row.uid,
    t.image_ref = $image_ref,
    t.tag = $tag,
    t.resolved_digest = row.digest,
    t.previous_digest = $blank,
    t.mutated = false,
    t.repository_id = $repository_id,
    t.identity_strength = $strength,
    t.first_observed_at = row.at`, g.seedParams(batch))
	}
	// Written by a SEPARATE statement that never names first_observed_at, so the
	// property is genuinely absent rather than set to null.
	g.write(`
UNWIND $rows AS row
CREATE (t:ContainerImageTagObservation)
SET t.uid = row.uid,
    t.image_ref = $image_ref,
    t.tag = $tag,
    t.resolved_digest = row.digest,
    t.previous_digest = $blank,
    t.mutated = false,
    t.repository_id = $repository_id,
    t.identity_strength = $strength`, g.seedParams(withoutAt))

	for _, batch := range chunkLiveRows(images, 150) {
		g.write(`
UNWIND $rows AS row
CREATE (i:ContainerImage {digest: row.digest, uid: row.digest, id: row.digest, pending_repository_id: row.repository_id})`,
			map[string]any{"rows": batch})
	}
	// One edge per image, created after both endpoints exist. Verified below
	// rather than assumed: an UNWIND-batched bare-MATCH write is a shape this
	// backend has silently dropped before
	// (docs/public/reference/nornicdb-pitfalls.md).
	for _, repositoryID := range []string{grantedRepositoryID, otherRepositoryID} {
		digests := make([]string, 0, len(images))
		for _, image := range images {
			if image["repository_id"] == repositoryID {
				digests = append(digests, image["digest"].(string))
			}
		}
		for _, batch := range chunkLiveStrings(digests, 150) {
			g.write(`
UNWIND $digests AS digest
MATCH (i:ContainerImage {digest: digest})
MATCH (repo:Repository {id: $repository_id})
CREATE (i)-[:BUILT_FROM {scope_id: $repository_id, evidence_source: 'live_proof'}]->(repo)`,
				map[string]any{"digests": batch, "repository_id": repositoryID})
		}
	}

	edgeRow, err := g.reader.RunSingle(g.ctx,
		`MATCH (i:ContainerImage)-[b:BUILT_FROM]->(repo:Repository) WHERE repo.id = $repository_id RETURN count(b) AS count`,
		map[string]any{"repository_id": grantedRepositoryID})
	if err != nil {
		g.t.Fatalf("count granted BUILT_FROM edges: %v", err)
	}
	if got, want := IntVal(edgeRow, "count"), len(seed.grantedDigests); got != want {
		g.t.Fatalf("granted BUILT_FROM edges = %d, want %d: the seed write did not land, so any scoped result below would be vacuous", got, want)
	}
	return seed
}

// seedParams binds every literal the observation writer needs as a PARAMETER.
// An empty-string literal, or an oci-registry:// one, is not safe in the
// statement TEXT here: the pinned build's parser treats the double slash inside
// a string literal as a comment start and fails the whole statement with
// "unclosed quote" (measured while writing this proof).
func (g *liveTagHistoryGraph) seedParams(rows []map[string]any) map[string]any {
	return map[string]any{
		"rows":          rows,
		"image_ref":     liveTagHistoryImageRef,
		"tag":           "1.0.0",
		"blank":         "",
		"repository_id": "oci-registry://live.invalid/eshu-6564-keyset",
		"strength":      "digest",
	}
}

func (g *liveTagHistoryGraph) cleanup(seed liveTagHistorySeed) {
	for _, batch := range chunkLiveStrings(seed.order, 150) {
		g.write(`UNWIND $uids AS uid MATCH (t:ContainerImageTagObservation {uid: uid}) DETACH DELETE t`,
			map[string]any{"uids": batch})
	}
	digests := make([]string, 0, len(seed.order))
	rows, err := g.reader.Run(g.ctx,
		`MATCH (i:ContainerImage) WHERE i.digest STARTS WITH $prefix RETURN i.digest AS digest`,
		map[string]any{"prefix": "sha256:live6564"})
	if err == nil {
		for _, row := range rows {
			digests = append(digests, StringVal(row, "digest"))
		}
	}
	for _, batch := range chunkLiveStrings(digests, 150) {
		g.write(`UNWIND $digests AS digest MATCH (i:ContainerImage {digest: digest}) DETACH DELETE i`,
			map[string]any{"digests": batch})
	}
	g.write(`MATCH (r:Repository) WHERE r.id STARTS WITH $prefix DETACH DELETE r`,
		map[string]any{"prefix": "repository:live-6564-"})
}

func chunkLiveRows(rows []map[string]any, size int) [][]map[string]any {
	var out [][]map[string]any
	for start := 0; start < len(rows); start += size {
		out = append(out, rows[start:min(start+size, len(rows))])
	}
	return out
}

func chunkLiveStrings(values []string, size int) [][]string {
	var out [][]string
	for start := 0; start < len(values); start += size {
		out = append(out, values[start:min(start+size, len(values))])
	}
	return out
}

func firstN(values []string, n int) []string {
	if len(values) <= n {
		return values
	}
	return values[:n]
}
