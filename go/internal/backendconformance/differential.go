// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// Differential capture is slice 2 of issue #6782: record every production
// Cypher statement a golden-corpus run executes — normalized text
// fingerprint, bound-parameter fingerprint, and a digest of its result rows
// — so slice 3 can diff the NornicDB and Neo4j recordings and fail the gate
// on divergence. The wrappers below sit at the graph reader
// ([GraphQuery]) and executor (sourcecypher.Executor) seams, so they see
// real production statements, not copies. Recording is allocation-light
// (one fingerprint plus one digest per statement) and stays out of the hot
// path: [WrapGraphQuery] and [WrapExecutor] return the inner seam unchanged
// unless [CaptureEnabled] opts in, and a nil recorder is a passthrough.

// captureEnvVar is the opt-in for differential recording. It is gate
// tooling, not operator config, so it stays out of the env registry like
// the other test-gating ESHU_* variables.
const captureEnvVar = "ESHU_DIFFERENTIAL_CAPTURE"

// CaptureEnabled reports whether differential statement recording is on.
func CaptureEnabled() bool {
	return os.Getenv(captureEnvVar) == "1"
}

// DifferentialFingerprint identifies one executed statement: its normalized
// text plus its bound parameters as JSON with sorted keys.
type DifferentialFingerprint struct {
	Statement  string
	Parameters string
}

// FingerprintStatement normalizes a statement's text (whitespace runs
// collapse to one blank) and encodes its parameters. The same production
// statement produces the same fingerprint on either backend; formatting
// drift does not.
func FingerprintStatement(cypher string, params map[string]any) (DifferentialFingerprint, error) {
	encoded, err := json.Marshal(params)
	if err != nil {
		return DifferentialFingerprint{}, fmt.Errorf("encode differential parameters: %w", err)
	}
	return DifferentialFingerprint{
		Statement:  strings.Join(strings.Fields(cypher), " "),
		Parameters: string(encoded),
	}, nil
}

// HasOrderBy reports whether cypher carries an ORDER BY clause. Matching is
// case-insensitive over whole words outside string literals, backtick-quoted
// identifiers, and line/block comments, so none of those can force an
// order-sensitive digest and a false divergence. Both quote styles honor the
// backslash escape and the doubled-quote escape. Skipping is one-directional
// by design: an ORDER BY hidden inside a comment or literal would read as
// unordered, but production builders emit clauses, not prose about clauses.
func HasOrderBy(cypher string) bool {
	var words []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			words = append(words, strings.ToUpper(current.String()))
			current.Reset()
		}
	}
	// skipQuoted consumes a quoted span starting at i (the opening quote or
	// backtick) and returns the index just past its closer. A backslash
	// escapes the next byte; otherwise a doubled quote is one escaped quote.
	skipQuoted := func(i int, quote byte) int {
		for j := i + 1; j < len(cypher); j++ {
			switch cypher[j] {
			case '\\':
				j++
			case quote:
				if j+1 < len(cypher) && cypher[j+1] == quote {
					j++
					continue
				}
				return j + 1
			}
		}
		return len(cypher)
	}
	for i := 0; i < len(cypher); {
		c := cypher[i]
		switch {
		case c == '/' && i+1 < len(cypher) && cypher[i+1] == '/':
			flush()
			next := strings.IndexByte(cypher[i:], '\n')
			if next < 0 {
				return hasOrderByWords(words)
			}
			i += next
		case c == '/' && i+1 < len(cypher) && cypher[i+1] == '*':
			flush()
			end := strings.Index(cypher[i+2:], "*/")
			if end < 0 {
				return hasOrderByWords(words)
			}
			i += end + 4
		case c == '\'' || c == '"' || c == '`':
			flush()
			i = skipQuoted(i, c)
		case c == '_' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z':
			current.WriteByte(c)
			i++
		default:
			flush()
			i++
		}
	}
	flush()
	return hasOrderByWords(words)
}

// hasOrderByWords reports whether adjacent upper-cased words spell ORDER BY.
func hasOrderByWords(words []string) bool {
	for i := 0; i+1 < len(words); i++ {
		if words[i] == "ORDER" && words[i+1] == "BY" {
			return true
		}
	}
	return false
}

