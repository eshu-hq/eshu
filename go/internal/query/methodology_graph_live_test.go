// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build queryplan_profile_live

package query

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/imports"
	neo4j "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestImportDependencyMethodologyLive exercises each valid request through the
// shipped reader and profiles each distinct statement it actually emits.
func TestImportDependencyMethodologyLive(t *testing.T) {
	if os.Getenv("ESHU_QUERY_METHODOLOGY_LIVE") != "1" {
		t.Skip("set ESHU_QUERY_METHODOLOGY_LIVE=1")
	}
	if os.Getenv(queryplanProfileIsolatedEnv) != "1" {
		t.Fatal("isolated Neo4j required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	driver := methodologyGraphDriver(t, ctx)
	defer func() { _ = driver.Close(context.Background()) }()
	database := strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	if database == "" {
		database = "neo4j"
	}
	methodologySeedGraph(t, ctx, driver, database)
	methodologyProveMissingGraphIndex(t, ctx, driver, database)
	versionRows := methodologyExecute(t, ctx, driver, database, "CALL dbms.components() YIELD name, versions RETURN name, versions", nil)
	t.Logf("backend_components=%v", versionRows)
	recorder := &methodologyGraphReader{driver: driver, database: database}
	requests := reachableImportDependencyQueryplanRequests()
	expectedByRequest := make(map[string][]string, len(requests))
	actualByRequest := make(map[string][]string, len(requests))
	moreByRequest := make(map[string]bool, len(requests))
	requestQueryCounts := make(map[string]int, len(requests))
	maxRequestQueryCount := 0
	if len(requests) != 488 {
		t.Fatalf("valid request shapes=%d, want 488", len(requests))
	}
	for _, request := range requests {
		requestName := importDependencyQueryplanRequestName(request)
		if _, duplicate := requestQueryCounts[requestName]; duplicate {
			t.Fatalf("duplicate request shape %s", requestName)
		}
		recorder.requestName = requestName
		if err := request.Validate(); err != nil {
			t.Fatalf("invalid enumerated request: %v", err)
		}
		priorCalls := recorder.calls
		rows, enumeration, err := imports.Rows(ctx, recorder, request)
		if err != nil {
			t.Fatalf("request %s: %v", importDependencyQueryplanRequestName(request), err)
		}
		queryCount := recorder.calls - priorCalls
		if err := methodologyRequestQueryCountWithinBudget(queryCount, 3); err != nil {
			t.Fatalf("request %s: %v", requestName, err)
		}
		requestQueryCounts[requestName] = queryCount
		if queryCount > maxRequestQueryCount {
			maxRequestQueryCount = queryCount
		}
		response := codemodel.ImportDependencyResponseWithCycleEnumeration(request, rows, enumeration)
		methodologyAssertResponse(t, request, response)
		wantIDs, wantMore := methodologyExpectedIdentityPage(request)
		gotIDs := methodologyResponseIdentities(t, request, response)
		if !slices.Equal(gotIDs, wantIDs) || response["has_more"] != wantMore {
			t.Fatalf("request %s identities=%v has_more=%v, fixture oracle wants %v %v", importDependencyQueryplanRequestName(request), gotIDs, response["has_more"], wantIDs, wantMore)
		}
		expectedByRequest[requestName] = wantIDs
		actualByRequest[requestName] = gotIDs
		moreByRequest[requestName] = wantMore
	}
	methodologyAssertSemanticCases(t, ctx, recorder)
	if got := len(recorder.statements); got != 280 {
		t.Fatalf("executed distinct texts=%d, want 280", got)
	}
	registered := importDependencyQueryplanVariants()
	expected := make(map[string]string, len(registered))
	for name, cypher := range registered {
		expected[cypher] = name
	}
	report := methodologyReport{BackendComponents: versionRows, ValidRequests: len(requests), RequestQueryCounts: requestQueryCounts, MaxRequestQueryCount: maxRequestQueryCount, RequestQueryCountBudget: 3}
	for cypher, capture := range recorder.statements {
		name, ok := expected[cypher]
		if !ok {
			t.Fatalf("unregistered executed text sha256=%s", methodologyHash(cypher))
		}
		entryID := methodologyEntryID(name)
		capture.entryID = entryID
		capture.expectedIDs = methodologyOracleStatementIDs(entryID, capture.params)
		capture.actualIDs = methodologyStatementIDs(entryID, capture.data)
		if !slices.Equal(capture.actualIDs, capture.expectedIDs) {
			t.Fatalf("statement %s raw IDs=%v, independent fixture wants %v", name, capture.actualIDs, capture.expectedIDs)
		}
		oracleRecordedAt := time.Now().UTC()
		statement := methodologyStatementReport{
			VariantID: name, EntryID: entryID, CaseID: "representative", ScopeMode: methodologyScopeMode(name), RequestName: capture.requestName,
			Expected: expectedByRequest[capture.requestName], Actual: actualByRequest[capture.requestName], HasMore: moreByRequest[capture.requestName], SHA256: methodologyHash(cypher),
			EmittedText: cypher, Params: capture.params, ResultRows: capture.rows, ExpectedStatementIDs: capture.expectedIDs, ActualStatementIDs: capture.actualIDs,
			OracleRecordedAt: oracleRecordedAt.Format(time.RFC3339Nano),
		}
		if os.Getenv("ESHU_QUERY_METHODOLOGY_CALIBRATED") == "1" {
			statement.MeasuredAt = time.Now().UTC().Format(time.RFC3339Nano)
			base, candidate := methodologyPairedMeasurements(t, ctx, driver, database, capture)
			statement.Base, statement.Candidate = &base, &candidate
		} else {
			statement.NormalMilliseconds = methodologyNormalTimes(t, ctx, driver, database, capture)
			statement.Profile = methodologyProfile(t, ctx, driver, database, capture)
		}
		report.Statements = append(report.Statements, statement)
	}
	for cypher := range expected {
		if _, ok := recorder.statements[cypher]; !ok {
			t.Fatalf("registered text not executed sha256=%s", methodologyHash(cypher))
		}
	}
	if os.Getenv("ESHU_QUERY_METHODOLOGY_CALIBRATED") == "1" {
		report.Statements = append(report.Statements, methodologyParameterCases(t, ctx, driver, database, expected)...)
	}
	sort.Slice(report.Statements, func(i, j int) bool {
		if report.Statements[i].VariantID != report.Statements[j].VariantID {
			return report.Statements[i].VariantID < report.Statements[j].VariantID
		}
		return report.Statements[i].CaseID < report.Statements[j].CaseID
	})
	methodologyWriteReport(t, report)
	if os.Getenv("ESHU_QUERY_METHODOLOGY_CALIBRATED") == "1" {
		methodologyWriteGraphArtifact(t, ctx, driver, database, report)
	}
	methodologyAssertCappedCyclePage(t, ctx, driver, database)
	t.Logf("valid_requests=%d distinct_executed_texts=%d", len(requests), len(recorder.statements))
}

type methodologyReport struct {
	BackendComponents       []map[string]any             `json:"backend_components"`
	ValidRequests           int                          `json:"valid_requests"`
	RequestQueryCounts      map[string]int               `json:"request_query_counts"`
	MaxRequestQueryCount    int                          `json:"max_request_query_count"`
	RequestQueryCountBudget int                          `json:"request_query_count_budget"`
	Statements              []methodologyStatementReport `json:"statements"`
}

func methodologyRequestQueryCountWithinBudget(count, budget int) error {
	if budget <= 0 || count < 0 || count > budget {
		return fmt.Errorf("graph queries per request=%d exceeds declared budget=%d", count, budget)
	}
	return nil
}

type methodologyStatementReport struct {
	VariantID            string                   `json:"variant_id"`
	EntryID              string                   `json:"entry_id"`
	CaseID               string                   `json:"case_id"`
	ScopeMode            string                   `json:"scope_mode"`
	RequestName          string                   `json:"request_name"`
	Expected             []string                 `json:"expected"`
	Actual               []string                 `json:"actual"`
	HasMore              bool                     `json:"has_more"`
	SHA256               string                   `json:"sha256"`
	EmittedText          string                   `json:"emitted_text"`
	Params               map[string]any           `json:"params"`
	ResultRows           int                      `json:"result_rows"`
	ExpectedStatementIDs []string                 `json:"expected_statement_ids"`
	ActualStatementIDs   []string                 `json:"actual_statement_ids"`
	OracleRecordedAt     string                   `json:"oracle_recorded_at"`
	MeasuredAt           string                   `json:"measured_at"`
	NormalMilliseconds   []float64                `json:"normal_milliseconds"`
	Profile              methodologyProfileReport `json:"profile"`
	Base                 *methodologyPairedRun    `json:"base,omitempty"`
	Candidate            *methodologyPairedRun    `json:"candidate,omitempty"`
}

func methodologyScopeMode(name string) string {
	if strings.HasPrefix(name, "import-dependencies/scoped+") {
		return "scoped"
	}
	return "all_scopes"
}

type methodologyProfileReport struct {
	DbHits    int64                 `json:"db_hits"`
	Rows      int64                 `json:"rows"`
	Operators []string              `json:"operators"`
	Arguments map[string]any        `json:"arguments"`
	Alerts    []string              `json:"alerts,omitempty"`
	Plan      methodologyPlanReport `json:"plan"`
}

type methodologyPlanReport struct {
	Operator          string                  `json:"operator"`
	Arguments         map[string]any          `json:"arguments"`
	Identifiers       []string                `json:"identifiers"`
	DbHits            int64                   `json:"db_hits"`
	Rows              int64                   `json:"rows"`
	PageCacheHits     int64                   `json:"page_cache_hits"`
	PageCacheMisses   int64                   `json:"page_cache_misses"`
	PageCacheHitRatio *float64                `json:"page_cache_hit_ratio,omitempty"`
	TimeRaw           *int64                  `json:"time_raw,omitempty"`
	Children          []methodologyPlanReport `json:"children"`
}

func methodologyEntryID(name string) string {
	switch {
	case strings.HasSuffix(name, "/direct-imports"):
		return "QP-CODE-IMPORT-ROWS-REPOSITORY"
	case strings.HasSuffix(name, "/package-imports"):
		return "QP-CODE-IMPORT-PACKAGES"
	case strings.HasSuffix(name, "/source-membership"):
		return "QP-CODE-IMPORT-SOURCE-MODULE-FILES"
	case strings.HasSuffix(name, "/target-membership"):
		return "QP-CODE-IMPORT-TARGET-MODULE-FILES"
	case strings.HasSuffix(name, "/source-module-imports"):
		return "QP-CODE-IMPORT-SOURCE-MODULE-ROWS"
	case strings.HasSuffix(name, "/cross-module-calls"):
		return "QP-CODE-IMPORT-CROSS-MODULE-CALLS"
	case strings.HasSuffix(name, "/cycle-edges"):
		return "QP-CODE-IMPORT-CYCLE-EDGES"
	default:
		return ""
	}
}

func methodologyWriteReport(t *testing.T, report methodologyReport) {
	t.Helper()
	path := strings.TrimSpace(os.Getenv("ESHU_QUERY_METHODOLOGY_REPORT"))
	if path == "" {
		t.Fatal("ESHU_QUERY_METHODOLOGY_REPORT is required for live evidence")
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("captured evidence at %s sha256=%s", path, methodologyHash(string(data)))
}

func methodologyGraphDriver(t *testing.T, ctx context.Context) neo4j.DriverWithContext {
	t.Helper()
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI required")
	}
	auth := neo4j.NoAuth()
	if user := os.Getenv("ESHU_NEO4J_USERNAME"); user != "" {
		auth = neo4j.BasicAuth(user, os.Getenv("ESHU_NEO4J_PASSWORD"), "")
	}
	driver, err := neo4j.NewDriverWithContext(uri, auth)
	if err != nil {
		t.Fatal(err)
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatal(err)
	}
	return driver
}

func methodologyExecute(t *testing.T, ctx context.Context, driver neo4j.DriverWithContext, database, cypher string, params map[string]any) []map[string]any {
	t.Helper()
	session := driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: database, AccessMode: neo4j.AccessModeWrite})
	defer func() { _ = session.Close(context.Background()) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("run %s: %v", cypher, err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		t.Fatalf("collect %s: %v", cypher, err)
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		row := make(map[string]any, len(record.Keys))
		for i, key := range record.Keys {
			row[key] = record.Values[i]
		}
		rows = append(rows, row)
	}
	return rows
}

type methodologyCapturedStatement struct {
	cypher      string
	entryID     string
	params      map[string]any
	data        []map[string]any
	expectedIDs []string
	actualIDs   []string
	rows        int
	elapsed     time.Duration
	requestName string
}

type methodologyGraphReader struct {
	driver      neo4j.DriverWithContext
	database    string
	statements  map[string]methodologyCapturedStatement
	requestName string
	calls       int
}

func (r *methodologyGraphReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	r.calls++
	if r.statements == nil {
		r.statements = make(map[string]methodologyCapturedStatement)
	}
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: r.database, AccessMode: neo4j.AccessModeRead})
	defer func() { _ = session.Close(context.Background()) }()
	started := time.Now()
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
	if _, seen := r.statements[cypher]; !seen {
		copied := make(map[string]any, len(params))
		for key, value := range params {
			copied[key] = value
		}
		capturedRows := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			captured := make(map[string]any, len(row))
			for key, value := range row {
				captured[key] = value
			}
			capturedRows = append(capturedRows, captured)
		}
		r.statements[cypher] = methodologyCapturedStatement{cypher: cypher, params: copied, rows: len(rows), data: capturedRows, elapsed: time.Since(started), requestName: r.requestName}
	}
	return rows, nil
}

