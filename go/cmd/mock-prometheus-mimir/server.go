// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

const (
	healthEndpoint   = "/health"
	rangeEndpoint    = "/api/v1/query_range"
	fixtureTenantID  = "golden-corpus"
	fixtureStep      = "5m"
	queueDepthPromQL = `sum(eshu_dp_queue_depth{status=~"pending|in_flight|retrying"})`
)

type rangeResponse struct {
	Status string    `json:"status"`
	Data   rangeData `json:"data"`
}

type rangeData struct {
	ResultType string        `json:"resultType"`
	Result     []rangeResult `json:"result"`
}

type rangeResult struct {
	Metric map[string]string `json:"metric"`
	Values [][]any           `json:"values"`
}

// newHandler returns the strict Prometheus-compatible surface used by the
// golden-corpus proof. The fixed closed query prevents this fixture from
// becoming a general-purpose PromQL evaluator.
func newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+healthEndpoint, handleHealth)
	mux.HandleFunc("GET "+rangeEndpoint, handleRangeQuery)
	return mux
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func handleRangeQuery(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Scope-OrgID") != fixtureTenantID {
		http.Error(w, "synthetic tenant header is required", http.StatusUnauthorized)
		return
	}
	start, end, ok := validateRangeQuery(r.URL.Query())
	if !ok {
		http.Error(w, "expected the bounded golden queue-depth range query", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, rangeResponse{
		Status: "success",
		Data: rangeData{
			ResultType: "matrix",
			Result: []rangeResult{{
				Metric: map[string]string{"fixture": "golden-prometheus-range"},
				Values: [][]any{
					{float64(start.Unix()), "2"},
					{float64(end.Unix()), "0"},
				},
			}},
		},
	})
}

func validateRangeQuery(query url.Values) (time.Time, time.Time, bool) {
	if len(query) != 4 || len(query["query"]) != 1 || len(query["start"]) != 1 ||
		len(query["end"]) != 1 || len(query["step"]) != 1 {
		return time.Time{}, time.Time{}, false
	}
	if query.Get("query") != queueDepthPromQL || query.Get("step") != fixtureStep {
		return time.Time{}, time.Time{}, false
	}
	start, err := time.Parse(time.RFC3339, query.Get("start"))
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	end, err := time.Parse(time.RFC3339, query.Get("end"))
	if err != nil || end.Sub(start) != time.Hour {
		return time.Time{}, time.Time{}, false
	}
	return start.UTC(), end.UTC(), true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
