// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_dead_code_incoming

// Live proof for the #5167 dead-code incoming-edge probe.
//
// The scoped path used to run a grant-bound probe and an unrestricted one and
// diff their rows. Both RETURN DISTINCTed the (entity, resolution_method) pair,
// so an ungranted source whose method a granted source also carried produced a
// row identical to the granted one: the diff was empty and the caller was never
// told a consumer was hidden from them. A text-capture test cannot see that --
// both statements are present and correct either way -- and neither can a fake
// that was written from the same assumption. Only a real backend settles it.
//
// The shipped replacement expands the incoming edges once and projects the
// grant per row. That shape is backend-sensitive twice over: the grouping has
// to survive a trailing OPTIONAL MATCH, and the OPTIONAL MATCH has to leave an
// unattributed source in the answer rather than dropping it.
//
// Run against the pinned replay-tier proof image:
//
//	docker run -d --name nornic-5167-incoming -e NORNICDB_EMBEDDING_ENABLED=false \
//	  -e NORNICDB_NO_AUTH=true -p 17987:7687 \
//	  timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74
//	cd go && ESHU_NEO4J_URI=bolt://localhost:17987 go test ./internal/query \
//	  -tags live_nornicdb_dead_code_incoming -run TestLiveNornicDBDeadCodeIncoming -count=1 -v
package query

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

const (
	liveIncomingTarget     = "fn:live-incoming-target"
	liveIncomingSameMethod = codeprovenance.MethodImportBinding
)

// liveIncomingAccess is the caller: granted one of the two seeded repositories.
func liveIncomingAccess() repositoryAccessFilter {
	return repositoryAccessFilter{AllowedRepositoryIDs: []string{codeGrantGrantedRepo}}
}

func liveIncomingParams() map[string]any {
	return liveIncomingAccess().GraphParams(map[string]any{"entity_ids": []any{liveIncomingTarget}})
}

// TestLiveNornicDBDeadCodeIncomingProbeSeparatesSameMethodSources is the
// correctness proof. One target, three incoming edges carrying the SAME
// resolution method: a source inside the grant, one outside it, and one the
// graph attributes to no repository at all. The shipped probe must return three
// groups with in_grant true, false, false.
func TestLiveNornicDBDeadCodeIncomingProbeSeparatesSameMethodSources(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	driver := openLiveDeadCodeIncomingDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLiveDeadCodeIncomingGraph(ctx, t, driver)

	rows := runLiveDeadCodeIncomingStatement(
		ctx, t, driver,
		buildDeadCodeScopedIncomingBatchProbeCypher("Function", liveIncomingAccess()),
	)
	if got, want := len(rows), 2; got != want {
		t.Fatalf("scoped probe returned %d group(s), want %d (one granted, one for the two sources outside the grant): %#v", got, want, rows)
	}
	granted, hidden := 0, 0
	for _, row := range rows {
		if got, want := StringVal(row, "incoming_entity_id"), liveIncomingTarget; got != want {
			t.Fatalf("incoming_entity_id = %q, want %q; the projection was not evaluated", got, want)
		}
		if got, want := StringVal(row, "resolution_method"), liveIncomingSameMethod; got != want {
			t.Fatalf("resolution_method = %q, want %q", got, want)
		}
		if BoolVal(row, "in_grant") {
			granted++
			continue
		}
		hidden++
		if got, want := IntVal(row, "edge_count"), 2; got != want {
			t.Fatalf("edge_count = %d, want %d: the ungranted source and the unattributed one both group as hidden", got, want)
		}
	}
	if granted != 1 || hidden != 1 {
		t.Fatalf("granted groups = %d, hidden groups = %d, want 1 and 1: %#v", granted, hidden, rows)
	}
}

// TestLiveNornicDBDeadCodeIncomingWithdrawnPairCollapses is the break-it half:
// the two statements this probe replaced, run against the same graph. They
// return the identical single row, so the diff that was supposed to reveal the
// hidden consumer is empty. If this ever stops collapsing, the defect the
// merged probe was built for is gone and the change can be re-argued.
func TestLiveNornicDBDeadCodeIncomingWithdrawnPairCollapses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	driver := openLiveDeadCodeIncomingDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLiveDeadCodeIncomingGraph(ctx, t, driver)

	access := liveIncomingAccess()
	withdrawnGranted := `
		UNWIND $entity_ids AS entity_id
		MATCH (e:Function {uid: entity_id})<-[rel:CALLS|IMPORTS|REFERENCES|INHERITS|EXECUTES]-(source)<-[:CONTAINS]-(source_file:File)<-[:REPO_CONTAINS]-(source_repo:Repository)
		WHERE ` + access.GraphCondition("source_repo") + `
		RETURN DISTINCT coalesce(e.uid, e.id) as incoming_entity_id,
		       rel.resolution_method as resolution_method
	`
	granted := runLiveDeadCodeIncomingStatement(ctx, t, driver, withdrawnGranted)
	signal := runLiveDeadCodeIncomingStatement(ctx, t, driver, buildDeadCodeIncomingBatchProbeCypher("Function"))
	if len(granted) != 1 || len(signal) != 1 {
		t.Fatalf("withdrawn pair returned %d granted and %d signal row(s), want 1 and 1: %#v / %#v", len(granted), len(signal), granted, signal)
	}
	if StringVal(granted[0], "resolution_method") != StringVal(signal[0], "resolution_method") {
		t.Fatalf("the withdrawn pair's rows differ, so the collapse this change fixes is not reproduced: %#v / %#v", granted[0], signal[0])
	}
}

