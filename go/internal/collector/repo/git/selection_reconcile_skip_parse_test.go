// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/preparse"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// skipParseResolver serves a settable reconcile state per scope and records
// the skipped-parse re-projections the sync asks for.
type skipParseResolver struct {
	states       map[string]scope.FullReconcileState
	fallback     scope.FullReconcileState
	writers      []scope.UncoveredProjectionWriter
	reprojected  []scope.ActiveReprojection
	reprojectErr error
}

func (r *skipParseResolver) LastProjectedCommitSHA(context.Context, string) (string, error) {
	return "samesha", nil
}

func (r *skipParseResolver) FullReconcileState(_ context.Context, scopeID string) (scope.FullReconcileState, error) {
	if state, ok := r.states[scopeID]; ok {
		return state, nil
	}
	return r.fallback, nil
}

func (r *skipParseResolver) UncoveredProjectionWriters(context.Context, string) ([]scope.UncoveredProjectionWriter, error) {
	return r.writers, nil
}

func (r *skipParseResolver) ReprojectActiveGeneration(_ context.Context, request scope.ActiveReprojection) (string, error) {
	if r.reprojectErr != nil {
		return "", r.reprojectErr
	}
	r.reprojected = append(r.reprojected, request)
	return telemetry.RefreshSkipReasonReconcileReprojected, nil
}

// skipParseWorld is one or more managed repositories, a fake git whose remote
// head is samesha, and the pre-parse configuration the sync uses.
type skipParseWorld struct {
	t        *testing.T
	reposDir string
	now      time.Time
	config   preparse.Config
	resolver *skipParseResolver
	reader   *sdkmetric.ManualReader
	inst     *telemetry.Instruments
}

func newSkipParseWorld(t *testing.T, gitArms string, repos ...string) *skipParseWorld {
	t.Helper()
	w := &skipParseWorld{
		t: t, reposDir: t.TempDir(), now: time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC),
		config:   preparse.Config{SCIPLanguages: []string{"go"}},
		resolver: &skipParseResolver{states: map[string]scope.FullReconcileState{}},
		reader:   sdkmetric.NewManualReader(),
	}
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(w.reader)).Meter("skip-parse-test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	w.inst = inst
	for _, repo := range repos {
		if err := os.MkdirAll(filepath.Join(w.reposDir, "github", "org", repo, ".git"), 0o755); err != nil {
			t.Fatalf("create .git marker: %v", err)
		}
	}
	writeFakeGitForBaseline(t, `	*"rev-parse refs/remotes/origin/main"*)
		printf "samesha\n"
		;;
	*"checkout -B main refs/remotes/origin/main"*)
		;;`+gitArms)
	return w
}

func (w *skipParseWorld) repoPath(repo string) string {
	return filepath.Join(w.reposDir, "github", "org", repo)
}

func (w *skipParseWorld) scopeID(repo string) string {
	return gitScopeIDForManagedRepo(reconcileTestConfig(w.reposDir), w.repoPath(repo))
}

// identityOf is the identity the sync computes for repo at samesha: one
// bootstrap sync with no stored identity, on a throwaway resolver, gives the
// refs the sync reads (the default branch comes from them).
func (w *skipParseWorld) identityOf(repo string) string {
	w.t.Helper()
	saved, savedInst := w.resolver, w.inst
	w.resolver = &skipParseResolver{fallback: w.overdue("", 0)}
	w.inst = nil
	synced := w.sync(7, nil, repo)
	w.resolver, w.inst = saved, savedInst
	refs, ok := synced.RefsByRepoPath[w.repoPath(repo)]
	if !ok {
		w.t.Fatalf("bootstrap sync did not select %s", repo)
	}
	return preparse.Identity(GenerationContentVersion(), "samesha", repositoryDefaultBranch(refs), w.config)
}

func (w *skipParseWorld) identity() string { return w.identityOf("big") }

