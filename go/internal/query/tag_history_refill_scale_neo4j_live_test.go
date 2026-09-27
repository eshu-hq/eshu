// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/taghistory"
)

// tagHistoryRefillScaleLiveEnv gates TestTagHistoryRefillScaleNeo4jLive.
const tagHistoryRefillScaleLiveEnv = "ESHU_TAG_HISTORY_REFILL_SCALE_LIVE"

// tagHistoryRefillScaleObservations is the per-image_ref observation count
// this proof seeds: strictly more than 4*taghistory.MaxLimit (800) so a
// fully-withheld scoped page reads every one of taghistory.MaxRefillReads
// windows and still leaves raw history unread.
const tagHistoryRefillScaleObservations = 850

// tagHistoryRefillScaleWarmTrials is how many repeated calls follow the cold
// one when measuring the fully-withheld path's warm latency distribution.
const tagHistoryRefillScaleWarmTrials = 20

// tagHistoryRefillScaleLimit is the scoped caller's requested page size for
// every RefillScopedPage call this proof makes.
const tagHistoryRefillScaleLimit = 100

// tagHistoryRefillScaleCorpusSizes are the two corpus scales #6705 asks for.
var tagHistoryRefillScaleCorpusSizes = []int{5000, 10000}

// TestTagHistoryRefillScaleNeo4jLive proves taghistory.RefillScopedPage's
// #6705 shared-deadline fix and its MaxRefillReads=4 cap against a real,
// pinned Neo4j at corpus scale -- never NornicDB (owner rule: NornicDB-only
// bugs get a hermetic guard, not a fake RED; corpus-scale timing claims are
// proven on Neo4j). It seeds N background ContainerImage nodes sizing the
// container_image_digest index the way ops-qa's read-only probe found it
// (#6705 claim comment: a 400-key NodeIndexSeek against a real store, 0-1ms),
// plus one image_ref whose tagHistoryRefillScaleObservations digests carry NO
// ContainerImage node at all -- the fully-withheld page that drives the
// refill loop through every one of taghistory.MaxRefillReads windows -- and a
// second image_ref of the same size fully covered by the caller's grant, the
// control proving no granted row is lost at this scale.
//
// It measures the withheld path's full RefillScopedPage call latency cold
// (first call after seed) and warm (tagHistoryRefillScaleWarmTrials more
// calls), and logs one machine-readable result line per corpus size so the
// evidence doc can cite exact numbers rather than an impression.
//
// Run against the isolated container docs/internal/evidence/6705-tag-history-refill-neo4j-scale.md
// records:
//
//	ESHU_TAG_HISTORY_REFILL_SCALE_LIVE=1 \
//	ESHU_NEO4J_URI=bolt://127.0.0.1:17705 \
//	ESHU_NEO4J_USERNAME=neo4j ESHU_NEO4J_PASSWORD=eshu-6705-pass \
//	go test ./internal/query -run TestTagHistoryRefillScaleNeo4jLive -count=1 -v -timeout 20m
func TestTagHistoryRefillScaleNeo4jLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv(tagHistoryRefillScaleLiveEnv)) == "" {
		t.Skip("set " + tagHistoryRefillScaleLiveEnv + "=1 to run the live Neo4j corpus-scale refill proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	auth := neo4jdriver.NoAuth()
	if user := strings.TrimSpace(os.Getenv("ESHU_NEO4J_USERNAME")); user != "" {
		auth = neo4jdriver.BasicAuth(user, os.Getenv("ESHU_NEO4J_PASSWORD"), "")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	if database == "" {
		database = "neo4j"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, auth)
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	schemaExec := refillScaleSchemaExecutor{driver: driver, database: database}
	if err := graph.EnsureSchemaWithBackend(ctx, schemaExec, logger, graph.SchemaBackendNeo4j); err != nil {
		t.Fatalf("apply production schema: %v", err)
	}

	g := &refillScaleGraph{t: t, ctx: ctx, driver: driver, database: database, reader: NewNeo4jReader(driver, database)}

	for _, n := range tagHistoryRefillScaleCorpusSizes {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			seed := g.seed(n)
			defer g.cleanup(seed)
			g.measure(t, n, seed)
		})
	}
}

