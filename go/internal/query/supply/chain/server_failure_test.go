// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

const (
	stageFailedEvent   = "supply_chain_query.stage_failed"
	stageCanceledEvent = "supply_chain_query.stage_canceled"
)

// supplyFailureSuite wraps routes in a suite on the package handler tracer.
func supplyFailureSuite(routes ...failure.Route) failure.Suite {
	return failure.Suite{HandlerTracer: &queryHandlerTracer, Routes: routes}
}

// runSupplyFailureRoutes drives every route through every failure case. It
// swaps the package-level queryHandlerTracer, so callers must not be parallel.
func runSupplyFailureRoutes(t *testing.T, routes []failure.Route) {
	t.Helper()
	failure.Run(t, supplyFailureSuite(routes...))
}

// stagedSupplyRoute builds the failure route for one handler-owned step: the
// fixed message answers as a plain detail body, and the step's stage-log
// verdict is checked from a per-case log capture the handler constructor
// resets on every case.
func stagedSupplyRoute(
	t *testing.T,
	name, method, target, body, message, operation, stage string,
	build func(err error) *Handler,
) failure.Route {
	t.Helper()
	var logBuf bytes.Buffer
	return failure.Route{
		Name: name, Method: method, Path: target, Body: body,
		Message: message, PlainDetail: true,
		Handler: func(err error) failure.Mounter {
			logBuf.Reset()
			handler := build(err)
			handler.Logger = slog.New(slog.NewJSONHandler(&logBuf, nil))
			return handler
		},
		Stage: &failure.StageLog{
			Operation: operation, Stage: stage,
			Logs: func() ([]map[string]any, []map[string]any) {
				records := decodeLogRecords(t, &logBuf)
				return recordsWithEvent(records, stageFailedEvent), recordsWithEvent(records, stageCanceledEvent)
			},
		},
	}
}

// detailEquals reports whether body is a plain error whose detail is want.
func detailEquals(body []byte, want string) bool {
	var plain struct {
		Detail string `json:"detail"`
	}
	return json.Unmarshal(body, &plain) == nil && plain.Detail == want
}

// assertFaultSpan checks the handler span of a server fault: Error status with
// the route's fixed message as its description and exactly one exception
// event, so no second marker (such as the removed failStage) records it again.
func assertFaultSpan(t *testing.T, span sdktrace.ReadOnlySpan, message string) {
	t.Helper()
	exceptions := 0
	for _, event := range span.Events() {
		if event.Name == "exception" {
			exceptions++
		}
	}
	status := span.Status()
	if status.Code != codes.Error || status.Description != message || exceptions != 1 {
		t.Fatalf("handler span status = %v (%q), exceptions = %d; want Error (%q), 1",
			status.Code, status.Description, exceptions, message)
	}
}

// supplyHandlerSpan returns the ended route handler span named spanName.
func supplyHandlerSpan(t *testing.T, ended []sdktrace.ReadOnlySpan, spanName string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range ended {
		if span.Name() == spanName {
			return span
		}
	}
	t.Fatalf("no ended %s span", spanName)
	return nil
}
