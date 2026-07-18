// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

const (
	defaultOrphanSweepBatchLimit = 100
	defaultOrphanSweepCountLimit = 10000
	defaultOrphanSweepTTL        = 7 * 24 * time.Hour

	// defaultOrphanSweepConnectedKeysChunkSize bounds how many keys one
	// BuildConnectedKeysQuery round trip anchors on. #5147 finding 2 measured
	// the UNWIND-per-key anchored S2 read's own per-statement cost scaling
	// super-linearly with key-list size on both pinned NornicDB backends
	// (roughly quadratic: 200 keys ~14ms, 5,000 keys ~4.7s). Splitting one
	// large key list into fixed-size chunked round trips keeps each
	// individual statement's key count small and empirically recovers close
	// to linear total cost (5,000 keys in 10 chunks of 500: ~570ms measured,
	// vs ~4.7s unchunked -- see evidence-5147-orphan-sweep-antijoin.md). This
	// preserves the anchored, CountLimit-bounded read shape -- it never falls
	// back to an unbounded full-label scan, so it carries no additional
	// relationship-existence-predicate or unbounded-backlog-scan risk.
	defaultOrphanSweepConnectedKeysChunkSize = 500
)

// OrphanSweepLabel names one closed graph node label eligible for bounded
// orphan detection and cleanup.
type OrphanSweepLabel string

const (
	// OrphanSweepLabelRepository covers relationship-created repository targets.
	OrphanSweepLabelRepository OrphanSweepLabel = "Repository"
	// OrphanSweepLabelPlatform covers inferred runtime and infrastructure targets.
	OrphanSweepLabelPlatform OrphanSweepLabel = "Platform"
	// OrphanSweepLabelEvidenceArtifact covers repository relationship evidence nodes.
	OrphanSweepLabelEvidenceArtifact OrphanSweepLabel = "EvidenceArtifact"
	// OrphanSweepLabelFile covers disconnected canonical source file nodes.
	OrphanSweepLabelFile OrphanSweepLabel = "File"
	// OrphanSweepLabelDirectory covers disconnected canonical source directory nodes.
	OrphanSweepLabelDirectory OrphanSweepLabel = "Directory"
	// OrphanSweepLabelModule covers disconnected canonical imported module nodes.
	OrphanSweepLabelModule OrphanSweepLabel = "Module"
)

// DefaultOrphanSweepLabels returns the closed orphan sweep label set.
func DefaultOrphanSweepLabels() []OrphanSweepLabel {
	return []OrphanSweepLabel{
		OrphanSweepLabelRepository,
		OrphanSweepLabelPlatform,
		OrphanSweepLabelEvidenceArtifact,
		OrphanSweepLabelFile,
		OrphanSweepLabelDirectory,
		OrphanSweepLabelModule,
	}
}

// OrphanSweepReader runs bounded graph read queries used by the orphan observer.
type OrphanSweepReader interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}

// OrphanSweepPolicy bounds one orphan sweep cycle.
type OrphanSweepPolicy struct {
	OrphanTTL  time.Duration
	BatchLimit int
	CountLimit int
	Labels     []string
	Now        time.Time
}

// OrphanSweepResult summarizes one graph orphan sweep cycle.
type OrphanSweepResult struct {
	Counts  map[string]int64
	Marked  map[string]int64
	Deleted map[string]int64
	// Skipped counts write statements that were not executed because the
	// computed key set for that write was empty. Values are 0..3 per label
	// (clear, mark, sweep).
	Skipped  map[string]int64
	Duration time.Duration
}

