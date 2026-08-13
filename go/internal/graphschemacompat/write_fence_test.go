// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphschemacompat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// markerReply is one scripted answer to the marker read.
type markerReply struct {
	fingerprint string
	compatible  string
	err         error
}

// scriptedMarkerQueryer answers successive marker reads from a script, so a
// test can change what the applied schema says WITHOUT restarting the writer --
// which is the whole situation the fence exists for. The last reply repeats.
type scriptedMarkerQueryer struct {
	mu      sync.Mutex
	replies []markerReply
	calls   int
}

func (q *scriptedMarkerQueryer) QueryContext(context.Context, string, ...any) (postgres.Rows, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	reply := q.replies[len(q.replies)-1]
	if q.calls < len(q.replies) {
		reply = q.replies[q.calls]
	}
	q.calls++
	if reply.err != nil {
		return nil, reply.err
	}
	return &fakeGraphSchemaRows{values: [][]any{{reply.fingerprint, []byte(reply.compatible)}}}, nil
}

func (q *scriptedMarkerQueryer) callCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.calls
}

// admittedReply is the marker a writer built from this source expects to find.
func admittedReply(t *testing.T) markerReply {
	t.Helper()
	app, err := graph.SchemaApplicationForBackend(graph.SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaApplicationForBackend() error = %v", err)
	}
	return markerReply{fingerprint: app.Fingerprint, compatible: "[]"}
}

// refusedReply is a marker recorded by some other release, which lists this
// writer's fingerprint nowhere.
func refusedReply() markerReply {
	return markerReply{fingerprint: "some-other-release-fingerprint", compatible: `["neither-is-this-one"]`}
}

// newTestFence returns a fence over a controllable clock so the interval can be
// crossed without sleeping.
func newTestFence(db postgres.Queryer, clock *time.Time) *WriteFence {
	fence := NewWriteFence(db, graph.SchemaBackendNornicDB, time.Minute)
	fence.now = func() time.Time { return *clock }
	return fence
}

// TestWriteFenceRefusesAWriterAfterTheAppliedSchemaChangesUnderIt is the
// finding this fence answers: RequireCompatible runs once at startup, so a
// writer that was admitted keeps writing after bootstrap records a marker that
// no longer admits it. Nothing here restarts the writer.
func TestWriteFenceRefusesAWriterAfterTheAppliedSchemaChangesUnderIt(t *testing.T) {
	t.Parallel()

	db := &scriptedMarkerQueryer{replies: []markerReply{admittedReply(t), refusedReply()}}
	clock := time.Unix(1_800_000_000, 0).UTC()
	fence := newTestFence(db, &clock)

	if err := fence.Check(context.Background()); err != nil {
		t.Fatalf("first Check() error = %v, want nil; this writer is the one the marker admits", err)
	}

	clock = clock.Add(2 * time.Minute)
	err := fence.Check(context.Background())
	if !errors.Is(err, ErrIncompatible) {
		t.Fatalf("Check() after the cutover = %v, want ErrIncompatible", err)
	}
	if got, want := err.Error(), "run eshu-bootstrap-data-plane"; !strings.Contains(got, want) {
		t.Fatalf("Check() error = %q, want it to keep the operator instruction %q", got, want)
	}
}

// TestWriteFenceReusesItsDecisionWithinTheInterval keeps the fence off the hot
// path: one indexed read per interval, not one per canonical write.
func TestWriteFenceReusesItsDecisionWithinTheInterval(t *testing.T) {
	t.Parallel()

	db := &scriptedMarkerQueryer{replies: []markerReply{admittedReply(t), refusedReply()}}
	clock := time.Unix(1_800_000_000, 0).UTC()
	fence := newTestFence(db, &clock)

	for i := 0; i < 5; i++ {
		if err := fence.Check(context.Background()); err != nil {
			t.Fatalf("Check() %d error = %v, want nil", i, err)
		}
	}
	if got := db.callCount(); got != 1 {
		t.Fatalf("marker read %d times inside one interval, want 1", got)
	}

	clock = clock.Add(2 * time.Minute)
	if err := fence.Check(context.Background()); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("Check() past the interval = %v, want ErrIncompatible", err)
	}
	if got := db.callCount(); got != 2 {
		t.Fatalf("marker read %d times across two intervals, want 2", got)
	}
}