// refillScaleSchemaExecutor applies Eshu's production schema through a plain
// Bolt driver, exactly the graph.EnsureSchemaWithBackend seam
// label_predicate_timing_live_test.go and canonical_moved_block_live_test.go
// already use, so the index/constraint DDL this proof runs under is the
// SAME DDL production applies rather than a hand-copied approximation.
type refillScaleSchemaExecutor struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (e refillScaleSchemaExecutor) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	_, err := neo4jdriver.ExecuteQuery(ctx, e.driver, stmt.Cypher, stmt.Parameters,
		neo4jdriver.EagerResultTransformer, neo4jdriver.ExecuteQueryWithDatabase(e.database))
	return err
}

// refillScaleGraph drives the seed, the measured reads, and the cleanup for
// one corpus size over one Bolt driver.
type refillScaleGraph struct {
	t        *testing.T
	ctx      context.Context
	driver   neo4jdriver.DriverWithContext
	database string
	reader   *Neo4jReader
}

// refillScaleSeed is what refillScaleGraph.seed wrote for one corpus size.
type refillScaleSeed struct {
	n                   int
	corpusPrefix        string
	imageRefWithheld    string
	imageRefGranted     string
	grantedRepositoryID string
	grantedCount        int
}

func (g *refillScaleGraph) write(cypher string, params map[string]any) {
	g.t.Helper()
	session := g.driver.NewSession(g.ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: g.database,
	})
	defer func() { _ = session.Close(g.ctx) }()
	result, err := session.Run(g.ctx, cypher, params)
	if err != nil {
		g.t.Fatalf("live Neo4j write: %v\ncypher: %.300s", err, cypher)
	}
	if _, err := result.Consume(g.ctx); err != nil {
		g.t.Fatalf("consume live Neo4j write: %v", err)
	}
}

func (g *refillScaleGraph) count(cypher string, params map[string]any) int {
	g.t.Helper()
	row, err := g.reader.RunSingle(g.ctx, cypher, params)
	if err != nil {
		g.t.Fatalf("live Neo4j count: %v\ncypher: %.300s", err, cypher)
	}
	return IntVal(row, "count")
}

// seed writes n background ContainerImage nodes plus the withheld and granted
// observation sets, and verifies every write landed before returning: an
// UNWIND-batched bare-MATCH write is a shape this codebase has silently
// dropped before (docs/public/reference/nornicdb-pitfalls.md), and this proof
// runs on a fresh, disposable Neo4j so that check has never run here.
func (g *refillScaleGraph) seed(n int) refillScaleSeed {
	g.t.Helper()
	corpusPrefix := fmt.Sprintf("sha256:6705-corpus-%d-", n)
	withheldRef := fmt.Sprintf("live.invalid/eshu-6705-scale-%d-withheld:1.0.0", n)
	grantedRef := fmt.Sprintf("live.invalid/eshu-6705-scale-%d-granted:1.0.0", n)
	grantedRepositoryID := fmt.Sprintf("repository:live-6705-granted-%d", n)

	for _, ref := range []string{withheldRef, grantedRef} {
		if got := g.count(
			`MATCH (t:ContainerImageTagObservation {image_ref: $image_ref}) RETURN count(t) AS count`,
			map[string]any{"image_ref": ref}); got != 0 {
			g.t.Fatalf("live proof requires an isolated graph with zero observations on %s, got %d", ref, got)
		}
	}

	g.write(`CREATE (r:Repository {id: $id, name: $id})`, map[string]any{"id": grantedRepositoryID})

	corpus := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		corpus = append(corpus, map[string]any{"digest": fmt.Sprintf("%s%06d", corpusPrefix, i)})
	}
	for _, batch := range chunkLiveRows(corpus, 500) {
		g.write(`UNWIND $rows AS row CREATE (i:ContainerImage {digest: row.digest, uid: row.digest, id: row.digest})`,
			map[string]any{"rows": batch})
	}

	withheldRows := scaleObservationRows("withheld", n, "17700000")
	for _, batch := range chunkLiveRows(withheldRows, 300) {
		g.write(scaleObservationCypher, g.obsParams(batch, withheldRef, "oci-registry://live.invalid/eshu-6705-scale-withheld"))
	}

	grantedRows := scaleObservationRows("granted", n, "17800000")
	for _, batch := range chunkLiveRows(grantedRows, 300) {
		g.write(scaleObservationCypher, g.obsParams(batch, grantedRef, "oci-registry://live.invalid/eshu-6705-scale-granted"))
		g.write(`UNWIND $rows AS row CREATE (i:ContainerImage {digest: row.digest, uid: row.digest, id: row.digest})`,
			map[string]any{"rows": batch})
		g.write(`
UNWIND $rows AS row
MATCH (i:ContainerImage {digest: row.digest})
MATCH (repo:Repository {id: $repository_id})
CREATE (i)-[:BUILT_FROM {scope_id: $repository_id, evidence_source: 'live_6705_scale'}]->(repo)`,
			map[string]any{"rows": batch, "repository_id": grantedRepositoryID})
	}

	if got := g.count(`MATCH (i:ContainerImage) WHERE i.digest STARTS WITH $prefix RETURN count(i) AS count`,
		map[string]any{"prefix": corpusPrefix}); got != n {
		g.t.Fatalf("corpus ContainerImage nodes = %d, want %d", got, n)
	}
	for _, ref := range []string{withheldRef, grantedRef} {
		if got := g.count(`MATCH (t:ContainerImageTagObservation {image_ref: $image_ref}) RETURN count(t) AS count`,
			map[string]any{"image_ref": ref}); got != tagHistoryRefillScaleObservations {
			g.t.Fatalf("observations on %s = %d, want %d", ref, got, tagHistoryRefillScaleObservations)
		}
	}
	if got := g.count(
		`MATCH (i:ContainerImage)-[b:BUILT_FROM]->(repo:Repository {id: $id}) RETURN count(b) AS count`,
		map[string]any{"id": grantedRepositoryID}); got != tagHistoryRefillScaleObservations {
		g.t.Fatalf("granted BUILT_FROM edges = %d, want %d: the seed write did not land, "+
			"so a scoped result below would be vacuous", got, tagHistoryRefillScaleObservations)
	}

	return refillScaleSeed{
		n:                   n,
		corpusPrefix:        corpusPrefix,
		imageRefWithheld:    withheldRef,
		imageRefGranted:     grantedRef,
		grantedRepositoryID: grantedRepositoryID,
		grantedCount:        tagHistoryRefillScaleObservations,
	}
}

