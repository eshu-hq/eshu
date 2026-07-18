// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build queryplan_profile_live

package query

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/queryplan"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	queryplanProfileLiveEnv     = "ESHU_QUERYPLAN_PROFILE_LIVE"
	queryplanProfileIsolatedEnv = "ESHU_QUERYPLAN_PROFILE_ISOLATED"
)

func TestHandlerQueryplanProfilesRejectWholeGraphScans(t *testing.T) {
	if os.Getenv(queryplanProfileLiveEnv) != "1" {
		t.Skipf("set %s=1 to run live handler PROFILE assertions", queryplanProfileLiveEnv)
	}
	if os.Getenv(queryplanProfileIsolatedEnv) != "1" {
		t.Fatal("ESHU_QUERYPLAN_PROFILE_ISOLATED=1 is required because this test creates schema objects")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	username := strings.TrimSpace(os.Getenv("ESHU_NEO4J_USERNAME"))
	password := os.Getenv("ESHU_NEO4J_PASSWORD")
	auth := neo4jdriver.NoAuth()
	if username != "" {
		auth = neo4jdriver.BasicAuth(username, password, "")
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, auth)
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify graph connectivity: %v", err)
	}
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE")),
	})
	defer func() { _ = session.Close(context.Background()) }()
	manifest, err := queryplan.LoadManifestFile("../queryplan/testdata/handler-hot-cypher.yaml")
	if err != nil {
		t.Fatalf("load handler queryplan manifest: %v", err)
	}
	applyQueryplanProfileSchema(ctx, t, session, manifest)
	for _, entry := range manifest.Entries {
		entry := entry
		t.Run(entry.ID, func(t *testing.T) {
			result, err := session.Run(ctx, "PROFILE "+entry.Cypher, queryplanProfileParams())
			if err != nil {
				t.Fatalf("PROFILE query: %v", err)
			}
			summary, err := result.Consume(ctx)
			if err != nil {
				t.Fatalf("consume PROFILE: %v", err)
			}
			profile := summary.Profile()
			if profile == nil {
				t.Fatal("PROFILE returned no plan")
			}
			operators := profiledPlanOperators(profile)
			assertProfileExcludesOperators(t, entry, operators)
			assertProfileHasBoundedAnchor(t, entry, operators)
			t.Logf("operators=%s", strings.Join(operators, ","))
		})
	}
}

func applyQueryplanProfileSchema(
	ctx context.Context,
	t *testing.T,
	session neo4jdriver.SessionWithContext,
	manifest queryplan.Manifest,
) {
	t.Helper()
	statements, err := graph.SchemaStatementsForBackend(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("Neo4j schema statements: %v", err)
	}
	required := make(map[string]struct{})
	for _, entry := range manifest.Entries {
		for _, schemaName := range entry.RequiredSchema {
			required[schemaName] = struct{}{}
		}
	}
	for _, statement := range statements {
		fields := strings.Fields(statement)
		if len(fields) < 3 || strings.ToUpper(fields[0]) != "CREATE" {
			continue
		}
		if _, ok := required[fields[2]]; !ok {
			continue
		}
		result, err := session.Run(ctx, statement, nil)
		if err != nil {
			t.Fatalf("apply PROFILE proof schema %q: %v", statement, err)
		}
		if _, err := result.Consume(ctx); err != nil {
			t.Fatalf("consume PROFILE proof schema %q: %v", statement, err)
		}
	}
	result, err := session.Run(ctx, "CALL db.awaitIndexes(120)", nil)
	if err != nil {
		t.Fatalf("await PROFILE proof indexes: %v", err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("consume index wait: %v", err)
	}
}

func queryplanProfileParams() map[string]any {
	return map[string]any{
		"environment":   "",
		"from":          "proof-repository",
		"from_id":       "proof-repository",
		"instance_ids":  []string{"proof-instance"},
		"limit":         10,
		"name":          "proof",
		"offset":        0,
		"q":             "proof",
		"query":         "proof",
		"repo_id":       "proof-repository",
		"resource_id":   "proof-resource",
		"resource_type": "proof-type",
		"source_file":   "proof.go",
	}
}

func profiledPlanOperators(plan neo4jdriver.ProfiledPlan) []string {
	operator, _, _ := strings.Cut(plan.Operator(), "@")
	operators := []string{operator}
	for _, child := range plan.Children() {
		operators = append(operators, profiledPlanOperators(child)...)
	}
	sort.Strings(operators)
	return operators
}

func assertProfileExcludesOperators(t *testing.T, entry queryplan.Entry, operators []string) {
	t.Helper()
	for _, forbidden := range entry.Plan.ForbiddenOperators {
		for _, operator := range operators {
			if strings.EqualFold(operator, forbidden) {
				t.Fatalf("PROFILE contains forbidden operator %s: %v", operator, operators)
			}
		}
	}
}

func assertProfileHasBoundedAnchor(t *testing.T, entry queryplan.Entry, operators []string) {
	t.Helper()
	allowed := []string{"NodeIndexSeek", "NodeUniqueIndexSeek", "NodeCountFromCountStore"}
	if entry.ID == "QP-GRAPH-ENTITY-LIST" {
		allowed = append(allowed, "NodeByLabelScan")
	}
	for _, operator := range operators {
		for _, candidate := range allowed {
			if strings.EqualFold(operator, candidate) {
				return
			}
		}
	}
	t.Fatalf("PROFILE has no bounded anchor operator (%s): %v", fmt.Sprint(allowed), operators)
}