// TestWriteFenceHoldsItsDecisionWhenTheMarkerCannotBeRead is the failure mode
// that would be worse than the gap it closes: an unreachable Postgres must not
// stop every canonical writer in the deployment at once. Only a marker that is
// readable and says no refuses.
func TestWriteFenceHoldsItsDecisionWhenTheMarkerCannotBeRead(t *testing.T) {
	t.Parallel()

	unreadable := markerReply{err: errors.New("dial tcp: connection refused")}
	db := &scriptedMarkerQueryer{replies: []markerReply{
		admittedReply(t),
		unreadable,
		refusedReply(),
	}}
	clock := time.Unix(1_800_000_000, 0).UTC()
	fence := newTestFence(db, &clock)

	if err := fence.Check(context.Background()); err != nil {
		t.Fatalf("first Check() error = %v, want nil", err)
	}

	clock = clock.Add(2 * time.Minute)
	if err := fence.Check(context.Background()); err != nil {
		t.Fatalf("Check() with the marker unreadable = %v, want nil; a database blip is not a refusal", err)
	}
	// The retry is rate limited too: a failed read still advances the interval,
	// so an outage costs one query per interval rather than one per write.
	if got := db.callCount(); got != 2 {
		t.Fatalf("marker read %d times, want 2", got)
	}
	if err := fence.Check(context.Background()); err != nil {
		t.Fatalf("Check() immediately after a failed read = %v, want nil", err)
	}
	if got := db.callCount(); got != 2 {
		t.Fatalf("marker read %d times after a failed read inside the interval, want 2", got)
	}

	clock = clock.Add(2 * time.Minute)
	if err := fence.Check(context.Background()); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("Check() once the marker is readable again = %v, want ErrIncompatible", err)
	}
}

// TestWriteFenceKeepsRefusingWhileTheMarkerIsUnreadable is the same rule from
// the other side: once refused, a read that fails must not re-admit the writer.
func TestWriteFenceKeepsRefusingWhileTheMarkerIsUnreadable(t *testing.T) {
	t.Parallel()

	db := &scriptedMarkerQueryer{replies: []markerReply{
		refusedReply(),
		{err: errors.New("dial tcp: connection refused")},
	}}
	clock := time.Unix(1_800_000_000, 0).UTC()
	fence := newTestFence(db, &clock)

	if err := fence.Check(context.Background()); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("first Check() = %v, want ErrIncompatible", err)
	}
	clock = clock.Add(2 * time.Minute)
	if err := fence.Check(context.Background()); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("Check() with the marker unreadable = %v, want the refusal to stand", err)
	}
}

// TestNewWriteFenceForRuntimeSkipsProfilesWithoutAMarker mirrors
// RequireCompatibleForRuntime: the profiles with no graph schema marker get no
// fence, and a nil fence admits every write so callers can wire it plainly.
func TestNewWriteFenceForRuntimeSkipsProfilesWithoutAMarker(t *testing.T) {
	t.Parallel()

	for name, env := range map[string]map[string]string{
		"disabled graph backend": {"ESHU_DISABLE_NEO4J": "true"},
		"local lightweight":      {"ESHU_QUERY_PROFILE": "local_lightweight"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fence, err := NewWriteFenceForRuntime(&scriptedMarkerQueryer{}, func(key string) string {
				return env[key]
			})
			if err != nil {
				t.Fatalf("NewWriteFenceForRuntime() error = %v, want nil", err)
			}
			if fence != nil {
				t.Fatalf("NewWriteFenceForRuntime() = %#v, want nil fence", fence)
			}
			if err := fence.Check(context.Background()); err != nil {
				t.Fatalf("nil fence Check() = %v, want nil", err)
			}
		})
	}
}

// TestNewWriteFenceForRuntimeUsesTheSelectedBackend proves the fence reads the
// same backend's marker the startup gate does, rather than a default.
func TestNewWriteFenceForRuntimeUsesTheSelectedBackend(t *testing.T) {
	t.Parallel()

	db := &scriptedMarkerQueryer{replies: []markerReply{refusedReply()}}
	fence, err := NewWriteFenceForRuntime(db, func(key string) string {
		if key == "ESHU_GRAPH_BACKEND" {
			return "neo4j"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("NewWriteFenceForRuntime() error = %v, want nil", err)
	}
	if got, want := fence.backend, graph.SchemaBackendNeo4j; got != want {
		t.Fatalf("fence backend = %q, want %q", got, want)
	}
	if got := fence.Check(context.Background()); !errors.Is(got, ErrIncompatible) {
		t.Fatalf("Check() = %v, want ErrIncompatible", got)
	}
	if want := fmt.Sprintf("for backend %s", graph.SchemaBackendNeo4j); !strings.Contains(fence.Check(context.Background()).Error(), want) {
		t.Fatalf("refusal does not name the backend it checked, want %q", want)
	}
}