// OrphanSweepStore counts, marks, and deletes disconnected graph nodes through
// backend-neutral Cypher seams.
//
// The store never relies on a relationship-existence predicate (`NOT
// (n)--()`, `(n)--()`, or `COUNT { (n)--() } = 0`): on the pinned NornicDB
// backends every such predicate is mis-evaluated (see
// docs/public/reference/nornicdb-pitfalls.md). Instead it computes orphan
// status as a Go-side anti-join between two concrete-relationship-variable
// reads: candidates (S1, a label+evidence_source scan) and connected keys
// (S2, `MATCH (n:Label {key: k})-[r]-(m)`, the only relationship primitive
// proven reliable on both pinned backends). See orphan_sweep_queries.go for
// the read builders and orphan_sweep_writes.go for the key-anchored writes.
type OrphanSweepStore struct {
	Executor   Executor
	Reader     OrphanSweepReader
	CountLimit int
	Labels     []OrphanSweepLabel
	Now        func() time.Time

	// cursors holds the last identity key each label's S1 candidate read
	// reached, so successive cycles page deterministically through a label with
	// more matching nodes than CountLimit instead of re-reading the same window.
	// It is process-local: the reducer reuses one OrphanSweepStore across all
	// cycles (guarded by the graph_orphan_sweep single-partition lease, so calls
	// are serial), and a restart resets to the label start, which harmlessly
	// re-scans from the beginning. The mutex keeps it safe if a store is ever
	// shared across goroutines.
	cursorMu sync.Mutex
	cursors  map[OrphanSweepLabel]string
}

// NewOrphanSweepStore returns a graph orphan sweep store.
func NewOrphanSweepStore(executor Executor, reader OrphanSweepReader) *OrphanSweepStore {
	return &OrphanSweepStore{
		Executor: executor,
		Reader:   reader,
		Labels:   DefaultOrphanSweepLabels(),
	}
}

// SweepOrphanNodes marks newly observed orphan nodes and deletes nodes whose
// orphan marker is older than the configured TTL.
func (s *OrphanSweepStore) SweepOrphanNodes(ctx context.Context, policy OrphanSweepPolicy) (OrphanSweepResult, error) {
	if s == nil || s.Executor == nil {
		return OrphanSweepResult{}, fmt.Errorf("orphan sweep executor is required")
	}
	if s.Reader == nil {
		return OrphanSweepResult{}, fmt.Errorf("orphan sweep reader is required")
	}

	if policy.Now.IsZero() && s.Now != nil {
		policy.Now = s.Now().UTC()
	}
	policy = normalizeOrphanSweepPolicy(policy)
	labels, err := orphanSweepLabels(policy.Labels)
	if err != nil {
		return OrphanSweepResult{}, err
	}
	start := time.Now()
	nowUnix := policy.Now.Unix()
	cutoffUnix := policy.Now.Add(-policy.OrphanTTL).Unix()
	result := OrphanSweepResult{
		Counts:  make(map[string]int64, len(labels)),
		Marked:  make(map[string]int64, len(labels)),
		Deleted: make(map[string]int64, len(labels)),
		Skipped: make(map[string]int64, len(labels)),
	}

	for _, label := range labels {
		labelKey := string(label)
		cycle, err := s.runOrphanSweepCycle(ctx, label, policy, nowUnix, cutoffUnix)
		if err != nil {
			return OrphanSweepResult{}, err
		}
		result.Counts[labelKey] = cycle.orphanCount
		result.Marked[labelKey] = cycle.markedCount
		result.Deleted[labelKey] = cycle.deletedCount
		result.Skipped[labelKey] = cycle.skipped
	}
	result.Duration = time.Since(start)
	return result, nil
}

// orphanSweepCycleResult summarizes one label's anti-join cycle.
type orphanSweepCycleResult struct {
	orphanCount  int64
	markedCount  int64
	deletedCount int64
	skipped      int64
}