// scaleObservationCypher is shared by the withheld and granted observation
// writes so the two sets differ only in image_ref, repository_id text and
// whether a matching ContainerImage node exists -- never in the observation
// shape itself.
const scaleObservationCypher = `
UNWIND $rows AS row
CREATE (t:ContainerImageTagObservation)
SET t.uid = row.uid,
    t.image_ref = $image_ref,
    t.tag = $tag,
    t.resolved_digest = row.digest,
    t.previous_digest = '',
    t.mutated = false,
    t.repository_id = $repository_id,
    t.identity_strength = 'digest',
    t.first_observed_at = row.at`

// scaleObservationRows builds tagHistoryRefillScaleObservations rows for one
// variant ("withheld" or "granted") of corpus size n, with fixed-width
// millisecond timestamps ascending from atPrefix so keyset ordering across
// windows is deterministic.
func scaleObservationRows(variant string, n int, atPrefix string) []map[string]any {
	rows := make([]map[string]any, 0, tagHistoryRefillScaleObservations)
	for i := 0; i < tagHistoryRefillScaleObservations; i++ {
		rows = append(rows, map[string]any{
			"uid":    fmt.Sprintf("live-6705-%s-%d-%05d", variant, n, i),
			"digest": fmt.Sprintf("sha256:6705-%s-%d-%05d", variant, n, i),
			"at":     fmt.Sprintf("%s%05d", atPrefix, i),
		})
	}
	return rows
}

func (g *refillScaleGraph) obsParams(rows []map[string]any, imageRef, repositoryID string) map[string]any {
	return map[string]any{
		"rows":          rows,
		"image_ref":     imageRef,
		"tag":           "6705-scale",
		"repository_id": repositoryID,
	}
}

func (g *refillScaleGraph) cleanup(seed refillScaleSeed) {
	g.t.Helper()
	for _, ref := range []string{seed.imageRefWithheld, seed.imageRefGranted} {
		g.write(`MATCH (t:ContainerImageTagObservation {image_ref: $image_ref}) DETACH DELETE t`,
			map[string]any{"image_ref": ref})
	}
	g.write(`MATCH (i:ContainerImage) WHERE i.digest STARTS WITH $prefix DETACH DELETE i`,
		map[string]any{"prefix": fmt.Sprintf("sha256:6705-granted-%d-", seed.n)})
	g.write(`MATCH (i:ContainerImage) WHERE i.digest STARTS WITH $prefix DETACH DELETE i`,
		map[string]any{"prefix": seed.corpusPrefix})
	g.write(`MATCH (r:Repository {id: $id}) DETACH DELETE r`, map[string]any{"id": seed.grantedRepositoryID})
}

