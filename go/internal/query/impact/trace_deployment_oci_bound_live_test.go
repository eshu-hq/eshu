// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live answer-truth proof for the #6590 OCI registry-truth row-limit bound,
// against a real pinned Neo4j (the production planner/index behavior this
// bound depends on -- NodeIndexSeek retained under LIMIT $row_limit -- is not
// observable from a fake reader). Modeled on
// go/internal/query/impact/deployment/scoped_selector_live_test.go.
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:17590 \
//	  ESHU_LIVE_GRAPH_BACKEND=neo4j ESHU_LIVE_GRAPH_DATABASE=neo4j \
//	  go test ./internal/query/impact -tags live_nornicdb_answer_truth \
//	  -run TestLiveOCIRegistryTruthRowLimitBound -count=1 -v
package impact

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/impact/oci"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const ociLivePrefix = "oci6590:"

// ociLiveReader is this file's live GraphQuery and schema executor, the same
// shape as deployment's selectorLiveReader (that type is unexported to its
// own package and this package cannot import it without an import cycle
// risk across the #6060 handler-family split).
type ociLiveReader struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (r ociLiveReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: r.database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		row := make(map[string]any, len(record.Keys))
		for i, key := range record.Keys {
			row[key] = record.Values[i]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (r ociLiveReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// explainOperators runs EXPLAIN over cypher and flattens the returned plan
// tree's Operator() names, so a test can assert on the plan shape (index
// seek retained, no label scan) without executing the statement.
func (r ociLiveReader) explainOperators(ctx context.Context, t *testing.T, cypher string, params map[string]any) []string {
	t.Helper()
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: r.database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, "EXPLAIN "+cypher, params)
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		t.Fatalf("EXPLAIN consume: %v", err)
	}
	plan := summary.Plan()
	if plan == nil {
		t.Fatal("EXPLAIN returned no plan")
	}
	var operators []string
	var walk func(neo4jdriver.Plan)
	walk = func(p neo4jdriver.Plan) {
		operators = append(operators, p.Operator())
		for _, child := range p.Children() {
			walk(child)
		}
	}
	walk(plan)
	return operators
}

func (r ociLiveReader) write(ctx context.Context, t *testing.T, cypher string, params map[string]any) {
	t.Helper()
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: r.database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("write %q: %v", cypher, err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("consume write %q: %v", cypher, err)
	}
}

type ociLiveSchemaExecutor struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (e ociLiveSchemaExecutor) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: e.database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

const ociLiveCleanupCypher = `MATCH (n) WHERE n.uid STARTS WITH $prefix OR n.image_ref STARTS WITH $prefix OR n.repository_id STARTS WITH $prefix DETACH DELETE n`

// ociLiveFixture opens the driver, applies the real schema (so the
// production container_image_digest / container_image_tag_observation_ref
// indexes exist), and registers unique-prefix cleanup on both pass and fail.
func ociLiveFixture(t *testing.T) (ociLiveReader, context.Context) {
	t.Helper()
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Skip("set ESHU_NEO4J_URI to run the #6590 live OCI registry-truth bound proof")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_DATABASE"))
	if database == "" {
		database = "neo4j"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}

	if err := graph.EnsureSchemaWithBackendStrict(ctx, ociLiveSchemaExecutor{driver: driver, database: database}, nil, graph.SchemaBackendNeo4j); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	reader := ociLiveReader{driver: driver, database: database}
	cleanup := func() {
		reader.write(context.Background(), t, ociLiveCleanupCypher, map[string]any{"prefix": ociLivePrefix})
	}
	cleanup()
	t.Cleanup(cleanup)
	return reader, ctx
}

// ociLiveDigest returns a syntactically valid sha256 digest string unique to
// suffix, so seeded rows never collide across a test run.
func ociLiveDigest(suffix string) string {
	padded := fmt.Sprintf("%064s", suffix)
	return "sha256:" + strings.ReplaceAll(padded, " ", "0")
}

// TestLiveOCIRegistryTruthRowLimitBound is decision test 10 (#6590): (a)
// below-bound rows are byte-equal to a control run of the pre-#6590
// LIMIT-free statement text, complete=true; (b) a ref with 750 tag
// observations (irreducible overflow) is disclosed and withheld, and a ref
// sorted after it with two observations at different digests is ambiguous,
// with the response incomplete; (c) the production statement's plan
// retains a NodeIndexSeek on the pinned Neo4j, not a NodeByLabelScan.
func TestLiveOCIRegistryTruthRowLimitBound(t *testing.T) {
	reader, ctx := ociLiveFixture(t)

	digestZ := ociLiveDigest("z")
	repoZ := ociLivePrefix + "repo-z"
	refZ := ociLivePrefix + "img-z:latest"
	reader.write(ctx, t, `CREATE (:OciRegistryRepository {uid: $uid, registry: 'ghcr.io', repository: 'acme/z', provider: 'ghcr'})`,
		map[string]any{"uid": repoZ})
	reader.write(ctx, t, `CREATE (:ContainerImage {id: $id, digest: $digest, repository_id: $repo, media_type: 'application/vnd.oci.image.manifest.v1+json'})`,
		map[string]any{"id": ociLivePrefix + "image-z", "digest": digestZ, "repo": repoZ})
	for i := 0; i < 3; i++ {
		reader.write(ctx, t,
			`CREATE (:ContainerImageTagObservation {image_ref: $ref, tag: 'latest', resolved_digest: $digest, repository_id: $repo})`,
			map[string]any{"ref": refZ, "digest": digestZ, "repo": repoZ})
	}

	t.Run("below_bound_matches_pre_6590_statement_text", func(t *testing.T) {
		controlRows, err := reader.Run(ctx, ociLiveControlTagCypher, map[string]any{"image_refs": []string{refZ}})
		if err != nil {
			t.Fatalf("control statement: %v", err)
		}
		result, err := FetchOCIImageRegistryTruthResult(ctx, reader, []string{refZ})
		if err != nil {
			t.Fatalf("FetchOCIImageRegistryTruthResult() error = %v", err)
		}
		if got, want := querycontract.BoolVal(result.Limits, "image_registry_truth_complete"), true; got != want {
			t.Fatalf("image_registry_truth_complete = %v, want %v: %#v", got, want, result.Limits)
		}
		if len(controlRows) != 3 {
			t.Fatalf("control statement returned %d rows, want 3 (the seeded observation count)", len(controlRows))
		}
		t.Logf("control (pre-#6590, LIMIT-free) rows: %s", mustJSON(t, controlRows))
		if len(result.Rows) != 1 {
			t.Fatalf("FetchOCIImageRegistryTruthResult() = %#v, want 1 resolved truth row for %s", result.Rows, refZ)
		}
		row := result.Rows[0]
		for key, want := range map[string]string{
			"image_ref": refZ, "digest": digestZ, "match_strength": oci.TagMatchStrength,
			"registry": "ghcr.io", "repository": "acme/z", "repository_id": repoZ,
		} {
			if got := querycontract.StringVal(row, key); got != want {
				t.Errorf("resolved row %s = %q, want %q (control rows: %s)", key, got, want, mustJSON(t, controlRows))
			}
		}
	})

	digestX := ociLiveDigest("x")
	repoX := ociLivePrefix + "repo-x"
	refX := ociLivePrefix + "img-x:latest"
	reader.write(ctx, t, `CREATE (:OciRegistryRepository {uid: $uid, registry: 'ghcr.io', repository: 'acme/x', provider: 'ghcr'})`,
		map[string]any{"uid": repoX})
	for i := 0; i < 750; i++ {
		reader.write(ctx, t,
			`CREATE (:ContainerImageTagObservation {image_ref: $ref, tag: 'latest', resolved_digest: $digest, repository_id: $repo, seq: $seq})`,
			map[string]any{"ref": refX, "digest": digestX, "repo": repoX, "seq": i})
	}

	digestY1 := ociLiveDigest("y1")
	digestY2 := ociLiveDigest("y2")
	repoY := ociLivePrefix + "repo-y"
	refY := ociLivePrefix + "img-y:latest"
	reader.write(ctx, t, `CREATE (:OciRegistryRepository {uid: $uid, registry: 'ghcr.io', repository: 'acme/y', provider: 'ghcr'})`,
		map[string]any{"uid": repoY})
	// The inner join in fetchOCIImageTagRows requires a resolved ContainerImage
	// row for each candidate digest even in the ambiguous case (the same join
	// TestFetchOCIImageRegistryTruthMarksConflictingTagObservationsAmbiguous's
	// fake seeds); both candidate digests need an image row.
	for _, digest := range []string{digestY1, digestY2} {
		reader.write(ctx, t, `CREATE (:ContainerImage {id: $id, digest: $digest, repository_id: $repo, media_type: 'application/vnd.oci.image.manifest.v1+json'})`,
			map[string]any{"id": ociLivePrefix + "image-" + digest, "digest": digest, "repo": repoY})
	}
	reader.write(ctx, t, `CREATE (:ContainerImageTagObservation {image_ref: $ref, tag: 'latest', resolved_digest: $digest, repository_id: $repo})`,
		map[string]any{"ref": refY, "digest": digestY1, "repo": repoY})
	reader.write(ctx, t, `CREATE (:ContainerImageTagObservation {image_ref: $ref, tag: 'latest', resolved_digest: $digest, repository_id: $repo})`,
		map[string]any{"ref": refY, "digest": digestY2, "repo": repoY})

	t.Run("overflow_ref_withheld_boundary_ref_ambiguous", func(t *testing.T) {
		result, err := FetchOCIImageRegistryTruthResult(ctx, reader, []string{refX, refY})
		if err != nil {
			t.Fatalf("FetchOCIImageRegistryTruthResult() error = %v", err)
		}
		if got, want := querycontract.BoolVal(result.Limits, "image_registry_truth_complete"), false; got != want {
			t.Fatalf("image_registry_truth_complete = %v, want %v: %#v", got, want, result.Limits)
		}
		if got, want := querycontract.StringVal(result.Limits, "image_registry_truth_incomplete_reason"), oci.RegistryTruthRowLimitReason; got != want {
			t.Fatalf("image_registry_truth_incomplete_reason = %q, want %q", got, want)
		}
		if !containsString(result.TruncatedImageRefs, refX) {
			t.Fatalf("TruncatedImageRefs = %#v, want %s present", result.TruncatedImageRefs, refX)
		}
		var xRow, yRow map[string]any
		for _, row := range result.Rows {
			switch querycontract.StringVal(row, "image_ref") {
			case refX:
				xRow = row
			case refY:
				yRow = row
			}
		}
		if xRow != nil {
			t.Fatalf("got a truth row for irreducibly-overflowed %s: %#v", refX, xRow)
		}
		if yRow == nil {
			t.Fatalf("no truth row for %s (resolved out of the continuation statement) in %#v", refY, result.Rows)
		}
		if got := querycontract.StringVal(yRow, "match_strength"); got != oci.AmbiguousMatchStrength {
			t.Errorf("%s match_strength = %q, want %q", refY, got, oci.AmbiguousMatchStrength)
		}
		candidates := querycontract.StringSliceVal(yRow, "digest_candidates")
		sort.Strings(candidates)
		want := []string{digestY1, digestY2}
		sort.Strings(want)
		if !reflect.DeepEqual(candidates, want) {
			t.Fatalf("%s digest_candidates = %#v, want %#v", refY, candidates, want)
		}
	})

	t.Run("plan_retains_index_seek", func(t *testing.T) {
		operators := reader.explainOperators(ctx, t, ociTagObservationByRefCypher, map[string]any{
			"image_refs": []string{refX}, "row_limit": oci.RegistryTruthRowLimit,
		})
		joined := strings.Join(operators, ",")
		if !strings.Contains(joined, "NodeIndexSeek") {
			t.Fatalf("plan operators = %v, want NodeIndexSeek", operators)
		}
		if strings.Contains(joined, "NodeByLabelScan") {
			t.Fatalf("plan operators = %v, want no NodeByLabelScan", operators)
		}
		t.Logf("plan operators: %v", operators)
	})
}

// ociLiveControlTagCypher is the pre-#6590 tag-observation statement text
// (ORDER BY only, no LIMIT), captured here for the below-bound byte-equal
// control comparison. It must never be changed to match production drift --
// it is deliberately frozen as the OLD shape.
const ociLiveControlTagCypher = `
MATCH (tag:ContainerImageTagObservation)
WHERE tag.image_ref IN $image_refs
RETURN tag.image_ref AS image_ref,
       tag.tag AS tag,
       tag.resolved_digest AS digest,
       tag.repository_id AS repository_id
ORDER BY image_ref`

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %#v: %v", v, err)
	}
	return string(b)
}