// TestLiveNornicDBDeadCodeIncomingRejectsReturnDistinct is the negative control
// for the backend behaviour the shipped shape works around, and it covers every
// row of the variant table in docs/public/reference/nornicdb-pitfalls.md so no
// row of that public table rests on a one-off observation.
//
// On the pinned v1.2.3, DISTINCT after a trailing OPTIONAL MATCH on the
// relationship-seeded traversal branch is absorbed into the first projection's
// source text: the column comes back as the literal expression and nothing is
// deduplicated. It happens to a function call and to a plain property alike.
// Moving the projections behind a WITH is worse -- every other column comes
// back null -- and a pattern comprehension used to avoid the OPTIONAL MATCH
// entirely is not evaluated per row. count(*) is what groups the shipped probe
// instead.
//
// No CI job builds this tag, so this control only fires when someone runs it.
// Run it by hand against the pin before changing the probe's shape, and again
// after moving the pin: a failure here means the backend behaviour the shipped
// statement is built around has changed, and the shape needs re-measuring
// rather than a quick edit.
func TestLiveNornicDBDeadCodeIncomingRejectsReturnDistinct(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	driver := openLiveDeadCodeIncomingDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLiveDeadCodeIncomingGraph(ctx, t, driver)

	access := liveIncomingAccess()
	grant := access.GraphCondition("source_repo")
	expansion := `
		UNWIND $entity_ids AS entity_id
		MATCH (e:Function {uid: entity_id})<-[rel:CALLS|IMPORTS|REFERENCES|INHERITS|EXECUTES]-(source)
		OPTIONAL MATCH (source)<-[:CONTAINS]-(:File)<-[:REPO_CONTAINS]-(source_repo:Repository)
	`

	// Each case names the column the pitfall table says is corrupted and the
	// literal source text it comes back as instead.
	for _, testCase := range []struct {
		name        string
		cypher      string
		column      string
		wantLiteral string
	}{
		{
			name: "DISTINCT swallows a function call",
			cypher: expansion + `
		RETURN DISTINCT coalesce(e.uid, e.id) as incoming_entity_id,
		       rel.resolution_method as resolution_method,
		       (source_repo IS NOT NULL AND ` + grant + `) as in_grant
	`,
			column:      "incoming_entity_id",
			wantLiteral: "DISTINCT coalesce(e.uid, e.id)",
		},
		{
			name: "DISTINCT swallows a plain property too",
			cypher: expansion + `
		RETURN DISTINCT e.uid as incoming_entity_id,
		       rel.resolution_method as resolution_method,
		       (source_repo IS NOT NULL AND ` + grant + `) as in_grant
	`,
			column:      "incoming_entity_id",
			wantLiteral: "DISTINCT e.uid",
		},
		{
			name: "a WITH before the RETURN does not rescue it",
			cypher: expansion + `
		WITH coalesce(e.uid, e.id) as incoming_entity_id,
		     rel.resolution_method as resolution_method,
		     (source_repo IS NOT NULL AND ` + grant + `) as in_grant
		RETURN DISTINCT incoming_entity_id, resolution_method, in_grant
	`,
			column:      "DISTINCT incoming_entity_id",
			wantLiteral: "DISTINCT incoming_entity_id",
		},
	} {
		rows := runLiveDeadCodeIncomingStatement(ctx, t, driver, testCase.cypher)
		if len(rows) == 0 {
			t.Fatalf("%s: returned nothing; re-read the pitfall before changing the shipped probe", testCase.name)
		}
		if got := StringVal(rows[0], testCase.column); got != testCase.wantLiteral {
			t.Fatalf("%s: %s = %q, want the literal %q; the pinned backend's handling of RETURN DISTINCT after an OPTIONAL MATCH has changed, so re-measure before relying on either shape",
				testCase.name, testCase.column, got, testCase.wantLiteral)
		}
	}

	// The WITH variant nulls every column it does not corrupt, which is why it
	// is not the fallback either.
	withRows := runLiveDeadCodeIncomingStatement(ctx, t, driver, expansion+`
		WITH coalesce(e.uid, e.id) as incoming_entity_id,
		     rel.resolution_method as resolution_method,
		     (source_repo IS NOT NULL AND `+grant+`) as in_grant
		RETURN DISTINCT incoming_entity_id, resolution_method, in_grant
	`)
	if withRows[0]["resolution_method"] != nil {
		t.Fatalf("resolution_method = %#v, want null: the WITH variant no longer nulls the columns it does not corrupt", withRows[0]["resolution_method"])
	}

	// A pattern comprehension avoids the OPTIONAL MATCH but is not evaluated
	// per row: on the seeded graph all three sources answered in_grant=true and
	// DISTINCT then collapsed them to one row, hiding both hidden consumers.
	comprehension := runLiveDeadCodeIncomingStatement(ctx, t, driver, `
		UNWIND $entity_ids AS entity_id
		MATCH (e:Function {uid: entity_id})<-[rel:CALLS|IMPORTS|REFERENCES|INHERITS|EXECUTES]-(source)
		RETURN DISTINCT coalesce(e.uid, e.id) as incoming_entity_id,
		       rel.resolution_method as resolution_method,
		       size([(source)<-[:CONTAINS]-(:File)<-[:REPO_CONTAINS]-(r:Repository)
		             WHERE r.id IN $allowed_repository_ids OR r.id IN $allowed_scope_ids | 1]) > 0 as in_grant
	`)
	if len(comprehension) != 1 || !BoolVal(comprehension[0], "in_grant") {
		t.Fatalf("pattern comprehension returned %#v, want the single wrongly-granted row this backend produces; if it is now evaluated per row, the pitfall table needs updating", comprehension)
	}
}

