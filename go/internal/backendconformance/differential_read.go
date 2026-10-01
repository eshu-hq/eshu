// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"context"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/queryplan"
)

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
	callsite := recordCallsite()
	fp, fpErr := FingerprintStatement(cypher, params)
	if fpErr != nil {
		return DifferentialRecord{Backend: backend, Callsite: callsite, RowCount: len(rows), Failed: true, Error: fpErr.Error()}
	}
	if runErr != nil {
		return DifferentialRecord{Fingerprint: fp, Backend: backend, Callsite: callsite, RowCount: len(rows), Failed: true, Error: runErr.Error()}
	}
	digest, digestErr := DigestRows(rows, HasOrderBy(cypher))
	if digestErr != nil {
		return DifferentialRecord{Fingerprint: fp, Backend: backend, Callsite: callsite, RowCount: len(rows), Failed: true, Error: digestErr.Error()}
	}
	return DifferentialRecord{Fingerprint: fp, Backend: backend, Callsite: callsite, RowCount: len(rows), Digest: digest}
}

// captureAttributionFrames are the differential-capture decorator frames
// recordCallsite skips, by function-name suffix. The skip is by name,
// never by depth, and covers only this decorator: a future read-seam
// decorator in another package would attribute to itself, match no
// exemption, and fail the gate loudly instead of silently — extending
// this set is part of adding such a decorator.
var captureAttributionFrames = map[string]struct{}{
	"backendconformance.differentialQueryRecorder.Run":            {},
	"backendconformance.differentialQueryRecorder.RunSingle":      {},
	"backendconformance.differentialQueryRecorder.recorded":       {},
	"backendconformance.differentialQueryRecorder.recordedSingle": {},
	"backendconformance.captureRead":                              {},
	"backendconformance.recordCallsite":                           {},
}

// recordCallsite resolves the builder identity of the statement being
// recorded: the go-relative path:symbol of the first stack frame outside
// the capture decorator and the runtime, i.e. the direct Run/RunSingle
// caller (#7233). CallersFrames expands inlined frames so wrapper layers
// cannot hide the caller. An empty string means the identity could not
// be resolved; it matches no exemption and fails closed downstream.
func recordCallsite() string {
	goDir, modulePath := callsiteRoots()
	if goDir == "" || modulePath == "" {
		return ""
	}
	pcs := make([]uintptr, 32)
	if runtime.Callers(0, pcs) == 0 {
		return ""
	}
	frames := runtime.CallersFrames(pcs)
	for {
		frame, more := frames.Next()
		if !skipAttributionFrame(frame.Function) {
			return callsiteIdentity(goDir, modulePath, frame)
		}
		if !more {
			return ""
		}
	}
}

// skipAttributionFrame reports whether a frame belongs to the capture
// decorator or the runtime rather than to statement-producing code.
func skipAttributionFrame(function string) bool {
	if strings.HasPrefix(function, "runtime.") {
		return true
	}
	for suffix := range captureAttributionFrames {
		if strings.HasSuffix(function, suffix) {
			return true
		}
	}
	return false
}

var callsiteRoots = sync.OnceValues(func() (goDir, modulePath string) {
	goDir, err := queryplan.GoDir()
	if err != nil {
		return "", ""
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return "", ""
	}
	if info.Main.Path == "" {
		return "", ""
	}
	return goDir, info.Main.Path
})

// callsiteIdentity formats one frame as go-relative path:symbol, e.g.
// internal/query/entity/context_handler.go:(*Handler).GetEntityContext.
// The file comes from the frame's file path; the symbol is the frame
// function with the module path and package directory trimmed, which
// keeps method receivers, closures, and generic instantiations intact.
// Main-package frames report a bare main. prefix (no import path), so
// the file alone locates them. Anything unparseable yields "" and fails
// closed downstream.
func callsiteIdentity(goDir, modulePath string, frame runtime.Frame) string {
	rel, err := filepath.Rel(goDir, frame.File)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return ""
	}
	rel = filepath.ToSlash(rel)
	qualified, ok := strings.CutPrefix(frame.Function, modulePath+"/")
	if !ok {
		qualified, ok = strings.CutPrefix(frame.Function, "main.")
		if !ok {
			return ""
		}
		// A bare main. symbol belongs to exactly one main package: the
		// file carrying this frame locates it, so no package path is
		// needed to keep the identity unique. Canonicalized like every
		// other symbol so a main-package method matches its manifest
		// entry whatever receiver form each side was written in.
		return rel + ":" + queryplan.CanonicalCallsiteSymbol(qualified)
	}
	// qualified is <dir>.<symbol...>: trim the directory prefix, keeping
	// receivers, closures, and generic instantiations intact.
	dir := ""
	if index := strings.LastIndex(rel, "/"); index >= 0 {
		dir = rel[:index]
	}
	symbol := qualified
	if dir != "" {
		var ok bool
		symbol, ok = strings.CutPrefix(qualified, dir+".")
		if !ok {
			return ""
		}
	}
	if symbol == "" {
		return ""
	}
	// Canonicalize the receiver form (value receivers record bare):
	// manifest load applies the same rule, so exemption and recording
	// meet on one identity.
	return rel + ":" + queryplan.CanonicalCallsiteSymbol(symbol)
}