// overdue is a state whose active full generation is 25 h old at samesha.
func (w *skipParseWorld) overdue(identity string, skips int) scope.FullReconcileState {
	at := w.now.Add(-25 * time.Hour)
	return scope.FullReconcileState{
		HasProjectedFull: true, LastProjectedFullAt: at, LastStoredFullAt: at,
		HasLatestFull: true, LatestFullAt: at, LatestFullStatus: scope.GenerationStatusActive, LatestFullProjected: true,
		ActiveGenerationID: "gen-a", ActiveCommitSHA: "samesha",
		ActivePreparseIdentity: identity, ActivePreparseSkips: skips,
	}
}

func (w *skipParseWorld) sync(maxSkips int, baseline func(*gitDeltaBaseline), repos ...string) GitSyncSelection {
	w.t.Helper()
	ids := make([]string, 0, len(repos))
	for _, repo := range repos {
		ids = append(ids, "github/org/"+repo)
	}
	config := w.config
	b := gitDeltaBaseline{
		Resolver:      w.resolver,
		Instruments:   w.inst,
		Reconcile:     reconcilePolicy{Interval: 24 * time.Hour, MaxPerCycle: 10},
		Now:           func() time.Time { return w.now },
		Preparse:      &config,
		ParseMaxSkips: maxSkips,
	}
	if baseline != nil {
		baseline(&b)
	}
	synced, err := syncGitRepositoriesWithLogger(context.Background(), reconcileTestConfig(w.reposDir), ids, discardLogger(), b)
	if err != nil {
		w.t.Fatalf("syncGitRepositoriesWithLogger: %v", err)
	}
	return synced
}

func (w *skipParseWorld) outcomes() map[string]int64 {
	w.t.Helper()
	var collected metricdata.ResourceMetrics
	if err := w.reader.Collect(context.Background(), &collected); err != nil {
		w.t.Fatalf("collect: %v", err)
	}
	out := map[string]int64{}
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != "eshu_dp_collector_reconcile_parse_skip_decisions_total" {
				continue
			}
			for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
				value, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionOutcome))
				out[value.AsString()] += point.Value
			}
		}
	}
	return out
}

// TestSkipParseBootstrapThenSkip (ruling a9-143 item 3): the first forced
// reconcile after deploy finds no stored identity, parses, and carries the
// identity the store stamps; the next one finds it equal and skips the parse,
// re-projecting the active generation instead.
func TestSkipParseBootstrapThenSkip(t *testing.T) {
	w := newSkipParseWorld(t, "", "big")
	w.resolver.fallback = w.overdue("", 0)
	first := w.sync(7, nil, "big")
	if len(first.SelectedRepoPaths) != 1 || len(w.resolver.reprojected) != 0 {
		t.Fatalf("first sweep selected %d and reprojected %d, want a parse", len(first.SelectedRepoPaths), len(w.resolver.reprojected))
	}
	stamped := first.PreparseIdentityByRepoPath[w.repoPath("big")]
	if want := preparse.Identity(GenerationContentVersion(), "samesha", repositoryDefaultBranch(first.RefsByRepoPath[w.repoPath("big")]), w.config); stamped == "" || stamped != want {
		t.Fatalf("first sweep identity = %q, want %q", stamped, want)
	}
	w.resolver.fallback = w.overdue(stamped, 0)
	second := w.sync(7, nil, "big")
	if len(second.SelectedRepoPaths) != 0 || len(w.resolver.reprojected) != 1 {
		t.Fatalf("second sweep selected %d and reprojected %d, want a skipped parse", len(second.SelectedRepoPaths), len(w.resolver.reprojected))
	}
	got := w.resolver.reprojected[0]
	if got.GenerationID != "gen-a" || got.CommitSHA != "samesha" || got.PreparseIdentity != stamped ||
		got.Reason != preparse.ReasonIntervalElapsed || got.ScopeID != w.scopeID("big") {
		t.Fatalf("reprojection = %+v, want gen-a at samesha with the stamped identity", got)
	}
	if outcomes := w.outcomes(); outcomes["identity_absent"] != 1 || outcomes["skipped"] != 1 {
		t.Fatalf("decision outcomes = %v, want identity_absent=1 skipped=1", outcomes)
	}
}

