package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestPrintAdminStatusVersionFlagReturnsBeforePostgresOpen(t *testing.T) {
	original := buildinfo.Version
	buildinfo.Version = "v1.2.3-admin"
	t.Cleanup(func() { buildinfo.Version = original })

	var stdout bytes.Buffer
	handled, err := printAdminStatusVersionFlag([]string{"-v"}, &stdout)
	if err != nil {
		t.Fatalf("printAdminStatusVersionFlag() error = %v, want nil", err)
	}
	if !handled {
		t.Fatal("printAdminStatusVersionFlag() handled = false, want true")
	}
	if got, want := stdout.String(), "eshu-admin-status v1.2.3-admin\n"; got != want {
		t.Fatalf("printAdminStatusVersionFlag() output = %q, want %q", got, want)
	}
}

func TestRenderStatusOutputsTextFromSharedStatusReport(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	reader := &fakeReader{
		snapshot: statuspkg.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			ScopeActivity: statuspkg.ScopeActivitySnapshot{
				Active:  4,
				Changed: 2,
			},
			GenerationCounts: []statuspkg.NamedCount{
				{Name: "active", Count: 1},
				{Name: "completed", Count: 2},
				{Name: "superseded", Count: 1},
			},
			GenerationTransitions: []statuspkg.GenerationTransitionSnapshot{
				{
					ScopeID:       "scope-1",
					GenerationID:  "generation-b",
					Status:        "active",
					TriggerKind:   "snapshot",
					ObservedAt:    time.Date(2026, 4, 12, 15, 45, 0, 0, time.UTC),
					ActivatedAt:   time.Date(2026, 4, 12, 15, 46, 0, 0, time.UTC),
					FreshnessHint: "fresh snapshot",
				},
			},
			Queue: statuspkg.QueueSnapshot{
				Outstanding:          2,
				InFlight:             1,
				OldestOutstandingAge: 45 * time.Second,
			},
		},
	}

	err := renderStatus(
		context.Background(),
		[]string{"--format=text"},
		stdout,
		stderr,
		reader,
		func() time.Time {
			return time.Date(2026, 4, 12, 12, 0, 0, 0, time.FixedZone("EDT", -4*60*60))
		},
	)
	if err != nil {
		t.Fatalf("renderStatus() error = %v, want nil", err)
	}
	if !reader.asOf.Equal(time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("renderStatus() reader asOf = %v, want UTC timestamp", reader.asOf)
	}
	if got := stdout.String(); !strings.Contains(got, "Health: progressing") {
		t.Fatalf("renderStatus() stdout = %q, want health summary", got)
	}
	if got := stdout.String(); !strings.Contains(got, "Scope activity: active=4 changed=2") {
		t.Fatalf("renderStatus() stdout = %q, want scope activity summary", got)
	}
	if got := stdout.String(); !strings.Contains(got, "Scope activity: active=4 changed=2 unchanged=2") {
		t.Fatalf("renderStatus() stdout = %q, want unchanged scope summary", got)
	}
	if got := stdout.String(); !strings.Contains(got, "Generation history: active=1 pending=0 completed=2 superseded=1 failed=0 other=0") {
		t.Fatalf("renderStatus() stdout = %q, want generation history summary", got)
	}
	if got := stdout.String(); !strings.Contains(got, "Generation transitions:") {
		t.Fatalf("renderStatus() stdout = %q, want generation transitions summary", got)
	}
	if got := stdout.String(); !strings.Contains(got, "Flow:") {
		t.Fatalf("renderStatus() stdout = %q, want flow summary", got)
	}
}

func TestRenderStatusOutputsJSONFromSharedStatusReport(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	reader := &fakeReader{
		snapshot: statuspkg.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			ScopeActivity: statuspkg.ScopeActivitySnapshot{
				Active:  4,
				Changed: 1,
			},
			GenerationCounts: []statuspkg.NamedCount{
				{Name: "active", Count: 2},
				{Name: "pending", Count: 1},
				{Name: "completed", Count: 3},
				{Name: "superseded", Count: 1},
				{Name: "failed", Count: 1},
			},
			GenerationTransitions: []statuspkg.GenerationTransitionSnapshot{
				{
					ScopeID:                   "scope-2",
					GenerationID:              "generation-c",
					Status:                    "superseded",
					TriggerKind:               "snapshot",
					ObservedAt:                time.Date(2026, 4, 12, 15, 30, 0, 0, time.UTC),
					SupersededAt:              time.Date(2026, 4, 12, 15, 40, 0, 0, time.UTC),
					CurrentActiveGenerationID: "generation-d",
				},
			},
		},
	}

	err := renderStatus(
		context.Background(),
		[]string{"--format=json"},
		stdout,
		stderr,
		reader,
		func() time.Time {
			return time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC)
		},
	)
	if err != nil {
		t.Fatalf("renderStatus() error = %v, want nil", err)
	}
	if got := stdout.String(); !strings.Contains(got, "\"health\"") {
		t.Fatalf("renderStatus() stdout = %q, want json payload", got)
	}
	if got := stdout.String(); !strings.Contains(got, "\"scope_activity\"") {
		t.Fatalf("renderStatus() stdout = %q, want scope activity payload", got)
	}
	if got := stdout.String(); !strings.Contains(got, "\"generation_history\"") {
		t.Fatalf("renderStatus() stdout = %q, want generation history payload", got)
	}
	if got := stdout.String(); !strings.Contains(got, "\"generation_transitions\"") {
		t.Fatalf("renderStatus() stdout = %q, want generation transitions payload", got)
	}
	if got := stdout.String(); !strings.Contains(got, "\"flow\"") {
		t.Fatalf("renderStatus() stdout = %q, want flow payload", got)
	}
}

func TestRenderStatusPropagatesReaderErrors(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	wantErr := errors.New("boom")

	err := renderStatus(
		context.Background(),
		nil,
		stdout,
		stderr,
		&fakeReader{err: wantErr},
		func() time.Time {
			return time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC)
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("renderStatus() error = %v, want wrapped %v", err, wantErr)
	}
}

func TestRunReturnsPostgresConfigErrorWhenDSNMissing(t *testing.T) {
	t.Parallel()

	err := run(
		context.Background(),
		nil,
		&bytes.Buffer{},
		&bytes.Buffer{},
		func(string) string { return "" },
	)
	if err == nil {
		t.Fatal("run() error = nil, want non-nil")
	}
}

type fakeReader struct {
	snapshot statuspkg.RawSnapshot
	err      error
	asOf     time.Time
}

func (r *fakeReader) ReadStatusSnapshot(_ context.Context, asOf time.Time) (statuspkg.RawSnapshot, error) {
	r.asOf = asOf
	if r.err != nil {
		return statuspkg.RawSnapshot{}, r.err
	}

	return r.snapshot, nil
}
