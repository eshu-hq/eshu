// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build queryplan_profile_live

package query

import (
	"context"
	"fmt"
	"os"
	"slices"
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

func TestQueryplanBoundedAnchorOperatorPolicyIsClosed(t *testing.T) {
	tests := map[string][]string{
		"QP-GRAPH-ENTITY-COUNT":                           {"NodeCountFromCountStore"},
		"QP-GRAPH-ENTITY-LIST":                            {"NodeByLabelScan"},
		"QP-RESOURCE-INVESTIGATION-SELECTOR":              {"NodeByLabelScan"},
		"QP-RESOURCE-INVESTIGATION-WORKLOADS":             {"DirectedRelationshipTypeScan"},
		"QP-RESOURCE-INVESTIGATION-REPO-PATHS":            {"NodeByLabelScan"},
		"QP-CODE-REL-STORY-ANCHOR-COLLISION":              {"NodeByLabelScan"},
		"QP-RELATIONSHIPS-CATALOG-COUNT":                  {"RelationshipCountFromCountStore"},
		"QP-RELATIONSHIPS-EDGES":                          {"DirectedRelationshipTypeScan"},
		"QP-RELATIONSHIPS-CATALOG-SOURCE-TOOL-REPOSITORY": {"NodeByLabelScan"},
		"QP-RELATIONSHIPS-CATALOG-SOURCE-TOOL-INSTANCE":   {"DirectedRelationshipTypeScan"},
		"QP-INFRA-RESOURCE-SEARCH":                        {"NodeByLabelScan"},
		"QP-INFRA-RESOURCE-AGGREGATE":                     {"NodeByLabelScan"},
		"unregistered-or-indexed-production-path":         {"NodeIndexSeek", "NodeUniqueIndexSeek", "NodeCountFromCountStore"},
	}
	for entryID, want := range tests {
		if got := queryplanBoundedAnchorOperators(entryID); !slices.Equal(got, want) {
			t.Errorf("queryplanBoundedAnchorOperators(%q) = %v, want %v", entryID, got, want)
		}
	}
	variantTests := map[string][]string{
		"cloud-resource-list/unfiltered":                                          {"NodeByLabelScan"},
		"cloud-resource-list/provider+region+account":                             {"NodeByLabelScan"},
		"cloud-resource-list/resource-type":                                       {"NodeIndexSeek", "NodeUniqueIndexSeek"},
		"cloud-resource-list/cursor":                                              {"NodeIndexSeek", "NodeUniqueIndexSeek"},
		"resource-selector/all/default/any-environment/exact/label-cloudresource": {"NodeByLabelScan"},
	}
	for name, want := range variantTests {
		if got := queryplanProductionVariantAnchorOperators(name); !slices.Equal(got, want) {
			t.Errorf("queryplanProductionVariantAnchorOperators(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestQueryplanForbiddenOperatorPolicyIsClosed(t *testing.T) {
	entry := queryplan.Entry{Plan: queryplan.PlanExpectation{ForbiddenOperators: []string{"Eager"}}}
	want := []string{"AllNodesScan", "CartesianProduct", "UnboundedExpand", "Eager"}
	if got := queryplanForbiddenOperators(entry); !slices.Equal(got, want) {
		t.Fatalf("queryplanForbiddenOperators() = %v, want %v", got, want)
	}
	scalarEntry := queryplan.Entry{ID: "QP-GRAPH-ENTITY-COUNT"}
	if got, want := queryplanForbiddenOperators(scalarEntry), []string{"AllNodesScan", "UnboundedExpand"}; !slices.Equal(got, want) {
		t.Fatalf("queryplanForbiddenOperators(scalar count) = %v, want %v", got, want)
	}
}

func TestProductionQueryplanProfilesRejectWholeGraphScans(t *testing.T) {
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
	manifest, err = queryplan.BindProductionCypher(manifest, handlerQueryplanProductionCypher())
	if err != nil {
		t.Fatalf("bind production handler Cypher: %v", err)
	}
	legacyManifest, err := queryplan.LoadManifestFile("../queryplan/testdata/hot-cypher.yaml")
	if err != nil {
		t.Fatalf("load legacy queryplan manifest: %v", err)
	}
	legacyManifest, err = queryplan.BindProductionCypher(legacyManifest, legacyQueryplanProductionCypher(t))
	if err != nil {
		t.Fatalf("bind production legacy Cypher: %v", err)
	}
	manifest.Entries = append(manifest.Entries, legacyManifest.Entries...)
	applyQueryplanProfileSchema(ctx, t, session, manifest)
	for _, entry := range manifest.Entries {
		entry := entry
		if strings.TrimSpace(entry.Cypher) == "" {
			continue
		}
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
			assertProfileCartesianBound(t, entry, operators)
			assertProfileHasBoundedAnchor(t, entry, operators)
			t.Logf("operators=%s", strings.Join(operators, ","))
		})
	}
	profileQueryplanSafeProductionVariants(ctx, t, session)
}

func profileQueryplanSafeProductionVariants(
	ctx context.Context,
	t *testing.T,
	session neo4jdriver.SessionWithContext,
) {
	t.Helper()
	variants := handlerQueryplanSafeCypherVariants()
	names := make([]string, 0, len(variants))
	for name := range variants {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		name := name
		t.Run("production-variant/"+name, func(t *testing.T) {
			result, err := session.Run(ctx, "PROFILE "+variants[name], queryplanProfileParams())
			if err != nil {
				t.Fatalf("PROFILE production variant: %v", err)
			}
			summary, err := result.Consume(ctx)
			if err != nil {
				t.Fatalf("consume production variant PROFILE: %v", err)
			}
			profile := summary.Profile()
			if profile == nil {
				t.Fatal("production variant PROFILE returned no plan")
			}
			operators := profiledPlanOperators(profile)
			assertProductionVariantOperators(t, name, operators)
			t.Logf("operators=%s", strings.Join(operators, ","))
		})
	}
}

func assertProductionVariantOperators(t *testing.T, name string, operators []string) {
	t.Helper()
	for _, forbidden := range []string{"AllNodesScan", "CartesianProduct", "UnboundedExpand"} {
		for _, operator := range operators {
			if strings.EqualFold(operator, forbidden) {
				t.Fatalf("production variant %s contains forbidden operator %s: %v", name, operator, operators)
			}
		}
	}
	allowed := queryplanProductionVariantAnchorOperators(name)
	for _, operator := range operators {
		for _, candidate := range allowed {
			if strings.EqualFold(operator, candidate) {
				return
			}
		}
	}
	t.Fatalf("production variant %s has no bounded anchor operator (%s): %v", name, fmt.Sprint(allowed), operators)
}

func queryplanProductionVariantAnchorOperators(name string) []string {
	switch {
	case strings.HasPrefix(name, "import-dependencies/"):
		return []string{"NodeIndexSeek", "NodeUniqueIndexSeek", "NodeByLabelScan", "DirectedRelationshipTypeScan"}
	case strings.HasPrefix(name, "cloud-resource-list/") &&
		!strings.Contains(name, "resource-type") &&
		!strings.Contains(name, "cursor"):
		return []string{"NodeByLabelScan"}
	case strings.HasPrefix(name, "resource/") && strings.HasSuffix(name, "/workloads"):
		return []string{"DirectedRelationshipTypeScan"}
	case strings.HasPrefix(name, "resource/") && strings.Contains(name, "/paths/"):
		return []string{"NodeByLabelScan", "NodeIndexSeek", "NodeUniqueIndexSeek"}
	case strings.HasPrefix(name, "resource-selector/"):
		return []string{"NodeByLabelScan"}
	default:
		return []string{"NodeIndexSeek", "NodeUniqueIndexSeek"}
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
	for _, forbidden := range queryplanForbiddenOperators(entry) {
		for _, operator := range operators {
			if strings.EqualFold(operator, forbidden) {
				t.Fatalf("PROFILE contains forbidden operator %s: %v", operator, operators)
			}
		}
	}
}

func queryplanForbiddenOperators(entry queryplan.Entry) []string {
	forbidden := []string{"AllNodesScan"}
	if entry.ID != "QP-GRAPH-ENTITY-COUNT" {
		forbidden = append(forbidden, "CartesianProduct")
	}
	forbidden = append(forbidden, "UnboundedExpand")
	seen := make(map[string]struct{}, len(forbidden))
	for _, operator := range forbidden {
		seen[operator] = struct{}{}
	}
	for _, operator := range entry.Plan.ForbiddenOperators {
		operator = strings.TrimSpace(operator)
		if operator == "" {
			continue
		}
		if _, ok := seen[operator]; ok {
			continue
		}
		seen[operator] = struct{}{}
		forbidden = append(forbidden, operator)
	}
	return forbidden
}

func assertProfileCartesianBound(t *testing.T, entry queryplan.Entry, operators []string) {
	t.Helper()
	want := 0
	if entry.ID == "QP-GRAPH-ENTITY-COUNT" {
		want = len(graphEntityKinds) - 1
	}
	got := 0
	for _, operator := range operators {
		if strings.EqualFold(operator, "CartesianProduct") {
			got++
		}
	}
	if got != want {
		t.Fatalf("PROFILE CartesianProduct count = %d, want exactly %d: %v", got, want, operators)
	}
	if entry.ID != "QP-GRAPH-ENTITY-COUNT" {
		return
	}
	countStoreAnchors := 0
	for _, operator := range operators {
		if strings.EqualFold(operator, "NodeCountFromCountStore") {
			countStoreAnchors++
		}
	}
	if countStoreAnchors != len(graphEntityKinds) {
		t.Fatalf("PROFILE NodeCountFromCountStore count = %d, want exactly %d: %v",
			countStoreAnchors, len(graphEntityKinds), operators)
	}
}

func assertProfileHasBoundedAnchor(t *testing.T, entry queryplan.Entry, operators []string) {
	t.Helper()
	allowed := queryplanBoundedAnchorOperators(entry.ID)
	for _, operator := range operators {
		for _, candidate := range allowed {
			if strings.EqualFold(operator, candidate) {
				return
			}
		}
	}
	t.Fatalf("PROFILE has no bounded anchor operator (%s): %v", fmt.Sprint(allowed), operators)
}

func queryplanBoundedAnchorOperators(entryID string) []string {
	switch entryID {
	case "QP-GRAPH-ENTITY-LIST", "QP-RESOURCE-INVESTIGATION-SELECTOR", "QP-RESOURCE-INVESTIGATION-REPO-PATHS",
		"QP-CODE-REL-STORY-ANCHOR-COLLISION",
		"QP-RELATIONSHIPS-CATALOG-SOURCE-TOOL-REPOSITORY",
		"QP-INFRA-RESOURCE-SEARCH", "QP-INFRA-RESOURCE-AGGREGATE":
		return []string{"NodeByLabelScan"}
	case "QP-RESOURCE-INVESTIGATION-WORKLOADS", "QP-RELATIONSHIPS-EDGES",
		"QP-RELATIONSHIPS-CATALOG-SOURCE-TOOL-INSTANCE":
		return []string{"DirectedRelationshipTypeScan"}
	case "QP-RELATIONSHIPS-CATALOG-COUNT":
		return []string{"RelationshipCountFromCountStore"}
	case "QP-GRAPH-ENTITY-COUNT":
		return []string{"NodeCountFromCountStore"}
	default:
		return []string{"NodeIndexSeek", "NodeUniqueIndexSeek", "NodeCountFromCountStore"}
	}
}