// TestSkipParseParsesWhenARuleRefuses: every refusal parses and re-projects
// nothing (D1, D3, items 1 and 7).
func TestSkipParseParsesWhenARuleRefuses(t *testing.T) {
	cells := []struct {
		name     string
		gitArms  string
		maxSkips int
		state    func(*skipParseWorld) scope.FullReconcileState
		setup    func(*skipParseWorld)
		baseline func(*gitDeltaBaseline)
		outcome  string
	}{
		{
			name: "valve due on the 7th interval", maxSkips: 7,
			state: func(w *skipParseWorld) scope.FullReconcileState { return w.overdue(w.identity(), 6) }, outcome: "valve_due",
		},
		{
			name: "valve off", maxSkips: 0,
			state: func(w *skipParseWorld) scope.FullReconcileState { return w.overdue(w.identity(), 0) }, outcome: "disabled",
		},
		{
			name: "identity mismatch", maxSkips: 7,
			state: func(w *skipParseWorld) scope.FullReconcileState { return w.overdue("preparse:v1:other", 0) }, outcome: "identity_mismatch",
		},
		{name: "delta active", maxSkips: 7, state: func(w *skipParseWorld) scope.FullReconcileState {
			s := w.overdue(w.identity(), 0)
			s.ActiveIsDelta = true
			return s
		}, outcome: "delta_active"},
		{name: "head moved", maxSkips: 7, state: func(w *skipParseWorld) scope.FullReconcileState {
			s := w.overdue(w.identity(), 0)
			s.ActiveCommitSHA = "oldsha"
			return s
		}, outcome: "head_moved"},
		{name: "dirty tree", maxSkips: 7, gitArms: `
	*"status --porcelain"*)
		printf "?? leftover.txt\n"
		;;`, state: func(w *skipParseWorld) scope.FullReconcileState { return w.overdue(w.identity(), 0) }, outcome: "dirty_tree"},
		{
			name: "submodules", maxSkips: 7,
			state: func(w *skipParseWorld) scope.FullReconcileState { return w.overdue(w.identity(), 0) },
			setup: func(w *skipParseWorld) {
				if err := os.WriteFile(filepath.Join(w.repoPath("big"), ".gitmodules"), []byte("[submodule \"x\"]\n"), 0o600); err != nil {
					t.Fatalf("write .gitmodules: %v", err)
				}
			}, outcome: "submodules_present",
		},
		{
			name: "explicit reindex", maxSkips: 7,
			state:    func(w *skipParseWorld) scope.FullReconcileState { return w.overdue(w.identity(), 0) },
			baseline: func(b *gitDeltaBaseline) { b.ReindexRequestedAt = time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC) },
			outcome:  "reason_ineligible",
		},
		{
			name: "store error", maxSkips: 7,
			state: func(w *skipParseWorld) scope.FullReconcileState { return w.overdue(w.identity(), 0) },
			setup: func(w *skipParseWorld) { w.resolver.reprojectErr = errors.New("postgres down") }, outcome: "read_error",
		},
	}
	for _, cell := range cells {
		t.Run(cell.name, func(t *testing.T) {
			w := newSkipParseWorld(t, cell.gitArms, "big")
			w.resolver.fallback = cell.state(w)
			if cell.setup != nil {
				cell.setup(w)
			}
			synced := w.sync(cell.maxSkips, cell.baseline, "big")
			if len(synced.SelectedRepoPaths) != 1 || len(w.resolver.reprojected) != 0 {
				t.Fatalf("selected %d and reprojected %d, want a parse", len(synced.SelectedRepoPaths), len(w.resolver.reprojected))
			}
			if outcomes := w.outcomes(); outcomes[cell.outcome] != 1 {
				t.Fatalf("decision outcomes = %v, want %s=1", outcomes, cell.outcome)
			}
		})
	}
}

// TestSkipParseGraphDirtyIsEligible (D1): a graph_dirty reconcile with an
// equal identity skips the parse like interval_elapsed.
func TestSkipParseGraphDirtyIsEligible(t *testing.T) {
	w := newSkipParseWorld(t, "", "big")
	state := w.overdue(w.identity(), 0)
	state.LastProjectedFullAt = w.now.Add(-time.Hour)
	state.LastStoredFullAt = w.now.Add(-time.Hour)
	w.resolver.fallback = state
	w.resolver.writers = []scope.UncoveredProjectionWriter{{GenerationID: "gen-w"}}
	synced := w.sync(7, nil, "big")
	if len(synced.SelectedRepoPaths) != 0 || len(w.resolver.reprojected) != 1 || w.resolver.reprojected[0].Reason != preparse.ReasonGraphDirty {
		t.Fatalf("selected %d, reprojections %+v, want one graph_dirty re-projection", len(synced.SelectedRepoPaths), w.resolver.reprojected)
	}
}