// runOrphanSweepCycle runs the S1/S2 anti-join reads, computes the
// clear/mark/sweep key sets in Go, and issues only the key-anchored writes
// that have a non-empty key set. Steady state (no orphans, no markers) issues
// exactly two reads (S1, S2) and zero writes.
func (s *OrphanSweepStore) runOrphanSweepCycle(
	ctx context.Context,
	label OrphanSweepLabel,
	policy OrphanSweepPolicy,
	nowUnix, cutoffUnix int64,
) (orphanSweepCycleResult, error) {
	var out orphanSweepCycleResult

	// S1: candidates are every node this label's sweep is allowed to touch,
	// regardless of relationship state. No relationship predicate appears
	// here; connectivity is resolved entirely by S2 below. The read pages past
	// the label's cursor and is ORDER BY the identity key, so a label with more
	// nodes than CountLimit is covered deterministically across cycles.
	candidates, err := s.readCandidateOrphanNodes(ctx, label, policy.CountLimit, s.candidateCursor(label))
	if err != nil {
		return out, err
	}

	candidateKeys := make([]string, 0, len(candidates))
	for _, c := range candidates {
		candidateKeys = append(candidateKeys, c.key)
	}
	sort.Strings(candidateKeys)
	// Advance the cursor before any early return so an all-connected window
	// still makes forward progress next cycle rather than re-reading the same
	// rows. An empty window wraps the cursor to the label start.
	s.advanceCursor(label, candidateKeys, policy.CountLimit)

	if len(candidates) == 0 {
		out.skipped = 3
		return out, nil
	}

	// S2: the only relationship primitive proven reliable on both pinned
	// NornicDB backends -- a concrete relationship-variable MATCH anchored on
	// the candidate keys.
	connectedKeys, err := s.readConnectedKeys(ctx, label, candidateKeys)
	if err != nil {
		return out, err
	}
	connected := make(map[string]bool, len(connectedKeys))
	for _, k := range connectedKeys {
		connected[k] = true
	}

	marked := make(map[string]bool, len(candidates))
	observedAt := make(map[string]int64, len(candidates))
	orphans := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		if c.observedAt != nil {
			marked[c.key] = true
			observedAt[c.key] = *c.observedAt
		}
		if !connected[c.key] {
			orphans[c.key] = true
		}
	}
	out.orphanCount = int64(len(orphans))

	toClear := sortedKeysWhere(marked, func(k string) bool { return connected[k] })
	toMarkAll := sortedKeysWhere(orphans, func(k string) bool { return !marked[k] })
	toMark := boundedKeys(toMarkAll, policy.BatchLimit)
	toSweepAll := sortedKeysWhere(orphans, func(k string) bool {
		return marked[k] && observedAt[k] <= cutoffUnix
	})
	toSweep := boundedKeys(toSweepAll, policy.BatchLimit)

	if len(toClear) > 0 {
		stmt, _ := BuildClearOrphanMarkerStatement(label, toClear)
		if err := s.Executor.Execute(ctx, stmt); err != nil {
			return out, fmt.Errorf("clear orphan marker for %s: %w", label, err)
		}
	} else {
		out.skipped++
	}

	if len(toMark) > 0 {
		stmt, _ := BuildMarkOrphanNodesStatement(label, toMark, nowUnix)
		if err := s.Executor.Execute(ctx, stmt); err != nil {
			return out, fmt.Errorf("mark orphan nodes for %s: %w", label, err)
		}
		out.markedCount = int64(len(toMark))
	} else {
		out.skipped++
	}

	if len(toSweep) == 0 {
		out.skipped++
		return out, nil
	}

	// TOCTOU guard: re-verify connectivity for exactly the keys about to be
	// deleted, immediately before the delete. A node can regain a
	// relationship between the top-of-cycle S2 read and this delete; this
	// cheap, BatchLimit-bounded re-read (only on a sweeping cycle) drops any
	// key that reconnected in that window instead of deleting it.
	reverifyConnected, err := s.readConnectedKeys(ctx, label, toSweep)
	if err != nil {
		return out, err
	}
	reconnected := make(map[string]bool, len(reverifyConnected))
	for _, k := range reverifyConnected {
		reconnected[k] = true
	}
	finalSweep := make([]string, 0, len(toSweep))
	for _, k := range toSweep {
		if !reconnected[k] {
			finalSweep = append(finalSweep, k)
		}
	}
	if len(finalSweep) == 0 {
		out.skipped++
		return out, nil
	}

	stmt, _ := BuildSweepOrphanNodesStatement(label, finalSweep, cutoffUnix)
	if err := s.Executor.Execute(ctx, stmt); err != nil {
		return out, fmt.Errorf("sweep orphan nodes for %s: %w", label, err)
	}
	out.deletedCount = int64(len(finalSweep))
	return out, nil
}

