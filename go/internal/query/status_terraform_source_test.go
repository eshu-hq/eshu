// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// terraformSourcedSnapshot reports a terraform_state section served from a
// stored row 12 seconds old and an active-work section that came from the live
// statement, so the two markers can be told apart on a route.
func terraformSourcedSnapshot() statuspkg.RawSnapshot {
	asOf := time.Date(2026, 10, 7, 9, 30, 0, 0, time.UTC)
	return statuspkg.RawSnapshot{
		AsOf: asOf,
		ActiveWorkSource: statuspkg.ActiveWorkSource{
			Source: statuspkg.ActiveWorkSourceLive, Reason: statuspkg.ActiveWorkReasonFlagOff, AsOf: asOf,
		},
		TerraformStateSource: statuspkg.ActiveWorkSource{
			Source: statuspkg.ActiveWorkSourceModel, Reason: statuspkg.ActiveWorkReasonFresh,
			AsOf: asOf.Add(-12 * time.Second), Age: 12 * time.Second,
		},
	}
}

// terraformSourceRoutes are the routes that render Report.TerraformState and so
// carry terraform_state_source (the index alias is the MCP get_index_status
// proxy target).
var terraformSourceRoutes = []string{
	"/api/v0/status/pipeline",
	"/api/v0/status/index",
	"/api/v0/index-status",
}

func TestStatusRoutesCarryTheTerraformStateSource(t *testing.T) {
	t.Parallel()

	for _, path := range terraformSourceRoutes {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			payload := getStatusPayload(t, terraformSourcedSnapshot(), path)
			source, ok := payload["terraform_state_source"].(map[string]any)
			if !ok {
				t.Fatalf("GET %s has no terraform_state_source object: %#v", path, payload)
			}
			if source["source"] != "model" || source["reason"] != "fresh" || source["stale"] != false ||
				source["as_of"] != "2026-10-07T09:29:48Z" || source["age_seconds"] != float64(12) || len(source) != 5 {
				t.Fatalf("GET %s terraform_state_source = %#v", path, source)
			}
			// The two markers are independent: the active-work section came
			// from the live statement here.
			if active, _ := payload["active_work_source"].(map[string]any); active["source"] != "live" {
				t.Fatalf("GET %s active_work_source = %#v, want it to stay live", path, payload["active_work_source"])
			}
		})
	}
}

// TestStatusRoutesEmitTheFlagOffTerraformStateSource: with the reader off the
// marker is still present on the three routes, as live/flag_off at the
// snapshot clock.
func TestStatusRoutesEmitTheFlagOffTerraformStateSource(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 10, 7, 9, 30, 0, 0, time.UTC)
	snapshot := statuspkg.RawSnapshot{AsOf: asOf, TerraformStateSource: statuspkg.ActiveWorkSource{
		Source: statuspkg.ActiveWorkSourceLive, Reason: statuspkg.ActiveWorkReasonFlagOff, AsOf: asOf,
	}}
	for _, path := range terraformSourceRoutes {
		source, _ := getStatusPayload(t, snapshot, path)["terraform_state_source"].(map[string]any)
		if source["source"] != "live" || source["reason"] != "flag_off" || source["as_of"] != "2026-10-07T09:30:00Z" ||
			source["age_seconds"] != float64(0) || source["stale"] != false {
			t.Fatalf("GET %s flag-off terraform_state_source = %#v", path, source)
		}
	}
}

func TestStatusRoutesOmitTheTerraformStateSourceWhenTheReaderReportsNone(t *testing.T) {
	t.Parallel()

	for _, path := range terraformSourceRoutes {
		payload := getStatusPayload(t, statuspkg.RawSnapshot{AsOf: time.Date(2026, 10, 7, 9, 30, 0, 0, time.UTC)}, path)
		if _, present := payload["terraform_state_source"]; present {
			t.Fatalf("GET %s emitted terraform_state_source for a reader that reports none", path)
		}
	}
}

// TestRoutesThatSkipTerraformNeverCarryTheTerraformStateSource enumerates every
// status route that reads the snapshot with Terraform evidence skipped, or does
// not render the section, and requires the marker to be absent even when the
// snapshot carries one. A route that copied the field without rendering the
// section would tell an operator about evidence it never read.
func TestRoutesThatSkipTerraformNeverCarryTheTerraformStateSource(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v0/status/ingesters",
		"/api/v0/ingesters",
		"/api/v0/status/ingesters/repository",
		"/api/v0/ingesters/repository",
		"/api/v0/status/operations",
		"/api/v0/status/hosted-readiness",
		"/api/v0/status/collectors",
		"/api/v0/collectors",
		"/api/v0/status/collector-readiness",
		"/api/v0/collector-readiness",
		"/api/v0/status/operator-control-plane",
		"/api/v0/status/freshness-causality",
		"/api/v0/status/governance",
		"/api/v0/status/semantic-extraction",
		"/api/v0/status/answer-narration",
	} {
		handler := &StatusHandler{StatusReader: &selectionRecordingReader{snapshot: terraformSourcedSnapshot()}, LiveActivity: &fakeLiveActivityReader{}}
		mux := http.NewServeMux()
		handler.Mount(mux)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d: %s (the route must answer for the omission to mean anything)", path, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "terraform_state_source") {
			t.Fatalf("GET %s carries terraform_state_source although it skips Terraform evidence: %s", path, rec.Body.String())
		}
	}
}

// TestLiveEvidenceBundleNeverCarriesTheTerraformStateSource: the live bundle
// reads the report with Terraform evidence skipped and composes a
// LiveSnapshot that has no field for a Terraform-state marker, so it cannot
// carry the key even when the report does. (It does carry active_work_source
// via LiveSnapshot.ActiveWorkSource since #7660.) The route is not a
// StatusHandler route, so it has its own check.
func TestLiveEvidenceBundleNeverCarriesTheTerraformStateSource(t *testing.T) {
	t.Parallel()

	handler := &EvidenceHandler{
		StatusReader: fakeStatusReader{snapshot: terraformSourcedSnapshot()},
		Neo4j:        evidenceBundleFixtureGraph{count: 5},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0/evidence/bundle", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v0/evidence/bundle status = %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "terraform_state_source") {
		t.Fatalf("the live evidence bundle carries terraform_state_source: %s", rec.Body.String())
	}
}

// TestIndexStatusWithheldSectionsExcludeTheTerraformStateSource: the marker is
// not a section. A scoped caller's body never reads the snapshot, so it carries
// no marker, and the withheld list the scoped body discloses must not name one.
func TestIndexStatusWithheldSectionsExcludeTheTerraformStateSource(t *testing.T) {
	t.Parallel()

	if _, present := getStatusPayload(t, terraformSourcedSnapshot(), "/api/v0/status/index")["terraform_state_source"]; !present {
		t.Fatal("the unscoped index status lost terraform_state_source")
	}
	for _, section := range indexStatusWithheldSections {
		if section == "terraform_state_source" {
			t.Fatal("terraform_state_source is a marker, not a withheld section: it must stay out of indexStatusWithheldSections")
		}
	}
}