// TestSkipParseNeverRunsWhileARequestIsUnacknowledged (a9-117): an
// unacknowledged re-open holds the scope in flight, so the sweep forces
// nothing and the skipped-parse path never runs.
func TestSkipParseNeverRunsWhileARequestIsUnacknowledged(t *testing.T) {
	w := newSkipParseWorld(t, "", "big")
	state := w.overdue(w.identity(), 0)
	state.ReconcileRequestedAt = w.now.Add(-time.Hour)
	w.resolver.fallback = state
	w.sync(7, nil, "big")
	if len(w.resolver.reprojected) != 0 {
		t.Fatalf("reprojected %d while a request is unacknowledged, want 0", len(w.resolver.reprojected))
	}
}

// TestSkipParseSpendsTheBudgetSlot (D2): a skipped parse still re-projects,
// so it takes a MAX_PER_CYCLE slot; with a cap of 1 the second due scope
// waits for the next cycle.
func TestSkipParseSpendsTheBudgetSlot(t *testing.T) {
	w := newSkipParseWorld(t, "", "a-first", "b-second")
	w.resolver.fallback = w.overdue(w.identityOf("a-first"), 0)
	synced := w.sync(7, func(b *gitDeltaBaseline) { b.Reconcile.MaxPerCycle = 1 }, "a-first", "b-second")
	if len(w.resolver.reprojected) != 1 || w.resolver.reprojected[0].ScopeID != w.scopeID("a-first") {
		t.Fatalf("reprojections = %+v, want only a-first", w.resolver.reprojected)
	}
	if synced.ReconcileByRepoPath[w.repoPath("b-second")] {
		t.Fatal("b-second was forced although the only slot was spent")
	}
}

// TestPreparseReasonsMatchTheSweepReasons pins the two eligible reason
// strings to the sweep's own constants.
func TestPreparseReasonsMatchTheSweepReasons(t *testing.T) {
	if preparse.ReasonIntervalElapsed != reconcileReasonIntervalElapsed || preparse.ReasonGraphDirty != reconcileReasonGraphDirty {
		t.Fatalf("preparse reasons %q/%q != sweep reasons %q/%q", preparse.ReasonIntervalElapsed, preparse.ReasonGraphDirty,
			reconcileReasonIntervalElapsed, reconcileReasonGraphDirty)
	}
}

// TestPreparseIdentityFlowsToTheGeneration: the sync's identity reaches the
// selected repository, the snapshot, and a full generation; a delta
// generation never carries one.
func TestPreparseIdentityFlowsToTheGeneration(t *testing.T) {
	repoPath := filepath.Join(t.TempDir(), "github", "org", "big")
	repos := applyPreparseIdentities([]SelectedRepository{{RepoPath: repoPath}, {RepoPath: repoPath, Ref: "release"}},
		map[string]string{repoPath: "preparse:v1:x"})
	if repos[0].PreparseIdentity != "preparse:v1:x" || repos[1].PreparseIdentity != "" {
		t.Fatalf("selected identities = %q/%q, want the default branch only", repos[0].PreparseIdentity, repos[1].PreparseIdentity)
	}
	for _, delta := range []bool{false, true} {
		snapshot := RepositorySnapshot{RepoPath: repoPath, HeadCommitSHA: "samesha", Delta: delta, PreparseIdentity: "preparse:v1:x"}
		collected := buildStreamingGeneration(repoPath, testCollectorRepositoryMetadata(repoPath), "run", time.Now().UTC(), snapshot, false, "")
		drainFactChannel(collected.Facts)
		want := "preparse:v1:x"
		if delta {
			want = ""
		}
		if got := collected.Generation.PreparseIdentity; got != want {
			t.Fatalf("delta=%v generation identity = %q, want %q", delta, got, want)
		}
	}
}
