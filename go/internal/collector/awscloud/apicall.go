package awscloud

import (
	"context"
	"strings"
	"sync"
)

type apiCallRecorderContextKey struct{}

// APICallEvent is one bounded AWS SDK call observation. Resource identifiers,
// tags, ARNs, page tokens, and raw error payloads must stay out of this shape.
type APICallEvent struct {
	Boundary  Boundary
	Operation string
	Result    string
	Throttled bool
}

// APICallStats summarizes AWS API calls observed during one claimed scan.
type APICallStats struct {
	APICallCount    int
	ThrottleCount   int
	OperationCounts map[string]int
}

// APICallRecorder receives bounded AWS API call events from service adapters.
type APICallRecorder interface {
	RecordAPICall(context.Context, APICallEvent)
}

// APICallStatsRecorder accumulates per-claim AWS API call counts in memory so
// the runtime can persist one status update instead of writing per API call.
type APICallStatsRecorder struct {
	mu       sync.Mutex
	boundary Boundary
	stats    APICallStats
}

// NewAPICallStatsRecorder creates a recorder for one AWS claim boundary.
func NewAPICallStatsRecorder(boundary Boundary) *APICallStatsRecorder {
	return &APICallStatsRecorder{
		boundary: boundary,
		stats: APICallStats{
			OperationCounts: map[string]int{},
		},
	}
}

// ContextWithAPICallRecorder attaches a recorder to ctx for service adapters.
func ContextWithAPICallRecorder(ctx context.Context, recorder APICallRecorder) context.Context {
	if recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, apiCallRecorderContextKey{}, recorder)
}

// RecordAPICall forwards an AWS API call event to the recorder on ctx, if any.
func RecordAPICall(ctx context.Context, event APICallEvent) {
	recorder, ok := ctx.Value(apiCallRecorderContextKey{}).(APICallRecorder)
	if !ok || recorder == nil {
		return
	}
	recorder.RecordAPICall(ctx, event)
}

// RecordAPICall records one bounded API call event.
func (r *APICallStatsRecorder) RecordAPICall(_ context.Context, event APICallEvent) {
	if r == nil {
		return
	}
	if !sameAPICallBoundary(r.boundary, event.Boundary) {
		return
	}
	operation := strings.TrimSpace(event.Operation)
	if operation == "" {
		operation = "unknown"
	}
	result := strings.TrimSpace(event.Result)
	if result == "" {
		result = "unknown"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stats.APICallCount++
	if event.Throttled {
		r.stats.ThrottleCount++
	}
	r.stats.OperationCounts[operation+":"+result]++
}

// Snapshot returns a copy of the accumulated API call counts.
func (r *APICallStatsRecorder) Snapshot() APICallStats {
	if r == nil {
		return APICallStats{OperationCounts: map[string]int{}}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	operationCounts := make(map[string]int, len(r.stats.OperationCounts))
	for key, count := range r.stats.OperationCounts {
		operationCounts[key] = count
	}
	return APICallStats{
		APICallCount:    r.stats.APICallCount,
		ThrottleCount:   r.stats.ThrottleCount,
		OperationCounts: operationCounts,
	}
}

func sameAPICallBoundary(left Boundary, right Boundary) bool {
	return strings.TrimSpace(left.CollectorInstanceID) == strings.TrimSpace(right.CollectorInstanceID) &&
		strings.TrimSpace(left.AccountID) == strings.TrimSpace(right.AccountID) &&
		strings.TrimSpace(left.Region) == strings.TrimSpace(right.Region) &&
		strings.TrimSpace(left.ServiceKind) == strings.TrimSpace(right.ServiceKind) &&
		strings.TrimSpace(left.GenerationID) == strings.TrimSpace(right.GenerationID) &&
		left.FencingToken == right.FencingToken
}
