// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package intake

import (
	"net/http"
	"testing"
)

const (
	metricAWSFreshnessEvents = "eshu_dp_aws_freshness_events_total"
	metricGCPFreshnessEvents = "eshu_dp_gcp_freshness_events_total"
)

// freshnessRoute describes one freshness intake route: the counter it feeds,
// the exact label set a point on that counter must carry, and the hooks that
// shape its fake store.
type freshnessRoute struct {
	name      string
	route     int
	metric    string
	other     string
	kind      string
	labels    func(action string) map[string]string
	duplicate func(*testStoreSet, int)
	noStore   func(*testStoreSet) Stores
}

func freshnessRoutes() []freshnessRoute {
	routes := intakeRoutes()
	return []freshnessRoute{
		{
			name: "aws", route: 2, metric: metricAWSFreshnessEvents, other: metricGCPFreshnessEvents,
			kind: routes[2].eventKind,
			labels: func(action string) map[string]string {
				return map[string]string{"kind": routes[2].eventKind, "action": action}
			},
			duplicate: func(s *testStoreSet, n int) { s.aws.duplicateCount = n },
			noStore: func(s *testStoreSet) Stores {
				stores := s.stores()
				stores.AWS = nil
				return stores
			},
		},
		{
			name: "gcp", route: 3, metric: metricGCPFreshnessEvents, other: metricAWSFreshnessEvents,
			kind: routes[3].eventKind,
			labels: func(action string) map[string]string {
				return map[string]string{"kind": routes[3].eventKind, "action": action, "auth_path": "n/a"}
			},
			duplicate: func(s *testStoreSet, n int) { s.gcp.duplicateCount = n },
			noStore: func(s *testStoreSet) Stores {
				stores := s.stores()
				stores.GCP = nil
				return stores
			},
		},
	}
}

// TestIntakeFreshnessActionsFollowTheStoreOutcome is the #9814 red/green pair.
// The intake host must keep the stored, coalesced and failed freshness actions
// on eshu_dp_{aws,gcp}_freshness_events_total observable after the in-tree
// listener is removed. The counter lookups match the label set exactly, so a
// missing or extra label (the GCP auth_path) is a different series.
func TestIntakeFreshnessActionsFollowTheStoreOutcome(t *testing.T) {
	t.Parallel()

	for _, fr := range freshnessRoutes() {
		route := intakeRoutes()[fr.route]
		for _, tc := range []struct {
			name       string
			arrange    func(*testStoreSet) Stores
			wantStatus int
			wantAction string
		}{
			{"stored", func(s *testStoreSet) Stores { return s.stores() }, http.StatusAccepted, "intake_stored"},
			{"coalesced", func(s *testStoreSet) Stores { fr.duplicate(s, 2); return s.stores() }, http.StatusAccepted, "intake_coalesced"},
			{"store_failure", func(s *testStoreSet) Stores { route.failStore(s); return s.stores() }, http.StatusInternalServerError, "intake_failed"},
			{"store_unconfigured", fr.noStore, http.StatusServiceUnavailable, "intake_failed"},
		} {
			t.Run(fr.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				harness := newMetricsHarness(t)
				mux := harness.server(tc.arrange(testStores()), 0).Mux()

				rec := postIntake(t, mux, route.path, route.payload)
				if rec.Code != tc.wantStatus {
					t.Fatalf("intake = %d (%s), want %d", rec.Code, rec.Body.String(), tc.wantStatus)
				}

				for _, action := range []string{"intake_stored", "intake_coalesced", "intake_failed"} {
					want := int64(0)
					if action == tc.wantAction {
						want = 1
					}
					if got := harness.counter(t, fr.metric, fr.labels(action)); got != want {
						t.Fatalf("%s{action=%s} = %d, want %d", fr.metric, action, got, want)
					}
				}
				if got := harness.seriesCount(t, fr.metric); got != 1 {
					t.Fatalf("%s series = %d, want exactly 1", fr.metric, got)
				}
				if got := harness.seriesCount(t, fr.other); got != 0 {
					t.Fatalf("%s series = %d, want 0 on the other provider's route", fr.other, got)
				}
			})
		}
	}
}

// TestIntakeFreshnessRejectionsEmitNoActions pins the #9814 ruling: the host
// never emits intake_rejected or intake_ignored. A rejection here is a
// satellite-to-core fault already counted on eshu_dp_webhook_requests_total;
// labelling it intake_rejected would send an operator to provider auth.
func TestIntakeFreshnessRejectionsEmitNoActions(t *testing.T) {
	t.Parallel()

	for _, fr := range freshnessRoutes() {
		route := intakeRoutes()[fr.route]
		invalid := cloneRoute(route.payload)
		invalid["kind"] = "bogus"
		for _, tc := range []struct {
			name string
			auth string
			body []byte
		}{
			{"wrong_bearer", "Bearer nope", marshalRoute(t, route.payload)},
			{"invalid_kind", "Bearer core-intake-token", marshalRoute(t, invalid)},
			{"invalid_json", "Bearer core-intake-token", []byte("{not json")},
		} {
			t.Run(fr.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				harness := newMetricsHarness(t)
				mux := harness.server(testStores().stores(), 0).Mux()

				rec := rawIntake(mux, http.MethodPost, route.path, tc.auth, tc.body)
				if rec.Code < 400 || rec.Code >= 500 {
					t.Fatalf("rejection = %d, want 4xx", rec.Code)
				}
				for _, name := range []string{metricAWSFreshnessEvents, metricGCPFreshnessEvents} {
					if got := harness.seriesCount(t, name); got != 0 {
						t.Fatalf("%s series = %d, want 0 for a rejected request", name, got)
					}
				}
			})
		}
	}
}

// TestIntakeOtherRoutesEmitNoFreshnessActions pins that only the AWS and GCP
// routes feed the freshness counters.
func TestIntakeOtherRoutesEmitNoFreshnessActions(t *testing.T) {
	t.Parallel()

	for _, idx := range []int{0, 1, 4} {
		route := intakeRoutes()[idx]
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()
			harness := newMetricsHarness(t)
			mux := harness.server(testStores().stores(), 0).Mux()
			if rec := postIntake(t, mux, route.path, route.payload); rec.Code != http.StatusAccepted {
				t.Fatalf("intake = %d (%s), want 202", rec.Code, rec.Body.String())
			}
			for _, name := range []string{metricAWSFreshnessEvents, metricGCPFreshnessEvents} {
				if got := harness.seriesCount(t, name); got != 0 {
					t.Fatalf("%s series = %d, want 0", name, got)
				}
			}
		})
	}
	t.Run("healthz", func(t *testing.T) {
		t.Parallel()
		harness := newMetricsHarness(t)
		mux := harness.server(testStores().stores(), 0).Mux()
		if rec := rawIntake(mux, http.MethodGet, "/healthz", "", nil); rec.Code != http.StatusOK {
			t.Fatalf("healthz = %d, want 200", rec.Code)
		}
		for _, name := range []string{metricAWSFreshnessEvents, metricGCPFreshnessEvents} {
			if got := harness.seriesCount(t, name); got != 0 {
				t.Fatalf("%s series = %d, want 0", name, got)
			}
		}
	})
}