// GraphOrphanNodeCounts returns bounded disconnected-node counts by label
// using the same S1/S2 anti-join as SweepOrphanNodes, without issuing writes.
func (s *OrphanSweepStore) GraphOrphanNodeCounts(ctx context.Context) (map[string]int64, error) {
	if s == nil || s.Reader == nil {
		return nil, fmt.Errorf("orphan sweep reader is required")
	}
	labels := s.Labels
	if len(labels) == 0 {
		labels = DefaultOrphanSweepLabels()
	}
	limit := s.CountLimit
	if limit <= 0 {
		limit = defaultOrphanSweepCountLimit
	}
	counts := make(map[string]int64, len(labels))
	for _, label := range labels {
		// GraphOrphanNodeCounts is a read-only gauge, not the paging sweep, so
		// it always reads the first bounded window (cursor "") rather than
		// advancing the sweep's cursor.
		candidates, err := s.readCandidateOrphanNodes(ctx, label, limit, "")
		if err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			counts[string(label)] = 0
			continue
		}
		keys := make([]string, 0, len(candidates))
		for _, c := range candidates {
			keys = append(keys, c.key)
		}
		sort.Strings(keys)
		connectedKeys, err := s.readConnectedKeys(ctx, label, keys)
		if err != nil {
			return nil, err
		}
		connected := make(map[string]bool, len(connectedKeys))
		for _, k := range connectedKeys {
			connected[k] = true
		}
		var orphanCount int64
		for _, k := range keys {
			if !connected[k] {
				orphanCount++
			}
		}
		counts[string(label)] = orphanCount
	}
	return counts, nil
}

func orphanLabelMatch(label OrphanSweepLabel) (string, bool) {
	switch label {
	case OrphanSweepLabelRepository:
		return "Repository", true
	case OrphanSweepLabelPlatform:
		return "Platform", true
	case OrphanSweepLabelEvidenceArtifact:
		return "EvidenceArtifact", true
	case OrphanSweepLabelFile:
		return "File", true
	case OrphanSweepLabelDirectory:
		return "Directory", true
	case OrphanSweepLabelModule:
		return "Module", true
	default:
		return "", false
	}
}

// orphanSweepIdentityKey returns the per-label identity property used to
// anchor the key-based connected-keys read and the clear/mark/sweep writes.
// These mirror the canonical writers' MERGE identity: Repository/Platform/
// EvidenceArtifact use `id`, File/Directory use `path`, Module uses `name`.
func orphanSweepIdentityKey(label OrphanSweepLabel) (string, bool) {
	switch label {
	case OrphanSweepLabelRepository, OrphanSweepLabelPlatform, OrphanSweepLabelEvidenceArtifact:
		return "id", true
	case OrphanSweepLabelFile, OrphanSweepLabelDirectory:
		return "path", true
	case OrphanSweepLabelModule:
		return "name", true
	default:
		return "", false
	}
}

func orphanSweepEvidencePredicate(label OrphanSweepLabel) string {
	if label == OrphanSweepLabelRepository {
		return "n.evidence_source IS NOT NULL\n  AND n.evidence_source <> 'projector/canonical'"
	}
	return "n.evidence_source IS NOT NULL"
}

func orphanSweepLabels(raw []string) ([]OrphanSweepLabel, error) {
	if len(raw) == 0 {
		return DefaultOrphanSweepLabels(), nil
	}
	labels := make([]OrphanSweepLabel, 0, len(raw))
	for _, value := range raw {
		label := OrphanSweepLabel(value)
		if _, ok := orphanLabelMatch(label); !ok {
			return nil, fmt.Errorf("unsupported orphan sweep label %q", value)
		}
		labels = append(labels, label)
	}
	return labels, nil
}

func normalizeOrphanSweepPolicy(policy OrphanSweepPolicy) OrphanSweepPolicy {
	if policy.OrphanTTL <= 0 {
		policy.OrphanTTL = defaultOrphanSweepTTL
	}
	policy.BatchLimit = normalizePositiveInt(policy.BatchLimit, defaultOrphanSweepBatchLimit)
	policy.CountLimit = normalizePositiveInt(policy.CountLimit, defaultOrphanSweepCountLimit)
	if policy.Now.IsZero() {
		policy.Now = time.Now().UTC()
	}
	return policy
}

func normalizePositiveInt(value int, defaultValue int) int {
	if value <= 0 {
		return defaultValue
	}
	return value
}

// sortedKeysWhere returns the keys of set for which predicate is true, sorted
// ascending for deterministic write ordering and testable output.
func sortedKeysWhere(set map[string]bool, predicate func(string) bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		if predicate(k) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// boundedKeys caps keys at limit (or defaultOrphanSweepBatchLimit when limit
// is non-positive), preserving the input (already sorted) order.
func boundedKeys(keys []string, limit int) []string {
	if limit <= 0 {
		limit = defaultOrphanSweepBatchLimit
	}
	if len(keys) <= limit {
		return keys
	}
	return keys[:limit]
}
