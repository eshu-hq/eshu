// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package claim

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	workflowv1 "github.com/eshu-hq/eshu-workflow/workflowv1"
)

// recordedRequest captures one inbound fake-service call for assertions
// about wire hygiene: version header, authorization presence, and that no
// request was issued at all on locally-rejected input.
type recordedRequest struct {
	operation  workflowv1.Operation
	version    string
	authHeader string
	body       []byte
}

// fakeWorkflow is the test-double plane: a scripted stand-in for the
// eshu-workflow service implementing the merged workflowv1 RouteTable. Each
// test programs per-operation responders and then replays hostile inputs
// through the real Client against it.
type fakeWorkflow struct {
	t        *testing.T
	mu       sync.Mutex
	respond  map[workflowv1.Operation]func(*http.Request) (int, string)
	requests []recordedRequest
	server   *httptest.Server
}

func newFakeWorkflow(t *testing.T) *fakeWorkflow {
	t.Helper()
	fake := &fakeWorkflow{
		t:       t,
		respond: map[workflowv1.Operation]func(*http.Request) (int, string){},
	}
	mux := http.NewServeMux()
	for _, route := range workflowv1.RouteTable {
		route := route
		mux.HandleFunc(route.Path, func(w http.ResponseWriter, r *http.Request) {
			fake.mu.Lock()
			responder := fake.respond[route.Operation]
			fake.mu.Unlock()
			var body []byte
			if r.Body != nil {
				body, _ = io.ReadAll(io.LimitReader(r.Body, 1<<20))
			}
			fake.mu.Lock()
			fake.requests = append(fake.requests, recordedRequest{
				operation:  route.Operation,
				version:    r.Header.Get(workflowv1.VersionHeader),
				authHeader: r.Header.Get("Authorization"),
				body:       body,
			})
			fake.mu.Unlock()
			if responder == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			status, payload := responder(r)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			if payload != "" {
				_, _ = w.Write([]byte(payload))
			}
		})
	}
	fake.server = httptest.NewServer(mux)
	t.Cleanup(fake.server.Close)
	return fake
}

// on programs the responder for one operation.
func (f *fakeWorkflow) on(op workflowv1.Operation, responder func(*http.Request) (int, string)) {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.respond[op] = responder
}

// okJSON answers 200 with the JSON encoding of value.
func (f *fakeWorkflow) okJSON(op workflowv1.Operation, payload string) {
	f.t.Helper()
	f.on(op, func(*http.Request) (int, string) { return http.StatusOK, payload })
}

// fail answers with a protocol ErrorEnvelope body.
func (f *fakeWorkflow) fail(op workflowv1.Operation, status int, code, message string) {
	f.t.Helper()
	f.on(op, func(*http.Request) (int, string) {
		return status, `{"code":` + strconv.Quote(code) + `,"message":` + strconv.Quote(message) + `}`
	})
}

// hang blocks the responder until the request context ends, so cancellation
// replays can prove the client honors ctx without a live server reply.
func (f *fakeWorkflow) hang(op workflowv1.Operation) {
	f.t.Helper()
	f.on(op, func(r *http.Request) (int, string) {
		<-r.Context().Done()
		return http.StatusTeapot, ``
	})
}

// calls counts recorded requests for one operation.
func (f *fakeWorkflow) calls(op workflowv1.Operation) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, req := range f.requests {
		if req.operation == op {
			count++
		}
	}
	return count
}

// totalCalls counts every recorded request on any operation.
func (f *fakeWorkflow) totalCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}
