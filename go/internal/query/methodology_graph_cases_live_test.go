// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build queryplan_profile_live

package query

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/imports"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4j "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// methodologyParameterCases keeps a populated graph while proving that a
// missing anchor and a page past the end emit the registered production text
// and return empty physical rows. Each case gets its own paired measurements.
func methodologyParameterCases(t *testing.T, ctx context.Context, driver neo4j.DriverWithContext, database string, registered map[string]string) []methodologyStatementReport {
	t.Helper()
	cases := []struct {
		caseID  string
		request codemodel.ImportDependencyRequest
	}{
		{
			caseID: "missing-source-file",
			request: codemodel.ImportDependencyRequest{
				QueryType: "imports_by_file", SourceFile: "src/missing.py", Limit: 10,
				Access: queryplanScopedRepositoryAccess(),
			},
		},
		{
			caseID: "empty-page",
			request: codemodel.ImportDependencyRequest{
				QueryType: "imports_by_file", RepoID: "proof-repository", Limit: 10, Offset: 10000,
				Access: querycontract.RepositoryAccessFilter{AllScopes: true},
			},
		},
	}
	result := make([]methodologyStatementReport, 0, len(cases))
	for _, tc := range cases {
		if err := tc.request.Validate(); err != nil {
			t.Fatal(err)
		}
		reader := &methodologyGraphReader{driver: driver, database: database, requestName: importDependencyQueryplanRequestName(tc.request)}
		rows, enumeration, err := imports.Rows(ctx, reader, tc.request)
		if err != nil {
			t.Fatal(err)
		}
		response := codemodel.ImportDependencyResponseWithCycleEnumeration(tc.request, rows, enumeration)
		wantPublic, wantMore := methodologyExpectedIdentityPage(tc.request)
		gotPublic := methodologyResponseIdentities(t, tc.request, response)
		if !slices.Equal(gotPublic, wantPublic) || response["has_more"] != wantMore {
			t.Fatalf("%s public result=%v has_more=%v, want %v %v", tc.caseID, gotPublic, response["has_more"], wantPublic, wantMore)
		}
		if len(reader.statements) != 1 {
			t.Fatalf("%s emitted %d statements, want 1", tc.caseID, len(reader.statements))
		}
		for cypher, capture := range reader.statements {
			variant, ok := registered[cypher]
			if !ok {
				t.Fatalf("%s emitted unregistered SHA %s", tc.caseID, methodologyHash(cypher))
			}
			capture.entryID = methodologyEntryID(variant)
			capture.expectedIDs = methodologyOracleStatementIDs(capture.entryID, capture.params)
			capture.actualIDs = methodologyStatementIDs(capture.entryID, capture.data)
			if !slices.Equal(capture.expectedIDs, capture.actualIDs) || len(capture.expectedIDs) != 0 {
				t.Fatalf("%s physical IDs=%v, independent fixture wants empty %v", tc.caseID, capture.actualIDs, capture.expectedIDs)
			}
			oracleAt := time.Now().UTC().Format(time.RFC3339Nano)
			measuredAt := time.Now().UTC().Format(time.RFC3339Nano)
			base, candidate := methodologyPairedMeasurements(t, ctx, driver, database, capture)
			result = append(result, methodologyStatementReport{
				VariantID: variant, EntryID: capture.entryID, CaseID: tc.caseID, ScopeMode: methodologyScopeMode(variant),
				RequestName: reader.requestName, Expected: wantPublic, Actual: gotPublic, HasMore: wantMore,
				SHA256: methodologyHash(cypher), EmittedText: cypher, Params: capture.params, ResultRows: capture.rows,
				ExpectedStatementIDs: capture.expectedIDs, ActualStatementIDs: capture.actualIDs,
				OracleRecordedAt: oracleAt, MeasuredAt: measuredAt, Base: &base, Candidate: &candidate,
			})
		}
	}
	return result
}
