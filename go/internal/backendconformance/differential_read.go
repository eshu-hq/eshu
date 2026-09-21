// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import "context"

// differentialQueryRecorder decorates a GraphQuery with differential capture.
type differentialQueryRecorder struct {
	inner    GraphQuery
	recorder *DifferentialRecorder
	backend  string
}

// WrapGraphQuery returns inner unchanged when capture is disabled or the
// recorder is nil; otherwise it records every Run and RunSingle with its
// row digest. Errors propagate and record a failed entry.
func WrapGraphQuery(inner GraphQuery, recorder *DifferentialRecorder, backend string) GraphQuery {
	if inner == nil || recorder == nil || !CaptureEnabled() {
		return inner
	}
	return differentialQueryRecorder{inner: inner, recorder: recorder, backend: backend}
}

func (q differentialQueryRecorder) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	return q.recorded(cypher, params, func() ([]map[string]any, error) {
		return q.inner.Run(ctx, cypher, params)
	})
}

func (q differentialQueryRecorder) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	return q.recordedSingle(cypher, params, func() (map[string]any, error) {
		return q.inner.RunSingle(ctx, cypher, params)
	})
}

// recorded captures one read execution and passes the inner result through
// untouched. The interface methods return this helper's call result
// directly (rather than a held err variable) to keep the decorator
// transparent and satisfy the repo's wrapcheck rule the same way the
// backpressure wrapper's direct returns do.
func (q differentialQueryRecorder) recorded(cypher string, params map[string]any, run func() ([]map[string]any, error)) ([]map[string]any, error) {
	rows, err := run()
	q.recorder.Add(captureRead(cypher, params, rows, err, q.backend))
	return rows, err
}

func (q differentialQueryRecorder) recordedSingle(cypher string, params map[string]any, run func() (map[string]any, error)) (map[string]any, error) {
	row, err := run()
	var rows []map[string]any
	if err == nil && row != nil {
		rows = []map[string]any{row}
	}
	q.recorder.Add(captureRead(cypher, params, rows, err, q.backend))
	return row, err
}

func captureRead(cypher string, params map[string]any, rows []map[string]any, runErr error, backend string) DifferentialRecord {
	fp, fpErr := FingerprintStatement(cypher, params)
	if fpErr != nil {
		return DifferentialRecord{Backend: backend, RowCount: len(rows), Failed: true, Error: fpErr.Error()}
	}
	if runErr != nil {
		return DifferentialRecord{Fingerprint: fp, Backend: backend, RowCount: len(rows), Failed: true, Error: runErr.Error()}
	}
	digest, digestErr := DigestRows(rows, HasOrderBy(cypher))
	if digestErr != nil {
		return DifferentialRecord{Fingerprint: fp, Backend: backend, RowCount: len(rows), Failed: true, Error: digestErr.Error()}
	}
	return DifferentialRecord{Fingerprint: fp, Backend: backend, RowCount: len(rows), Digest: digest}
}