// DigestRows digests result rows for differential comparison. Each row is
// normalized to its JSON encoding (which sorts map keys and erases driver
// value-type differences such as int64 versus int), matching
// [compareReadRows]. Rows sort before digesting unless ordered is true: a
// backend is free to return an unordered result in any order, but an
// ORDER BY statement's row order is significant and an order regression
// must change the digest.
func DigestRows(rows []map[string]any, ordered bool) (string, error) {
	encoded := make([]string, 0, len(rows))
	for _, row := range rows {
		raw, err := json.Marshal(row)
		if err != nil {
			return "", fmt.Errorf("encode differential row %v: %w", row, err)
		}
		encoded = append(encoded, string(raw))
	}
	if !ordered {
		slices.Sort(encoded)
	}
	sum := sha256.Sum256([]byte(strings.Join(encoded, "\n")))
	return hex.EncodeToString(sum[:]), nil
}

// DifferentialRecord is one captured statement execution.
type DifferentialRecord struct {
	Fingerprint DifferentialFingerprint
	Backend     string
	RowCount    int
	Digest      string
	Failed      bool
}

// DifferentialRecorder collects records in execution order. It is safe for
// concurrent use.
type DifferentialRecorder struct {
	mu      sync.Mutex
	records []DifferentialRecord
}

// NewDifferentialRecorder returns an empty recorder.
func NewDifferentialRecorder() *DifferentialRecorder {
	return &DifferentialRecorder{}
}

// Add appends one record.
func (r *DifferentialRecorder) Add(record DifferentialRecord) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, record)
}

// Records returns a copy of the records in execution order.
func (r *DifferentialRecorder) Records() []DifferentialRecord {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.records)
}

// DifferentialDifference is one divergence between two recordings.
type DifferentialDifference struct {
	Fingerprint DifferentialFingerprint
	Detail      string
}

// CompareRecordings diffs two recordings statement by statement, keyed by
// fingerprint. Executions group per fingerprint as a multiset of digests:
// a fingerprint present on only one side, a digest-multiset mismatch, or a
// row-count mismatch each yields one difference naming the statement.
// Grouping (rather than last-write-wins) keeps a statement that answers
// differently across repeated executions from masking itself.
func CompareRecordings(a, b []DifferentialRecord) []DifferentialDifference {
	byFingerprint := func(records []DifferentialRecord) map[DifferentialFingerprint][]DifferentialRecord {
		out := make(map[DifferentialFingerprint][]DifferentialRecord)
		for _, rec := range records {
			out[rec.Fingerprint] = append(out[rec.Fingerprint], rec)
		}
		return out
	}
	left, right := byFingerprint(a), byFingerprint(b)
	digests := func(recs []DifferentialRecord) []string {
		out := make([]string, 0, len(recs))
		for _, rec := range recs {
			out = append(out, rec.Digest)
		}
		slices.Sort(out)
		return out
	}
	counts := func(recs []DifferentialRecord) int {
		total := 0
		for _, rec := range recs {
			total += rec.RowCount
		}
		return total
	}
	backend := func(recs []DifferentialRecord) string {
		if len(recs) == 0 {
			return ""
		}
		return recs[0].Backend
	}
	var diffs []DifferentialDifference
	for fp, lrecs := range left {
		rrecs, ok := right[fp]
		if !ok {
			diffs = append(diffs, DifferentialDifference{Fingerprint: fp, Detail: "recorded on the first backend only"})
			continue
		}
		if !slices.Equal(digests(lrecs), digests(rrecs)) {
			diffs = append(diffs, DifferentialDifference{
				Fingerprint: fp,
				Detail:      fmt.Sprintf("row digest differs (%s=%d rows, %s=%d rows)", backend(lrecs), counts(lrecs), backend(rrecs), counts(rrecs)),
			})
			continue
		}
		if counts(lrecs) != counts(rrecs) {
			diffs = append(diffs, DifferentialDifference{
				Fingerprint: fp,
				Detail:      fmt.Sprintf("row count differs (%s=%d, %s=%d) with equal digests", backend(lrecs), counts(lrecs), backend(rrecs), counts(rrecs)),
			})
		}
	}
	for fp := range right {
		if _, ok := left[fp]; !ok {
			diffs = append(diffs, DifferentialDifference{Fingerprint: fp, Detail: "recorded on the second backend only"})
		}
	}
	slices.SortFunc(diffs, func(x, y DifferentialDifference) int {
		if x.Fingerprint.Statement != y.Fingerprint.Statement {
			return strings.Compare(x.Fingerprint.Statement, y.Fingerprint.Statement)
		}
		return strings.Compare(x.Fingerprint.Parameters, y.Fingerprint.Parameters)
	})
	return diffs
}

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
		return DifferentialRecord{Backend: backend, RowCount: len(rows), Failed: true}
	}
	if runErr != nil {
		return DifferentialRecord{Fingerprint: fp, Backend: backend, RowCount: len(rows), Failed: true}
	}
	digest, digestErr := DigestRows(rows, HasOrderBy(cypher))
	if digestErr != nil {
		return DifferentialRecord{Fingerprint: fp, Backend: backend, RowCount: len(rows), Failed: true}
	}
	return DifferentialRecord{Fingerprint: fp, Backend: backend, RowCount: len(rows), Digest: digest}
}

