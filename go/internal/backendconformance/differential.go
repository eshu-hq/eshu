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
// [WrapExecutor] strips the grouped/phased/probe surface when inner lacks
// GroupExecutor — the WrapExecutorWithGate composition — so capture mode
// preserves production dispatch; the group recorder's remaining guards
// mirror the InstrumentedExecutor loud-fail precedent.

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
//
// Diagnostic metadata keys (any key prefixed with "_", such as the
// `_eshu_*` phase tags) are excluded, mirroring SanitizeStatementParameters:
// they never reach either backend's driver, so they carry no execution
// truth. Without this, a backend whose capture layer sits below its
// sanitize layer records different params than one whose capture sits
// above it, and the same logical statement never pairs (#6782).
func FingerprintStatement(cypher string, params map[string]any) (DifferentialFingerprint, error) {
	fingerprinted, err := normalizeComparisonParams(params)
	if err != nil {
		return DifferentialFingerprint{}, err
	}
	encoded, err := json.Marshal(fingerprinted)
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
// [compareReadRows]. Backend-typed graph values canonicalize first
// ([canonicalizeGraphValue]), then lineage and clock cells blind
// ([canonicalizeDigestValue]): backend-assigned node/relationship identity,
// run-scoped lineage digests, and wall-clock observations are serialization
// or lineage, not graph truth. Rows sort before digesting unless ordered is
// true: a backend is free to return an unordered result in any order, but an
// ORDER BY statement's row order is significant and an order regression
// must change the digest.
func DigestRows(rows []map[string]any, ordered bool) (string, error) {
	encoded := make([]string, 0, len(rows))
	for _, row := range rows {
		normalized, err := normalizeComparisonValue(canonicalizeGraphValue(row))
		if err != nil {
			return "", fmt.Errorf("encode differential row %v: %w", row, err)
		}
		raw, err := json.Marshal(canonicalizeDigestValue(normalized))
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
//
// OnRecord, when non-nil, receives every record as it is added, after the
// record is stored. The graph/capture sessions use it to stream records to
// disk as statements execute, so a SIGTERM-killed replay binary loses at
// most the in-flight statement. It must not call back into the recorder.
type DifferentialRecorder struct {
	mu       sync.Mutex
	records  []DifferentialRecord
	OnRecord func(DifferentialRecord)
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
	r.records = append(r.records, record)
	stream := r.OnRecord
	r.mu.Unlock()
	// Stream outside the lock: the sink blocks on disk, and holding the
	// recorder lock through that would serialize every capturing statement.
	if stream != nil {
		stream(record)
	}
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

// differentialExecutorRecorder decorates a sourcecypher.Executor with
// differential capture of single-statement writes. Writes carry no result
// rows, so each record holds the statement fingerprint and its success.
type differentialExecutorRecorder struct {
	inner    sourcecypher.Executor
	recorder *DifferentialRecorder
	backend  string
}

// differentialGroupRecorder extends the execute-only recorder with the
// grouped, phased, and probe surfaces. It exists as a separate type so
// capture mode preserves production dispatch: callers probing a wrapped
// non-grouping inner must keep falling back to sequential Execute instead
// of hitting a loud unsupported error.
type differentialGroupRecorder struct {
	differentialExecutorRecorder
}

// WrapExecutor returns inner unchanged when capture is disabled or the
// recorder is nil. Otherwise it records every executed statement, stripping
// the grouped/phased/probe surface when inner lacks GroupExecutor — the
// same capability-stripping composition WrapExecutorWithGate uses, so a
// wrapped non-grouping inner keeps its sequential fallback.
//
// Wrap the inner grouped executor, not a phase-only outer (e.g. the
// NornicDB PhaseGroupExecutor, which is not a GroupExecutor): a phase-only
// inner gets the execute-only recorder and loses its phased surface. This
// mirrors the backpressure call-site discipline of gating at the inner
// grouped layer, never the phase-only outer.
func WrapExecutor(inner sourcecypher.Executor, recorder *DifferentialRecorder, backend string) sourcecypher.Executor {
	if inner == nil || recorder == nil || !CaptureEnabled() {
		return inner
	}
	base := differentialExecutorRecorder{inner: inner, recorder: recorder, backend: backend}
	if _, ok := inner.(sourcecypher.GroupExecutor); ok {
		return differentialGroupRecorder{base}
	}
	return base
}

// errDifferentialNoExecuteGroup guards the group entry on a group recorder
// whose inner lacks GroupExecutor, which WrapExecutor prevents but the
// method keeps total, mirroring the InstrumentedExecutor guard.
var errDifferentialNoExecuteGroup = errors.New("differential inner executor does not support ExecuteGroup")

// errDifferentialNoExecutePhaseGroup is returned by ExecutePhaseGroup when a
// group-capable wrapped executor does not implement
// sourcecypher.PhaseGroupExecutor, so a phased write fails loudly rather
// than silently degrading.
var errDifferentialNoExecutePhaseGroup = errors.New("differential inner executor does not support ExecutePhaseGroup")

// errDifferentialNoExecuteProbe is the ExecuteProbe counterpart for the same
// group-capable case.
var errDifferentialNoExecuteProbe = errors.New("differential inner executor does not support ExecuteProbe")

func (e differentialExecutorRecorder) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	return e.recorded(stmt, func() error {
		return e.inner.Execute(ctx, stmt)
	})
}

func (e differentialGroupRecorder) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	grouped, ok := e.inner.(sourcecypher.GroupExecutor)
	if !ok {
		return errDifferentialNoExecuteGroup
	}
	return e.recordedAll(stmts, func() error {
		return grouped.ExecuteGroup(ctx, stmts)
	})
}

func (e differentialGroupRecorder) ExecutePhaseGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	phased, ok := e.inner.(sourcecypher.PhaseGroupExecutor)
	if !ok {
		return errDifferentialNoExecutePhaseGroup
	}
	return e.recordedAll(stmts, func() error {
		return phased.ExecutePhaseGroup(ctx, stmts)
	})
}

func (e differentialGroupRecorder) ExecuteProbe(ctx context.Context, stmt sourcecypher.Statement) (bool, error) {
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
func (e differentialGroupRecorder) recordedProbe(stmt sourcecypher.Statement, run func() (bool, error)) (bool, error) {
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

func (e differentialGroupRecorder) recordedAll(stmts []sourcecypher.Statement, run func() error) error {
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
