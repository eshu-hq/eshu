// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package compare

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

const (
	compareEnvironmentsPath = "/api/v0/compare/environments"
	compareEnvironmentsBody = `{"workload_id":"workload:svc","left":"qa","right":"prod"}`
)

// failingCompareStep names the one read a compareFailureHandler fails.
type failingCompareStep int

const (
	failWorkloadLookup failingCompareStep = iota + 1
	failServiceEvidence
	failLeftSnapshot
	failRightSnapshot
)

// compareFailureHandler builds a Handler whose read for step returns err and
// whose every other read succeeds, so the route reaches exactly that step's
// failure branch.
func compareFailureHandler(step failingCompareStep, err error) failure.Mounter {
	return &Handler{
		Profile: querycontract.ProfileProduction,
		Neo4j: fakeCompareGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				switch {
				case strings.Contains(cypher, "MATCH (w:Workload)"):
					if step == failWorkloadLookup {
						return nil, err
					}
					return map[string]any{"id": "workload:svc", "name": "svc", "kind": "service", "repo_id": "repo-svc"}, nil
				case strings.Contains(cypher, "MATCH (i:WorkloadInstance)"):
					environment, _ := params["environment"].(string)
					if (step == failLeftSnapshot && environment == "qa") || (step == failRightSnapshot && environment == "prod") {
						return nil, err
					}
					return map[string]any{"id": "instance:" + environment, "environment": environment, "workload_id": "workload:svc"}, nil
				}
				return nil, nil
			},
		},
		Content: fakeCompareEvidenceReader{err: func() error {
			if step == failServiceEvidence {
				return err
			}
			return nil
		}()},
	}
}

// fakeCompareEvidenceReader is the service evidence reader the comparison
// loads before it reads either environment; err fails its file list.
type fakeCompareEvidenceReader struct{ err error }

func (f fakeCompareEvidenceReader) ListRepoFiles(context.Context, string, int) ([]querycontract.FileContent, error) {
	return nil, f.err
}

func (f fakeCompareEvidenceReader) GetFileContent(context.Context, string, string) (*querycontract.FileContent, error) {
	return nil, f.err
}

// TestCompareEnvironmentsAnswersFixedServerFailures is the #7674 regression
// for POST /api/v0/compare/environments. Each failed read answered 500 with
// the backend error text through the package's own writer, which the error
// text guard could not see, and a client cancel answered 500 too. The route
// opens no handler span, so the harness reads the failure off the request
// span. The service evidence read goes through the guarded content reader,
// so it answers the reader fence like the three graph reads.
func TestCompareEnvironmentsAnswersFixedServerFailures(t *testing.T) {
	t.Parallel()
	route := func(name string, step failingCompareStep, message string) failure.Route {
		return failure.Route{
			Name: name, Method: http.MethodPost, Path: compareEnvironmentsPath, Body: compareEnvironmentsBody,
			Message: message,
			Handler: func(err error) failure.Mounter { return compareFailureHandler(step, err) },
		}
	}
	failure.Run(t, failure.Suite{Routes: []failure.Route{
		route("workload lookup", failWorkloadLookup, compareWorkloadLookupFailedMessage),
		route("service evidence", failServiceEvidence, compareServiceEvidenceFailedMessage),
		route("left snapshot", failLeftSnapshot, compareLeftSnapshotFailedMessage),
		route("right snapshot", failRightSnapshot, compareRightSnapshotFailedMessage),
	}})
}

// TestCompareEnvironmentsValidationStays400 pins the request-validation
// answers the conversion leaves alone: a malformed body echoes only the
// decoder's view of the caller's own input, and a missing field answers its
// fixed text.
func TestCompareEnvironmentsValidationStays400(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body, want string
	}{
		{name: "malformed body", body: `{"workload_id":`, want: "invalid JSON: unexpected EOF"},
		{name: "missing workload", body: `{"left":"qa","right":"prod"}`, want: "workload_id is required"},
		{name: "missing left", body: `{"workload_id":"w","right":"prod"}`, want: "left environment is required"},
		{name: "missing right", body: `{"workload_id":"w","left":"qa"}`, want: "right environment is required"},
	} {
		rec, _ := failure.Serve(failure.Suite{},
			failure.Route{Method: http.MethodPost, Path: compareEnvironmentsPath, Body: tc.body},
			compareFailureHandler(0, nil), false)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400; body = %s", tc.name, rec.Code, rec.Body.String())
			continue
		}
		if got := failure.Message(rec.Body.Bytes()); got != tc.want {
			t.Errorf("%s: body message = %q, want %q", tc.name, got, tc.want)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
			t.Errorf("%s: Content-Type = %q", tc.name, got)
		}
	}
}