// differentialExecutorRecorder decorates a sourcecypher.Executor with differential
// capture. Writes carry no result rows, so each record holds the statement
// fingerprint and its success.
type differentialExecutorRecorder struct {
	inner    sourcecypher.Executor
	recorder *DifferentialRecorder
	backend  string
}

// WrapExecutor returns inner unchanged when capture is disabled or the
// recorder is nil; otherwise it records every executed statement.
func WrapExecutor(inner sourcecypher.Executor, recorder *DifferentialRecorder, backend string) sourcecypher.Executor {
	if inner == nil || recorder == nil || !CaptureEnabled() {
		return inner
	}
	return differentialExecutorRecorder{inner: inner, recorder: recorder, backend: backend}
}

// errDifferentialNoExecuteGroup is returned by ExecuteGroup when the wrapped
// executor does not implement sourcecypher.GroupExecutor, mirroring the
// backpressure wrapper's explicit guard so a grouped write fails loudly
// rather than silently degrading to per-statement execution.
var errDifferentialNoExecuteGroup = errors.New("differential inner executor does not support ExecuteGroup")

// errDifferentialNoExecutePhaseGroup is the ExecutePhaseGroup counterpart.
var errDifferentialNoExecutePhaseGroup = errors.New("differential inner executor does not support ExecutePhaseGroup")

// errDifferentialNoExecuteProbe is the ExecuteProbe counterpart. The probe
// contract requires every GroupExecutor-forwarding wrapper to forward
// probing the same way, so callers that already type-asserted keep a loud
// unsupported error to fail safe on instead of a silent skip.
var errDifferentialNoExecuteProbe = errors.New("differential inner executor does not support ExecuteProbe")

func (e differentialExecutorRecorder) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	return e.recorded(stmt, func() error {
		return e.inner.Execute(ctx, stmt)
	})
}

func (e differentialExecutorRecorder) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	grouped, ok := e.inner.(sourcecypher.GroupExecutor)
	if !ok {
		return errDifferentialNoExecuteGroup
	}
	return e.recordedAll(stmts, func() error {
		return grouped.ExecuteGroup(ctx, stmts)
	})
}

func (e differentialExecutorRecorder) ExecutePhaseGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	phased, ok := e.inner.(sourcecypher.PhaseGroupExecutor)
	if !ok {
		return errDifferentialNoExecutePhaseGroup
	}
	return e.recordedAll(stmts, func() error {
		return phased.ExecutePhaseGroup(ctx, stmts)
	})
}

func (e differentialExecutorRecorder) ExecuteProbe(ctx context.Context, stmt sourcecypher.Statement) (bool, error) {
	prober, ok := e.inner.(sourcecypher.ProbeExecutor)
	if !ok {
		return false, errDifferentialNoExecuteProbe
	}
	return e.recordedProbe(stmt, func() (bool, error) {
		return prober.ExecuteProbe(ctx, stmt)
	})
}

// recordedProbe captures a probe as a one-row read: the found boolean is
// the row, so a backend that probes differently digests differently.
func (e differentialExecutorRecorder) recordedProbe(stmt sourcecypher.Statement, run func() (bool, error)) (bool, error) {
	found, err := run()
	var rows []map[string]any
	if err == nil {
		rows = []map[string]any{{"found": found}}
	}
	e.recorder.Add(captureRead(stmt.Cypher, stmt.Parameters, rows, err, e.backend))
	return found, err
}

// recorded and recordedAll capture write executions and pass the inner
// result through untouched, under the same transparency rule as the read
// recorded helper above.
func (e differentialExecutorRecorder) recorded(stmt sourcecypher.Statement, run func() error) error {
	err := run()
	e.recorder.Add(captureWrite(stmt, err, e.backend))
	return err
}

func (e differentialExecutorRecorder) recordedAll(stmts []sourcecypher.Statement, run func() error) error {
	err := run()
	for _, stmt := range stmts {
		e.recorder.Add(captureWrite(stmt, err, e.backend))
	}
	return err
}

func captureWrite(stmt sourcecypher.Statement, execErr error, backend string) DifferentialRecord {
	fp, fpErr := FingerprintStatement(stmt.Cypher, stmt.Parameters)
	if fpErr != nil {
		return DifferentialRecord{Backend: backend, Failed: true}
	}
	return DifferentialRecord{Fingerprint: fp, Backend: backend, Failed: execErr != nil}
}