// measure runs the fully-withheld path cold and warm through the REAL
// Neo4jReader (so the #6705 bounded-read deadline the production handler gets
// is the one this call gets too), then walks the granted path to completion,
// and asserts every #6705 correctness requirement:
//
//   - the withheld page hits the cap (CapReached, Truncated, zero rows kept);
//   - a second call from its NextKey reaches the END of history rather than
//     re-scanning the same capped span -- the walk advances;
//   - the granted page's full walk keeps every one of seed.grantedCount rows,
//     so no granted row is lost at this scale.
//
// It logs one "tag_history_refill_scale result" line per variant so
// docs/internal/evidence/6705-tag-history-refill-neo4j-scale.md can cite the
// exact reported numbers instead of a paraphrase.
func (g *refillScaleGraph) measure(t *testing.T, n int, seed refillScaleSeed) {
	t.Helper()
	access := querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{seed.grantedRepositoryID},
		Allowed:              map[string]struct{}{seed.grantedRepositoryID: {}},
	}

	var coldMS float64
	var warmMS []float64
	var first taghistory.ScopedPage
	for trial := 0; trial <= tagHistoryRefillScaleWarmTrials; trial++ {
		start := time.Now()
		page, err := taghistory.RefillScopedPage(g.ctx, g.reader, seed.imageRefWithheld, nil, tagHistoryRefillScaleLimit, access)
		elapsed := msOf(time.Since(start))
		if err != nil {
			t.Fatalf("n=%d withheld RefillScopedPage trial %d: %v", n, trial, err)
		}
		if trial == 0 {
			coldMS, first = elapsed, page
			continue
		}
		warmMS = append(warmMS, elapsed)
	}
	if !first.CapReached || !first.Truncated || len(first.Rows) != 0 || first.NextKey == nil {
		t.Fatalf("n=%d withheld page = %+v, want CapReached=true Truncated=true Rows=0 NextKey!=nil", n, first)
	}
	if got, want := first.Reads, taghistory.MaxRefillReads; got != want {
		t.Fatalf("n=%d withheld page.Reads = %d, want %d", n, got, want)
	}

	next, err := taghistory.RefillScopedPage(g.ctx, g.reader, seed.imageRefWithheld, first.NextKey, tagHistoryRefillScaleLimit, access)
	if err != nil {
		t.Fatalf("n=%d withheld continuation from NextKey: %v", n, err)
	}
	if next.Truncated {
		t.Fatalf("n=%d withheld continuation from NextKey did not reach the end of history "+
			"(Truncated=true, CapReached=%t) -- the walk is re-scanning instead of advancing", n, next.CapReached)
	}

	sort.Float64s(warmMS)
	t.Logf("tag_history_refill_scale result n=%d variant=withheld reads=%d cap_reached=%t truncated=%t "+
		"cold_ms=%.2f warm_trials=%d warm_p50_ms=%.2f warm_p95_ms=%.2f warm_max_ms=%.2f",
		n, first.Reads, first.CapReached, first.Truncated, coldMS, len(warmMS),
		percentile(warmMS, 0.50), percentile(warmMS, 0.95), percentile(warmMS, 1.0))

	var kept, pages int
	var after *taghistory.Key
	var grantedColdMS float64
	for {
		start := time.Now()
		page, err := taghistory.RefillScopedPage(g.ctx, g.reader, seed.imageRefGranted, after, tagHistoryRefillScaleLimit, access)
		elapsed := msOf(time.Since(start))
		if err != nil {
			t.Fatalf("n=%d granted RefillScopedPage page %d: %v", n, pages+1, err)
		}
		if pages == 0 {
			grantedColdMS = elapsed
		}
		pages++
		kept += len(page.Rows)
		if !page.Truncated {
			break
		}
		if page.NextKey == nil {
			t.Fatalf("n=%d granted page %d truncated with no continuation key", n, pages)
		}
		after = page.NextKey
		if pages > 20 {
			t.Fatalf("n=%d granted walk did not terminate within 20 pages", n)
		}
	}
	if kept != seed.grantedCount {
		t.Fatalf("n=%d granted kept = %d rows, want %d -- a granted row was lost", n, kept, seed.grantedCount)
	}
	t.Logf("tag_history_refill_scale result n=%d variant=granted pages=%d cold_ms=%.2f kept=%d want=%d",
		n, pages, grantedColdMS, kept, seed.grantedCount)
}

// msOf converts a duration to fractional milliseconds for the log line.
func msOf(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// percentile returns the p-th percentile (0..1) of a SORTED, non-empty slice
// using nearest-rank; it returns 0 for an empty slice rather than panicking,
// since a variant with zero warm trials still needs a printable line.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	return sorted[idx]
}