func runLiveDeadCodeIncomingStatement(
	ctx context.Context,
	t *testing.T,
	driver neo4jdriver.DriverWithContext,
	cypher string,
) []map[string]any {
	t.Helper()

	rows, err := NewNeo4jReader(driver, "nornic").Run(ctx, cypher, liveIncomingParams())
	if err != nil {
		t.Fatalf("run %q: %v", cypher, err)
	}
	return rows
}

func openLiveDeadCodeIncomingDriver(ctx context.Context, t *testing.T) neo4jdriver.DriverWithContext {
	t.Helper()

	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		uri = "bolt://localhost:17987"
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify graph connectivity: %v", err)
	}
	return driver
}

// seedLiveDeadCodeIncomingGraph writes one target and three sources that all
// call it with the same resolution method: one in the granted repository, one
// in a repository the caller was not granted, and one attached to no repository
// at all. MERGE keeps repeated runs against a retained store idempotent.
func seedLiveDeadCodeIncomingGraph(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext) {
	t.Helper()

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "nornic",
		AccessMode:   neo4jdriver.AccessModeWrite,
	})
	defer func() { _ = session.Close(ctx) }()

	statements := []string{
		`MERGE (r:Repository {id:"` + codeGrantGrantedRepo + `"}) SET r.name="granted-service"`,
		`MERGE (r:Repository {id:"` + codeGrantOtherRepo + `"}) SET r.name="other-service"`,
		`MERGE (f:File {uid:"file:live-incoming-granted"}) SET f.id="file:live-incoming-granted"`,
		`MERGE (f:File {uid:"file:live-incoming-ungranted"}) SET f.id="file:live-incoming-ungranted"`,
		`MATCH (r:Repository {id:"` + codeGrantGrantedRepo + `"}),(f:File {uid:"file:live-incoming-granted"}) MERGE (r)-[:REPO_CONTAINS]->(f)`,
		`MATCH (r:Repository {id:"` + codeGrantOtherRepo + `"}),(f:File {uid:"file:live-incoming-ungranted"}) MERGE (r)-[:REPO_CONTAINS]->(f)`,
		`MERGE (e:Function {uid:"` + liveIncomingTarget + `"}) SET e.id="` + liveIncomingTarget + `", e.name="LiveIncomingTarget"`,
		`MERGE (s:Function {uid:"fn:live-incoming-granted"}) SET s.id="fn:live-incoming-granted"`,
		`MERGE (s:Function {uid:"fn:live-incoming-ungranted"}) SET s.id="fn:live-incoming-ungranted"`,
		`MERGE (s:Function {uid:"fn:live-incoming-orphan"}) SET s.id="fn:live-incoming-orphan"`,
		`MATCH (f:File {uid:"file:live-incoming-granted"}),(s:Function {uid:"fn:live-incoming-granted"}) MERGE (f)-[:CONTAINS]->(s)`,
		`MATCH (f:File {uid:"file:live-incoming-ungranted"}),(s:Function {uid:"fn:live-incoming-ungranted"}) MERGE (f)-[:CONTAINS]->(s)`,
		`MATCH (s:Function {uid:"fn:live-incoming-granted"}),(e:Function {uid:"` + liveIncomingTarget + `"}) MERGE (s)-[c:CALLS]->(e) SET c.resolution_method="` + liveIncomingSameMethod + `"`,
		`MATCH (s:Function {uid:"fn:live-incoming-ungranted"}),(e:Function {uid:"` + liveIncomingTarget + `"}) MERGE (s)-[c:CALLS]->(e) SET c.resolution_method="` + liveIncomingSameMethod + `"`,
		`MATCH (s:Function {uid:"fn:live-incoming-orphan"}),(e:Function {uid:"` + liveIncomingTarget + `"}) MERGE (s)-[c:CALLS]->(e) SET c.resolution_method="` + liveIncomingSameMethod + `"`,
	}
	for _, stmt := range statements {
		if _, err := session.Run(ctx, stmt, nil); err != nil {
			t.Fatalf("seed statement %q: %v", stmt, err)
		}
	}
}