func (r *methodologyGraphReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func methodologyHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func methodologyProfile(t *testing.T, ctx context.Context, driver neo4j.DriverWithContext, database string, capture methodologyCapturedStatement) methodologyProfileReport {
	t.Helper()
	session := driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: database, AccessMode: neo4j.AccessModeRead})
	defer func() { _ = session.Close(context.Background()) }()
	result, err := session.Run(ctx, "PROFILE "+capture.cypher, capture.params)
	if err != nil {
		t.Fatalf("PROFILE %s: %v", methodologyHash(capture.cypher), err)
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		t.Fatalf("consume PROFILE %s: %v", methodologyHash(capture.cypher), err)
	}
	profile := summary.Profile()
	if profile == nil {
		t.Fatalf("PROFILE %s has no plan", methodologyHash(capture.cypher))
	}
	if err := methodologyValidateProfileCounters(profile, capture.rows); err != nil {
		t.Fatalf("PROFILE %s: %v", methodologyHash(capture.cypher), err)
	}
	if found := queryplanUnboundedVarLengthOperators(profile); len(found) != 0 {
		t.Fatalf("unbounded PROFILE %s: %v", methodologyHash(capture.cypher), found)
	}
	operators := profiledPlanOperators(profile)
	alerts := make([]string, 0)
	for _, op := range operators {
		if op == "AllNodesScan" || op == "CartesianProduct" {
			alerts = append(alerts, op)
		}
	}
	return methodologyProfileReport{DbHits: methodologyPlanHits(profile), Rows: profile.Records(), Operators: operators, Arguments: profile.Arguments(), Alerts: alerts, Plan: methodologyPlanTree(profile)}
}

func methodologyNormalTimes(t *testing.T, ctx context.Context, driver neo4j.DriverWithContext, database string, capture methodologyCapturedStatement) []float64 {
	t.Helper()
	times := []float64{float64(capture.elapsed.Microseconds()) / 1000}
	for range 2 {
		started := time.Now()
		rows := methodologyExecute(t, ctx, driver, database, capture.cypher, capture.params)
		if ids := methodologyStatementIDs(capture.entryID, rows); !slices.Equal(ids, capture.expectedIDs) {
			t.Fatalf("repeat %s IDs=%v, independent fixture wants %v", methodologyHash(capture.cypher), ids, capture.expectedIDs)
		}
		times = append(times, float64(time.Since(started).Microseconds())/1000)
	}
	return times
}
